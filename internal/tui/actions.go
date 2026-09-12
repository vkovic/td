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
	return m.applied(e, res)
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
	return m.applied(e, res)
}

// applied puts what the service wrote back on the row and runs the epilogue.
//
// The row has to be updated here. The service resolves the id and works on its
// own copy of the item, so the entry the list is holding is the one from before
// the change, and the reload that would correct it only happens once the
// epilogue finishes. Without this the pane would go on showing an item as open
// for as long as a commit and a push take.
func (m *Model) applied(row *store.Entry, res task.Result) tea.Cmd {
	if len(res.Entries) > 0 {
		*row = res.Entries[0]
	}
	return m.runEpilogue(res.Message)
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
