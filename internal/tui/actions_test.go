package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/epilogue"
	"github.com/vkovic/td/internal/store"
)

// TestToggleDoneMarksAndReopens: space stamps done_at and space again clears it, and
// the row crosses the rule each time because the listing is re-read after.
func TestToggleDoneMarksAndReopens(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "finish me", updated: ago(2)})
	save(t, s, item{id: "bbb", title: "leave open", updated: ago(1)})

	m := newModel(t, s)
	// The cursor starts on the most recently updated item, which is the one
	// that must stay open, so step onto the other.
	press(m, "j")

	drain(t, m, press(m, " "))
	e := findTitle(t, m, "finish me")
	if !e.Item.Done() {
		t.Fatal("space did not mark the item done")
	}
	if got := strings.Join(titles(m), ","); got != "leave open,finish me" {
		t.Errorf("the listing is %s, want the done item below the open one", got)
	}
	if !crossedRule(t, m, "finish me") {
		t.Error("the done item is not below the rule")
	}

	// The cursor stayed among the open rows, on the one above, rather than
	// following the item below the rule, so step back onto it before undoing it.
	m.cursor = indexOf(m, "finish me")
	drain(t, m, press(m, " "))
	if findTitle(t, m, "finish me").Item.Done() {
		t.Error("space again did not reopen the item")
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

	drain(t, m, press(m, " "))

	if got, want := commitCount(t, s), before+1; got != want {
		t.Errorf("space left %d commits, want %d", got, want)
	}
}

// TestRemoveMovesToDeleted: x takes the item out of the list and leaves the
// file under deleted/, so an accident is recoverable from the directory.
func TestRemoveMovesToDeleted(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "delete me", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "keep me", updated: ago(2)})

	m := newModel(t, s)
	drain(t, m, press(m, "x"))

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

	drain(t, m, press(m, "x"))

	if got, want := commitCount(t, s), before+1; got != want {
		t.Errorf("x left %d commits, want %d", got, want)
	}
}

// TestGAndDDoNothing: g opened the picker and d deleted before they were
// rebound, and neither was kept as an alias. A key someone still reaches for
// out of habit must not quietly act.
func TestGAndDDoNothing(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "do not touch me", updated: ago(1)})

	m := newModel(t, s)
	for _, key := range []string{"g", "d"} {
		if cmd := press(m, key); cmd != nil {
			t.Errorf("%s returned a command", key)
		}
	}
	if m.picker.open {
		t.Error("g opened the picker")
	}
	if e := findTitle(t, m, "do not touch me"); e.Ref.Area != store.Active {
		t.Errorf("d filed the item under %q, want it still active", e.Ref.Area)
	}
}

// TestMutationsRunInOrder: two keystrokes back to back both complete, in the
// order they were pressed, and the store is left consistent.
func TestMutationsRunInOrder(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "first", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "second", updated: ago(2)})

	m := newModel(t, s)
	drain(t, m, press(m, " ")) // done: first
	m.cursor = indexOf(m, "second")
	drain(t, m, press(m, "x")) // remove: second

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
	if cmd := press(m, " "); cmd == nil {
		t.Fatal("space returned no command")
	}
	if !m.Busy() {
		t.Fatal("space did not mark the model busy")
	}
	for _, key := range []string{" ", "p", "x", "r", "e", "a"} {
		if cmd := press(m, key); cmd != nil {
			t.Errorf("%q ran while an epilogue was in flight", key)
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

// TestMutatingKeysDoNotForceCommit: space mirrors td done, which honors the
// configuration. Only the key asked for outright overrides it.
func TestMutatingKeysDoNotForceCommit(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "finish me", updated: ago(1)})

	m := newModel(t, s, func(o *Options) {
		o.Config = config.Default()
		o.Config.AutoCommit = false
	})
	before := commitCount(t, s)

	drain(t, m, press(m, " "))

	if got := commitCount(t, s); got != before {
		t.Errorf("space with auto_commit off left %d commits, want %d", got, before)
	}
	if !findTitle(t, m, "finish me").Item.Done() {
		t.Error("space did not mark the item done when the commit was skipped")
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

// TestTheDefaultClockDoesNotForgeAHandEdit: the pane's own writes must not
// look like edits made outside td.
//
// Marshal writes updated as whole-second RFC 3339 while Save pins the file's
// mtime to the in-memory value, so a clock carrying nanoseconds leaves mtime a
// fraction of a second past the updated it reads back as — which is exactly
// what HandEdited calls an outside edit. The pane then reported "1 hand edit
// recorded" for the item the user had just marked done, and rewrote the file to
// "catch up" a timestamp it had written itself a moment earlier.
//
// This test deliberately does not inject a clock. Every other test here pins
// one, and a pinned clock is a whole second, which is how the production
// default went three milestones without being exercised.
func TestTheDefaultClockDoesNotForgeAHandEdit(t *testing.T) {
	s := newStore(t)
	ref := save(t, s, item{id: "aaa", title: "toggle me", updated: ago(48)})

	m, err := New(Options{
		Store:    s,
		Config:   config.Default(),
		Scope:    store.ScopeChoice{Scope: store.Global},
		Renderer: testRenderer(),
		// Now is left nil on purpose: this is the clock a real pane runs on.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	drain(t, m, press(m, " "))

	entries, err := s.List(store.Global, store.Active)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Ref.Path != ref.Path {
			continue
		}
		edited, err := store.HandEdited(e)
		if err != nil {
			t.Fatal(err)
		}
		if edited {
			t.Errorf("the pane's own write reads as a hand edit: mtime is past the updated it wrote")
		}
	}
	if strings.Contains(m.status, "hand edit") {
		t.Errorf("the status claims %q after a toggle nobody hand edited", m.status)
	}
}

// TestStatusDoesNotClaimAPushWithoutARemote: a store with nowhere to push
// comes back with Pushed false, and the status line says only what happened.
func TestStatusDoesNotClaimAPushWithoutARemote(t *testing.T) {
	if got := (epilogue.Result{Committed: true}).Describe(); strings.Contains(got, "pushed") {
		t.Errorf("the status is %q, want no push claimed for a result that pushed nothing", got)
	}
	if got := (epilogue.Result{Committed: true, Pushed: true}).Describe(); !strings.Contains(got, "pushed") {
		t.Errorf("the status is %q, want the push reported when one happened", got)
	}
}

// TestActionsOnAnEmptyListDoNothing: space and x have nothing under the cursor.
func TestActionsOnAnEmptyListDoNothing(t *testing.T) {
	m := newModel(t, newStore(t))
	for _, key := range []string{" ", "p", "x"} {
		if cmd := press(m, key); cmd != nil {
			t.Errorf("%q on an empty list returned a command", key)
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

// TestAnActionSurvivesTheRowsBeingRebuilt: space writes the change and puts the
// result back on the row, but the listing is not re-read until the epilogue has
// committed and pushed. Anything that rebuilds the visible rows inside that
// window rebuilds them from the loaded listing, so the loaded listing has to
// carry the change too — /, t and esc all rebuild, none of them waits, and none
// is behind the busy gate.
func TestAnActionSurvivesTheRowsBeingRebuilt(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "finish me", updated: ago(2)})
	save(t, s, item{id: "bbb", title: "finish this too", updated: ago(1)})

	m := newModel(t, s)
	m.filters.title = "finish"
	m.applyFilters()
	m.cursor = indexOf(m, "finish me")

	// The epilogue command is deliberately not drained: this is the window
	// between the key press and the reload.
	press(m, " ")
	m.applyFilters()

	if !findTitle(t, m, "finish me").Item.Done() {
		t.Error("the item went back to open when the rows were rebuilt")
	}
	for _, e := range m.Loaded() {
		if e.Item.ID == "aaa" && !e.Item.Done() {
			t.Error("the loaded listing never learned the item was done")
		}
	}
}

// TestARemovalSurvivesTheRowsBeingRebuilt: the same window, for x. The row
// leaves the loaded listing on the key press, not at the reload, so a rebuild
// inside that window must not bring it back — a row still on screen is one an
// action taken from it would resolve in the wrong area.
func TestARemovalSurvivesTheRowsBeingRebuilt(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "delete me", updated: ago(2)})
	save(t, s, item{id: "bbb", title: "delete nothing", updated: ago(1)})

	m := newModel(t, s)
	m.filters.title = "delete"
	m.applyFilters()
	m.cursor = indexOf(m, "delete me")

	cmd := press(m, "x")
	m.applyFilters()
	assertRemoved(t, m, "aaa", "on the key press")
	if !trashed(t, s, "delete-me") {
		t.Error("the item is not filed under deleted/ after the key press")
	}

	drain(t, m, cmd)
	assertRemoved(t, m, "aaa", "after the reload")
	if !trashed(t, s, "delete-me") {
		t.Error("the item is not filed under deleted/ after the reload")
	}
}

// assertRemoved fails when the item is still in the loaded listing or on screen.
func assertRemoved(t *testing.T, m *Model, id, when string) {
	t.Helper()
	for _, e := range m.Loaded() {
		if e.Item.ID == id {
			t.Errorf("%s the loaded listing still holds %q, filed under %q", when, e.Item.Title, e.Ref.Area)
		}
	}
	for _, e := range m.Entries() {
		if e.Item.ID == id {
			t.Errorf("%s the pane still shows %q", when, e.Item.Title)
		}
	}
}

// trashed reports whether a file whose name contains slug is in the global
// scope's deleted/ directory.
func trashed(t *testing.T, s *store.Store, slug string) bool {
	t.Helper()
	deleted := s.Dir(store.Global, store.Deleted)
	names, err := os.ReadDir(deleted)
	if err != nil {
		t.Fatalf("reading %s: %v", deleted, err)
	}
	for _, n := range names {
		if strings.Contains(n.Name(), slug) {
			return true
		}
	}
	return false
}

// TestTheCursorLandsOnANeighbourInItsSection: space and x take the row out of
// its section, and the cursor stays behind among the rows you were working
// through — the next one, or the previous one from the section's last row.
//
// Where it lands is checked twice: on the key press, and again after the
// epilogue's reload, which re-anchors on the selected id and must keep the
// neighbour rather than pull the cursor back onto the item that moved.
//
// The pane is too short for every row, which is also what keeps the older
// regression covered: a cursor on a row the windowing declines to draw shows no
// marker anywhere.
func TestTheCursorLandsOnANeighbourInItsSection(t *testing.T) {
	cases := []struct {
		key, on, want string
	}{
		{" ", "alpha", "bravo"},
		{" ", "bravo", "charlie"},
		{" ", "charlie", "bravo"},
		{" ", "delta", "echo"},
		{" ", "echo", "foxtrot"},
		{" ", "foxtrot", "echo"},
		{"x", "alpha", "bravo"},
		{"x", "bravo", "charlie"},
		{"x", "charlie", "bravo"},
		{"x", "delta", "echo"},
		{"x", "echo", "foxtrot"},
		{"x", "foxtrot", "echo"},
	}
	for _, c := range cases {
		t.Run(keyName(c.key)+" on "+c.on, func(t *testing.T) {
			s := newStore(t)
			for i, title := range []string{"alpha", "bravo", "charlie"} {
				save(t, s, item{id: string(rune('a'+i)) + "aa", title: title, updated: ago(i + 1)})
			}
			for i, title := range []string{"delta", "echo", "foxtrot"} {
				save(t, s, item{id: string(rune('d'+i)) + "aa", title: title,
					updated: ago(i + 1), doneAt: done(ago(i + 1))})
			}

			m := newModel(t, s)
			m.Update(tea.WindowSizeMsg{Width: 60, Height: 5})
			m.cursor = indexOf(m, c.on)

			cmd := press(m, c.key)
			assertSelected(t, m, c.want, "on the key press")
			drain(t, m, cmd)
			assertSelected(t, m, c.want, "after the reload")
			if !strings.Contains(m.View(), "❯") {
				t.Errorf("the cursor is not drawn anywhere:\n%s", m.View())
			}
		})
	}
}

// TestTheCursorOnARowAloneInItsSection: with no neighbour to land on, space
// follows the item across the rule — it is all that is left of what you were
// on — and x hands the cursor to the nearest row on the other side.
func TestTheCursorOnARowAloneInItsSection(t *testing.T) {
	cases := []struct {
		name, key, on, want string
	}{
		{"space on the only open row follows it below the rule", " ", "open", "open"},
		{"space on the only done row follows it above the rule", " ", "done", "done"},
		{"x on the only open row lands on the first done row", "x", "open", "done"},
		{"x on the only done row lands on the last open row", "x", "done", "open"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t)
			// The row under test is alone in its section and the other section
			// holds two, so "nearest" is told apart from "first" or "last".
			if c.on == "open" {
				save(t, s, item{id: "aaa", title: "open", updated: ago(3)})
				save(t, s, item{id: "bbb", title: "done", updated: ago(1), doneAt: done(ago(1))})
				save(t, s, item{id: "ccc", title: "done earlier", updated: ago(2), doneAt: done(ago(2))})
			} else {
				save(t, s, item{id: "aaa", title: "open later", updated: ago(1)})
				save(t, s, item{id: "bbb", title: "open", updated: ago(2)})
				save(t, s, item{id: "ccc", title: "done", updated: ago(3), doneAt: done(ago(3))})
			}

			m := newModel(t, s)
			m.cursor = indexOf(m, c.on)

			cmd := press(m, c.key)
			assertSelected(t, m, c.want, "on the key press")
			drain(t, m, cmd)
			assertSelected(t, m, c.want, "after the reload")
		})
	}
}

// TestTheCursorAfterRemovingTheLastRow: x on a list of one leaves nothing to
// land on, and nothing selected once the reload drops the row.
func TestTheCursorAfterRemovingTheLastRow(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "only", updated: ago(1)})

	m := newModel(t, s)
	drain(t, m, press(m, "x"))

	if e := m.Selected(); e != nil {
		t.Errorf("the cursor is on %q, want nothing selected in an empty list", e.Item.Title)
	}
}

// TestTheCursorNeighbourIsCountedOnScreen: a filter narrows what the neighbour
// is. The row after the selected one in the loaded listing is hidden, so the
// cursor skips it for the next row that is on screen.
func TestTheCursorNeighbourIsCountedOnScreen(t *testing.T) {
	for _, key := range []string{" ", "x"} {
		t.Run(keyName(key), func(t *testing.T) {
			s := newStore(t)
			save(t, s, item{id: "aaa", title: "work first", updated: ago(1)})
			save(t, s, item{id: "bbb", title: "home", updated: ago(2)})
			save(t, s, item{id: "ccc", title: "work second", updated: ago(3)})

			m := newModel(t, s)
			m.filters.title = "work"
			m.applyFilters()
			m.cursor = indexOf(m, "work first")

			cmd := press(m, key)
			assertSelected(t, m, "work second", "on the key press")
			drain(t, m, cmd)
			assertSelected(t, m, "work second", "after the reload")
		})
	}
}

// assertSelected fails when the cursor is not on the row titled want.
func assertSelected(t *testing.T, m *Model, want, when string) {
	t.Helper()
	e := m.Selected()
	if e == nil {
		t.Errorf("%s nothing is selected, want %q", when, want)
		return
	}
	if e.Item.Title != want {
		t.Errorf("%s the cursor is on %q, want %q in %v", when, e.Item.Title, want, titles(m))
	}
}

// keyName spells a pressed key for a subtest name, where a bare space vanishes.
func keyName(key string) string {
	if key == " " {
		return "space"
	}
	return key
}
