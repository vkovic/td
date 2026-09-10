package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/store"
)

// tdBinary builds the real td once for the whole package and returns its path.
//
// The end-to-end pass needs a genuinely external actor — another process
// holding the same lock and writing the same files — which is what Claude and a
// shell both are. Calling into internal/store instead would test the TUI
// against itself.
var tdBinary = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "td-e2e-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "td")
	cmd := exec.Command("go", "build", "-o", path, "github.com/vkovic/td/cmd/td")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", &buildError{out: string(out), err: err}
	}
	return path, nil
})

// buildError reports a failed build with the compiler's own output.
type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string { return e.err.Error() + ": " + e.out }

// td runs the real binary against the test's store, as another process would.
func td(t *testing.T, s *store.Store, args ...string) string {
	t.Helper()
	bin, err := tdBinary()
	if err != nil {
		t.Skipf("cannot build td: %v", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), store.EnvRoot+"="+s.Root())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("td %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// everythingParses reads every item file in every scope and area, which is the
// Definition of Done's standing requirement: whatever the pane and the CLI did
// to each other, the store is still a store.
func everythingParses(t *testing.T, s *store.Store) {
	t.Helper()
	if _, err := s.ListAll(store.Active, store.Archived, store.Deleted); err != nil {
		t.Fatalf("the store no longer parses cleanly: %v", err)
	}
}

// TestDefinitionOfDone walks the pane and a separate td process through each
// other's changes, in one session, in the order a person would meet them.
func TestDefinitionOfDone(t *testing.T) {
	s := newStore(t)
	appended := script(t, "append", `echo "a paragraph the editor added" >> "$1"`)

	m := newModel(t, s, watching, func(o *Options) {
		o.Config = config.Default()
		o.Config.Editor = appended
		o.Exec = syncExec
	})
	t.Cleanup(func() { m.Close() })

	// 1. Something outside td adds an item. The pane shows it without being
	//    told, which is the whole reason it sits beside a Claude session.
	td(t, s, "add", "write the release notes", "--source", "claude")
	settle(t, m, 3*time.Second)

	if got := strings.Join(titles(m), ","); got != "write the release notes" {
		t.Fatalf("the pane shows %q after an external add, want the added item", got)
	}
	if got := m.Entries()[0].Item.Source; got != "claude" {
		t.Errorf("source is %q, want claude — the field records who created the item", got)
	}
	everythingParses(t, s)

	// 2. e opens it, the editor writes, and the pane comes back to the list
	//    with the change recorded and committed.
	drain(t, m, press(m, "e"))
	settle(t, m, 2*time.Second)

	path := m.Entries()[0].Ref.Path
	if got := body(t, s, path); !strings.Contains(got, "a paragraph the editor added") {
		t.Errorf("the body is %q, want the editor's paragraph", got)
	}
	if m.prompt.open() || m.showHelp {
		t.Error("the pane did not come back to the list after the editor")
	}
	everythingParses(t, s)

	// 3. x from the pane is visible in the file itself, not just on screen.
	drain(t, m, press(m, "x"))
	settle(t, m, 2*time.Second)

	it, err := s.Load(m.Entries()[0].Ref.Path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !it.Done() {
		t.Error("x did not write done_at to the file")
	}

	// 4. And the other direction: a change another process makes is visible in
	//    the pane.
	td(t, s, "undo", it.ID)
	settle(t, m, 3*time.Second)

	if m.Entries()[0].Item.Done() {
		t.Error("the pane still shows the item done after td undo reopened it")
	}
	everythingParses(t, s)

	// 5. d moves it to the trash, where it is recoverable by hand.
	drain(t, m, press(m, "d"))
	settle(t, m, 2*time.Second)

	if len(m.Entries()) != 0 {
		t.Errorf("the pane still lists %d items after d", len(m.Entries()))
	}
	if !filesUnder(t, s.Dir(store.Global, store.Deleted), it.ID) {
		t.Errorf("%s does not hold the removed item", s.Dir(store.Global, store.Deleted))
	}
	everythingParses(t, s)
}

// TestRefreshSweepsAnExpiredItemToTheArchive: an item completed longer ago than
// done_ttl_days leaves the list on the next epilogue, and r is how the pane asks
// for one.
func TestRefreshSweepsAnExpiredItemToTheArchive(t *testing.T) {
	s := newStore(t)

	cfg := config.Default()
	cfg.DoneTTLDays = 7
	long := time.Now().Add(-30 * 24 * time.Hour)
	save(t, s, item{id: "aaaaaaaa", title: "long finished", updated: long, created: long, doneAt: done(long)})
	save(t, s, item{id: "bbbbbbbb", title: "just finished", updated: time.Now(),
		created: time.Now(), doneAt: done(time.Now())})

	m := newModel(t, s, func(o *Options) {
		o.Config = cfg
		o.Now = time.Now
	})

	drain(t, m, press(m, "r"))

	if got := strings.Join(titles(m), ","); got != "just finished" {
		t.Errorf("the pane lists %s, want only the recently finished item", got)
	}
	if !filesUnder(t, s.Dir(store.Global, store.Archived), "aaaaaaaa") {
		t.Errorf("%s does not hold the swept item", s.Dir(store.Global, store.Archived))
	}
	if filesUnder(t, s.Dir(store.Global, store.Active), "aaaaaaaa") {
		t.Error("the swept item is still in the live list")
	}
	if !strings.Contains(m.status, "archived") {
		t.Errorf("the status is %q, want it to report the sweep", m.status)
	}
	everythingParses(t, s)
}

// TestTheCLISeesWhatThePaneWrote: the pane and the CLI go through the same
// store, so td ls reports an item the pane created.
func TestTheCLISeesWhatThePaneWrote(t *testing.T) {
	s := newStore(t)
	m := newModel(t, s, withEditor("true"))

	press(m, "a")
	for _, r := range "raised in the pane" {
		press(m, string(r))
	}
	drain(t, m, press(m, "enter"))

	out := td(t, s, "ls")
	if !strings.Contains(out, "raised in the pane") {
		t.Errorf("td ls does not show the pane's item:\n%s", out)
	}
	if !strings.Contains(out, m.Entries()[0].Item.ID) {
		t.Errorf("td ls does not show the pane's id %s:\n%s", m.Entries()[0].Item.ID, out)
	}
	everythingParses(t, s)
}

// TestTwoProcessesUnderOneLock: the pane's epilogue and a CLI command take the
// same lock, and both complete rather than one corrupting the other's work.
func TestTwoProcessesUnderOneLock(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaaaaaaa", title: "contended", updated: ago(1)})
	m := newModel(t, s)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		td(t, s, "add", "added while the pane was committing")
	}()
	drain(t, m, press(m, "x"))
	wg.Wait()

	everythingParses(t, s)
	if err := m.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(m.Entries()); got != 2 {
		t.Errorf("the store holds %d items, want both processes' work", got)
	}
	if !findTitle(t, m, "contended").Item.Done() {
		t.Error("the pane's change did not survive the CLI's")
	}
}

// filesUnder reports whether a directory holds a file naming id.
func filesUnder(t *testing.T, dir, id string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id) {
			return true
		}
	}
	return false
}
