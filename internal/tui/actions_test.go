package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/epilogue"
	"github.com/vkovic/td/internal/store"
)

// TestToggleDoneMarksAndReopens: x stamps done_at and x again clears it, and
// the row crosses the rule each time because the listing is re-read after.
func TestToggleDoneMarksAndReopens(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "finish me", updated: ago(2)})
	save(t, s, item{id: "bbb", title: "leave open", updated: ago(1)})

	m := newModel(t, s)
	// The cursor starts on the most recently updated item, which is the one
	// that must stay open, so step onto the other.
	press(m, "j")

	drain(t, m, press(m, "x"))
	e := findTitle(t, m, "finish me")
	if !e.Item.Done() {
		t.Fatal("x did not mark the item done")
	}
	if got := strings.Join(titles(m), ","); got != "leave open,finish me" {
		t.Errorf("the listing is %s, want the done item below the open one", got)
	}
	if !crossedRule(t, m, "finish me") {
		t.Error("the done item is not below the rule")
	}

	// The reload put the item back under the cursor's old neighbour, so find
	// it again before undoing it.
	m.cursor = indexOf(m, "finish me")
	drain(t, m, press(m, "x"))
	if findTitle(t, m, "finish me").Item.Done() {
		t.Error("x again did not reopen the item")
	}
}

// TestToggleDoneCommitsOnce: one keystroke leaves one commit, as one CLI
// command does.
func TestToggleDoneCommitsOnce(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "finish me", updated: ago(2)})

	m := newModel(t, s)
	drain(t, m, m.runEpilogue("")) // commit the fixture first
	before := commitCount(t, s)

	drain(t, m, press(m, "x"))

	if got, want := commitCount(t, s), before+1; got != want {
		t.Errorf("x left %d commits, want %d", got, want)
	}
}

// TestRemoveMovesToDeleted: d takes the item out of the list and leaves the
// file under deleted/, so an accident is recoverable from the directory.
func TestRemoveMovesToDeleted(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "delete me", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "keep me", updated: ago(2)})

	m := newModel(t, s)
	drain(t, m, press(m, "d"))

	if got := strings.Join(titles(m), ","); got != "keep me" {
		t.Errorf("the listing is %s, want only the kept item", got)
	}

	deleted := s.Dir(store.Global, store.Deleted)
	names, err := os.ReadDir(deleted)
	if err != nil {
		t.Fatalf("reading %s: %v", deleted, err)
	}
	found := false
	for _, n := range names {
		if strings.Contains(n.Name(), "delete-me") {
			found = true
		}
	}
	if !found {
		t.Errorf("%s holds %v, want the removed item's file", deleted, names)
	}
}

// TestRemoveCommitsOnce: the removal itself is one commit, even though the
// file it moved lands somewhere git ignores.
func TestRemoveCommitsOnce(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "delete me", updated: ago(1)})

	m := newModel(t, s)
	drain(t, m, m.runEpilogue(""))
	before := commitCount(t, s)

	drain(t, m, press(m, "d"))

	if got, want := commitCount(t, s), before+1; got != want {
		t.Errorf("d left %d commits, want %d", got, want)
	}
}

// TestMutationsRunInOrder: two keystrokes back to back both complete, in the
// order they were pressed, and the store is left consistent.
func TestMutationsRunInOrder(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "first", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "second", updated: ago(2)})

	m := newModel(t, s)
	drain(t, m, press(m, "x")) // done: first
	m.cursor = indexOf(m, "second")
	drain(t, m, press(m, "d")) // remove: second

	if got := strings.Join(titles(m), ","); got != "first" {
		t.Errorf("the listing is %s, want the done item alone", got)
	}
	if !findTitle(t, m, "first").Item.Done() {
		t.Error("the first mutation did not survive the second")
	}
	// Every item file the store still holds must parse, which is what says the
	// two runs did not tread on each other.
	if _, err := s.ListAll(store.Active, store.Archived); err != nil {
		t.Errorf("the store no longer parses cleanly: %v", err)
	}
}

// TestGateRefusesASecondEpilogue: while one epilogue holds the lock, a second
// keystroke is refused rather than queued, and says so.
func TestGateRefusesASecondEpilogue(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "finish me", updated: ago(1)})

	m := newModel(t, s)
	if cmd := press(m, "x"); cmd == nil {
		t.Fatal("x returned no command")
	}
	if !m.Busy() {
		t.Fatal("x did not mark the model busy")
	}
	for _, key := range []string{"x", "d", "r", "e", "a"} {
		if cmd := press(m, key); cmd != nil {
			t.Errorf("%s ran while an epilogue was in flight", key)
		}
	}
	if m.status != "still working" {
		t.Errorf("the status is %q, want it to say why the key did nothing", m.status)
	}
	if m.prompt.open() {
		t.Error("a opened a prompt while an epilogue was in flight")
	}
}

// TestRefreshForcesCommitAndPush: r was asked for outright, so it commits with
// auto_commit off — the way td commit does.
func TestRefreshForcesCommitAndPush(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "uncommitted", updated: ago(1)})

	m := newModel(t, s, func(o *Options) {
		o.Config = config.Default()
		o.Config.AutoCommit = false
	})
	before := commitCount(t, s)

	drain(t, m, press(m, "r"))

	if got, want := commitCount(t, s), before+1; got != want {
		t.Errorf("r with auto_commit off left %d commits, want %d", got, want)
	}
}

// TestMutatingKeysDoNotForceCommit: x mirrors td done, which honors the
// configuration. Only the key asked for outright overrides it.
func TestMutatingKeysDoNotForceCommit(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "finish me", updated: ago(1)})

	m := newModel(t, s, func(o *Options) {
		o.Config = config.Default()
		o.Config.AutoCommit = false
	})
	before := commitCount(t, s)

	drain(t, m, press(m, "x"))

	if got := commitCount(t, s); got != before {
		t.Errorf("x with auto_commit off left %d commits, want %d", got, before)
	}
	if !findTitle(t, m, "finish me").Item.Done() {
		t.Error("x did not mark the item done when the commit was skipped")
	}
}

// TestRefreshRecordsAHandEdit: r is the key that picks up a change made to a
// file outside td, which is what the epilogue's bump step is for.
func TestRefreshRecordsAHandEdit(t *testing.T) {
	s := newStore(t)
	ref := save(t, s, item{id: "aaa", title: "hand edited", updated: ago(48)})

	m := newModel(t, s)
	drain(t, m, m.runEpilogue(""))

	appendLine(t, ref.Path, "a line added by hand")

	drain(t, m, press(m, "r"))

	if got := updatedAt(t, s, ref.Path); !got.After(ago(48)) {
		t.Errorf("updated is %s, want the hand edit recorded", got)
	}
	if !strings.Contains(m.status, "hand edit") {
		t.Errorf("the status is %q, want it to name the hand edit", m.status)
	}
}

// TestStatusDoesNotClaimAPushWithoutARemote: a store with nowhere to push
// comes back with Pushed false, and the status line says only what happened.
func TestStatusDoesNotClaimAPushWithoutARemote(t *testing.T) {
	if got := describe(epilogue.Result{Committed: true}); strings.Contains(got, "pushed") {
		t.Errorf("the status is %q, want no push claimed for a result that pushed nothing", got)
	}
	if got := describe(epilogue.Result{Committed: true, Pushed: true}); !strings.Contains(got, "pushed") {
		t.Errorf("the status is %q, want the push reported when one happened", got)
	}
}

// TestActionsOnAnEmptyListDoNothing: x and d have nothing under the cursor.
func TestActionsOnAnEmptyListDoNothing(t *testing.T) {
	m := newModel(t, newStore(t))
	for _, key := range []string{"x", "d"} {
		if cmd := press(m, key); cmd != nil {
			t.Errorf("%s on an empty list returned a command", key)
		}
	}
}

// appendLine adds a line to a file the way a person editing it would, moving
// its modification time past the updated timestamp inside it.
func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("\n" + line + "\n"); err != nil {
		t.Fatal(err)
	}
}

// findTitle returns the loaded entry with the given title.
func findTitle(t *testing.T, m *Model, title string) store.Entry {
	t.Helper()
	for _, e := range m.Entries() {
		if e.Item.Title == title {
			return e
		}
	}
	t.Fatalf("no item titled %q in %v", title, titles(m))
	return store.Entry{}
}

// indexOf is where a title sits in the loaded listing.
func indexOf(m *Model, title string) int {
	for i, e := range m.Entries() {
		if e.Item.Title == title {
			return i
		}
	}
	return 0
}

// crossedRule reports whether a title renders below the done rule.
func crossedRule(t *testing.T, m *Model, title string) bool {
	t.Helper()
	ruleAt := -1
	for i, line := range lines(m.View()) {
		if strings.Contains(line, doneRule) {
			ruleAt = i
		}
		if strings.Contains(line, title) {
			return ruleAt >= 0 && i > ruleAt
		}
	}
	return false
}
