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
	res, err := m.tasks.SetDone(task.StatusRequest{
		Scope: e.Ref.Scope,
		IDs:   []string{e.Item.ID},
		Done:  !e.Item.Done(),
	})
	if err != nil {
		m.err = err
		return nil
	}
	return m.applied(res)
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
	res, err := m.tasks.Remove(task.MoveRequest{Scope: e.Ref.Scope, IDs: []string{e.Item.ID}})
	if err != nil {
		m.err = err
		return nil
	}
	return m.applied(res)
}

// applied puts what the service wrote back into the listing and runs the
// epilogue.
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
func (m *Model) applied(res task.Result) tea.Cmd {
	if len(res.Entries) > 0 {
		m.replace(res.Entries[0])
	}
	return m.runEpilogue(res.Message)
}

// replace swaps a changed entry into the loaded listing, reorders it, and
// rebuilds the rows around the item the cursor was on.
func (m *Model) replace(e store.Entry) {
	var anchor string
	if sel := m.Selected(); sel != nil {
		anchor = sel.Item.ID
	}
	for i := range m.entries {
		if m.entries[i].Item.ID == e.Item.ID {
			m.entries[i] = e
			break
		}
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
