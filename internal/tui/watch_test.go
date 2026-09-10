package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vkovic/td/internal/store"
)

// testDebounce is short enough to keep the tests quick and long enough that a
// burst of writes still lands inside one window.
const testDebounce = 40 * time.Millisecond

// startWatcher opens a watcher over a store and closes it with the test.
func startWatcher(t *testing.T, s *store.Store) *watcher {
	t.Helper()
	w, err := newWatcher(s, testDebounce)
	if err != nil {
		t.Fatalf("newWatcher: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// pulses counts how many bursts the watcher reported inside a window that
// comfortably outlasts the debounce.
func pulses(w *watcher, within time.Duration) int {
	deadline := time.After(within)
	n := 0
	for {
		select {
		case <-w.Pulses():
			n++
		case <-deadline:
			return n
		}
	}
}

// awaitPulse waits for one burst, reporting whether it arrived.
func awaitPulse(w *watcher, within time.Duration) bool {
	select {
	case <-w.Pulses():
		return true
	case <-time.After(within):
		return false
	}
}

// writeItem drops an item file straight into a directory, as a hand edit or
// another td process would. It does not go through the model.
func writeItem(t *testing.T, dir, name, title string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	text := "---\nid: " + strings.TrimSuffix(name, ".md") + "\ntitle: " + title +
		"\ncreated: 2026-09-10T00:00:00Z\nupdated: 2026-09-10T00:00:00Z\n---\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestWatchSeesANewItem: a file written straight into the store reaches the
// watcher inside the debounce window.
func TestWatchSeesANewItem(t *testing.T) {
	s := newStore(t)
	w := startWatcher(t, s)

	writeItem(t, s.Dir(store.Global, store.Active), "aaaaaaaa-outside.md", "written outside td")

	if !awaitPulse(w, time.Second) {
		t.Error("a new item file produced no pulse")
	}
}

// TestWatchCoalescesABurst: one td command writes, renames and commits, which
// is many events for one change. A person watching the pane wants one refresh.
func TestWatchCoalescesABurst(t *testing.T) {
	s := newStore(t)
	w := startWatcher(t, s)

	dir := s.Dir(store.Global, store.Active)
	for i := 0; i < 10; i++ {
		writeItem(t, dir, "aaaaaaa"+string(rune('a'+i))+"-burst.md", "burst")
	}

	if got := pulses(w, 10*testDebounce); got != 1 {
		t.Errorf("ten writes produced %d pulses, want 1", got)
	}
}

// TestWatchIgnoresTheDirectoriesThatAreNotTheList: git's own writes, the trash,
// the archive and the lock file are all changes the list does not show, and
// .git in particular would turn every commit into an event storm.
func TestWatchIgnoresTheDirectoriesThatAreNotTheList(t *testing.T) {
	s := newStore(t)
	w := startWatcher(t, s)

	writeItem(t, filepath.Join(s.Root(), ".git"), "aaaaaaaa-git.md", "inside git")
	writeItem(t, s.Dir(store.Global, store.Deleted), "bbbbbbbb-trash.md", "in the trash")
	writeItem(t, s.Dir(store.Global, store.Archived), "cccccccc-archive.md", "archived")
	if err := os.WriteFile(s.LockPath(), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := pulses(w, 10*testDebounce); got != 0 {
		t.Errorf("the ignored paths produced %d pulses, want 0", got)
	}
}

// TestWatchIgnoresTheAtomicWriteTempFile: the store writes a temp file and
// renames it, and the rename is the event worth reporting. The temp file
// appearing is not, or every write would report twice.
func TestWatchIgnoresTheAtomicWriteTempFile(t *testing.T) {
	s := newStore(t)
	w := startWatcher(t, s)

	f, err := os.CreateTemp(s.Dir(store.Global, store.Active), ".td-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("half written")
	f.Close()

	if got := pulses(w, 10*testDebounce); got != 0 {
		t.Errorf("a temp file produced %d pulses, want 0", got)
	}
}

// TestWatchFollowsANewProject: a project directory created after the watcher
// started has to be watched too, or items added to it would never appear.
func TestWatchFollowsANewProject(t *testing.T) {
	s := newStore(t)
	w := startWatcher(t, s)

	dir := s.Dir(store.Scope("acme"), store.Active)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !awaitPulse(w, time.Second) {
		t.Fatal("a new project directory produced no pulse")
	}

	writeItem(t, dir, "aaaaaaaa-in-acme.md", "inside the new project")
	if !awaitPulse(w, time.Second) {
		t.Error("an item in a project created after the watcher started produced no pulse")
	}
}

// TestModelReloadsOnAChange: the model re-reads on a pulse and the new item is
// in the list.
func TestModelReloadsOnAChange(t *testing.T) {
	s := newStore(t)
	m := newModel(t, s, watching)
	t.Cleanup(func() { m.Close() })

	writeItem(t, s.Dir(store.Global, store.Active), "aaaaaaaa-outside.md", "written outside td")

	settle(t, m, time.Second)

	if got := strings.Join(titles(m), ","); got != "written outside td" {
		t.Errorf("the listing is %q, want the externally written item", got)
	}
}

// TestReloadDoesNotWriteAnything: the watcher re-reads and re-renders, and
// nothing else. It must not bump, sweep, commit or push, or two panes watching
// one store would drive each other in a loop.
func TestReloadDoesNotWriteAnything(t *testing.T) {
	s := newStore(t)
	m := newModel(t, s, watching)
	t.Cleanup(func() { m.Close() })
	drain(t, m, m.runEpilogue("")) // commit whatever the fixture left

	path := writeItem(t, s.Dir(store.Global, store.Active), "aaaaaaaa-outside.md", "written outside td")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	commits := commitCount(t, s)

	settle(t, m, time.Second)

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("the reload rewrote the item file")
	}
	if got := commitCount(t, s); got != commits {
		t.Errorf("the reload left %d commits, want %d", got, commits)
	}
	it, err := s.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := it.Updated.UTC().Format(time.RFC3339); got != "2026-09-10T00:00:00Z" {
		t.Errorf("the reload bumped updated to %s", got)
	}
}

// TestExternalChangeFlashes: a change this pane did not make says so, briefly.
func TestExternalChangeFlashes(t *testing.T) {
	s := newStore(t)
	m := newModel(t, s, watching)
	t.Cleanup(func() { m.Close() })

	writeItem(t, s.Dir(store.Global, store.Active), "aaaaaaaa-outside.md", "written outside td")
	settle(t, m, time.Second)

	if !strings.Contains(plain(m.View()), externalFlash) {
		t.Errorf("no flash for an external change:\n%s", plain(m.View()))
	}
}

// TestTheTUIsOwnWriteDoesNotFlash: the pane knows what it just did, so it does
// not report its own change as somebody else's. The reload still happens.
func TestTheTUIsOwnWriteDoesNotFlash(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaaaaaaa", title: "finish me", updated: ago(1)})
	m := newModel(t, s, watching)
	t.Cleanup(func() { m.Close() })

	drain(t, m, press(m, "x"))
	settle(t, m, time.Second)

	if strings.Contains(plain(m.View()), externalFlash) {
		t.Errorf("the pane flashed its own change:\n%s", plain(m.View()))
	}
	if !findTitle(t, m, "finish me").Item.Done() {
		t.Error("the item is not done after x")
	}
}

// TestFlashRetires: the note is not permanent.
func TestFlashRetires(t *testing.T) {
	s := newStore(t)
	m := newModel(t, s)
	m.flashUntil = clock.Add(flashFor)
	if !m.flashing() {
		t.Fatal("the flash is not showing right after it was raised")
	}
	m.now = at(clock.Add(flashFor + time.Second))
	if m.flashing() {
		t.Error("the flash is still showing past its window")
	}
}

// watching turns the file watcher on, with the tests' short debounce.
func watching(o *Options) {
	o.Watch = true
	o.Debounce = testDebounce
	// The watcher's own timing is real, so the clock has to be too: a frozen
	// clock would leave every flash either permanent or already expired.
	o.Now = time.Now
}

// settle delivers every pulse the watcher produces to the model until nothing
// more arrives, which is the part of the event loop these tests need.
//
// The command the model hands back is discarded rather than run: it re-arms the
// wait by blocking on the watcher's channel, and running that here would block
// the test instead of the event loop.
func settle(t *testing.T, m *Model, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		select {
		case <-m.watch.Pulses():
			m.Update(storeChangedMsg{})
		case <-time.After(2 * testDebounce):
			return
		}
	}
}
