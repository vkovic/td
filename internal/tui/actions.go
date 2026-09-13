package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/store"
	"github.com/vkovic/td/internal/task"
)

// toggleDone completes the selected item, or reopens it. It is td done and
// td undo, because it is the same service call: the key press only decides
// which way, from what the row already is.
//
// The row is looked up by the scope it is filed under rather than the pane's
// own, because the merged view spans scopes and the selected row may not be in
// the one this directory resolved to.
func (m *Model) toggleDone() tea.Cmd {
	e := m.Selected()
	if e == nil {
		return nil
	}
	landing := m.landing(false)
	res, err := m.tasks.SetDone(task.StatusRequest{
		Scope: e.Ref.Scope,
		IDs:   []string{e.Item.ID},
		Done:  !e.Item.Done(),
	})
	if err != nil {
		m.err = err
		return nil
	}
	return m.applied(res, landing)
}

// togglePin pins the selected item, or unpins it: td pin and td unpin, the way
// toggleDone is td done and td undo.
//
// The cursor follows the item rather than landing on a neighbour. A pin
// reorders a row within its section without taking it out, so the row you
// were on is still the one you are working on.
func (m *Model) togglePin() tea.Cmd {
	e := m.Selected()
	if e == nil {
		return nil
	}
	res, err := m.tasks.SetPinned(task.PinRequest{
		Scope:  e.Ref.Scope,
		IDs:    []string{e.Item.ID},
		Pinned: !e.Item.Pinned,
	})
	if err != nil {
		m.err = err
		return nil
	}
	return m.applied(res, e.Item.ID)
}

// removeItem moves the selected item to the trash, as td rm does.
//
// deleted/ is git-ignored, so this is the one action whose result is not in the
// history: recovering it means going to the directory, not to git.
func (m *Model) removeItem() tea.Cmd {
	e := m.Selected()
	if e == nil {
		return nil
	}
	landing := m.landing(true)
	res, err := m.tasks.Remove(task.MoveRequest{Scope: e.Ref.Scope, IDs: []string{e.Item.ID}})
	if err != nil {
		m.err = err
		return nil
	}
	return m.applied(res, landing)
}

// landing names the row the cursor moves to when the selected row is about to
// leave its section, by being marked done, reopened, or removed.
//
// It is the next row in the same section, or the previous one when the
// selected row is the section's last. Working down a list with space or x then
// keeps you among the rows you were working through, instead of following the
// item below the rule or dropping onto whatever slid into its place.
//
// A row alone in its section has no neighbour there. An item that stays in
// the listing is followed, since it is the only thing left of what you were
// on. An item that leaves it hands the cursor to the nearest row across the
// rule: the row after it, else the row before.
//
// Neighbours are counted in shown, so a filter narrows them to what is on
// screen. The rows are sorted open before done, so two adjacent rows with the
// same done state are in the same section.
func (m *Model) landing(leaves bool) string {
	sel := m.Selected()
	if sel == nil {
		return ""
	}
	next, prev := m.cursor+1, m.cursor-1
	sameSection := func(i int) bool {
		return i >= 0 && i < len(m.shown) && m.shown[i].Item.Done() == sel.Item.Done()
	}
	switch {
	case sameSection(next):
		return m.shown[next].Item.ID
	case sameSection(prev):
		return m.shown[prev].Item.ID
	case !leaves:
		return sel.Item.ID
	case next < len(m.shown):
		return m.shown[next].Item.ID
	case prev >= 0:
		return m.shown[prev].Item.ID
	}
	return ""
}

// applied puts what the service wrote back into the listing, moves the cursor
// to the item named by anchor, and runs the epilogue.
//
// It has to go into the loaded listing and not just the visible rows. The
// service resolves the id and works on its own copy of the item, so what the
// pane holds is the item as it was before the change, and the reload that would
// correct it does not happen until the epilogue has committed and pushed.
// Anything that rebuilds the rows inside that window rebuilds them from the
// loaded listing — /, t and esc all do, and none of them waits — so a change
// written only to the rows is undone by the next keystroke.
//
// Re-sorting is part of putting it back. An item that just became done belongs
// below the rule, and the windowing in view.go derives the open and done cursor
// positions from the listing being ordered that way: leaving a done item among
// the open ones puts the cursor on a row the pane will not draw.
//
// The cursor moves here, on the key press, and not after the reload. The
// reload re-anchors on whatever is selected by then, so the row picked here is
// the row it keeps.
func (m *Model) applied(res task.Result, anchor string) tea.Cmd {
	if len(res.Entries) > 0 {
		m.replace(res.Entries[0], anchor)
	}
	return m.runEpilogue(res.Message)
}

// replace swaps a changed entry into the loaded listing, reorders it, and
// rebuilds the rows around the item named by anchor.
//
// An entry now filed under deleted/ is dropped instead. The pane lists only
// active items, so the reload would drop it anyway; dropping it on the key
// press means the row leaves with the cursor, rather than lingering until the
// epilogue has committed and pushed.
func (m *Model) replace(e store.Entry, anchor string) {
	for i := range m.entries {
		if m.entries[i].Item.ID != e.Item.ID {
			continue
		}
		if e.Ref.Area == store.Deleted {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
		} else {
			m.entries[i] = e
		}
		break
	}
	store.SortEntries(m.entries)
	m.rebuild(anchor)
}

// refresh runs the epilogue because it was asked for, rather than because
// something changed.
//
// Asking for it outright overrides the settings that would otherwise skip the
// commit and the push, the same way td commit commits with auto_commit off and
// td push pushes with auto_push off. A key you pressed should do what it says.
// The keys that change an item do not force anything: those mirror td done and
// td rm, which honor the configuration.
func (m *Model) refresh() tea.Cmd {
	return m.runEpilogueWith(forceWrites(m.cfg), "")
}

// forceWrites turns the commit and push gates on, for an epilogue that was
// asked for outright.
func forceWrites(cfg config.Config) config.Config {
	cfg.AutoCommit, cfg.AutoPush = true, true
	return cfg
}
