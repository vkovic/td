package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/store"
)

// toggleDone stamps or clears done_at on the selected item, which is what makes
// an item done. This is td done and td undo's own sequence: the stamp and the
// updated timestamp are the same instant, and the item stays in the list until
// the archive sweep moves it out, done_ttl_days after it was completed.
func (m *Model) toggleDone() tea.Cmd {
	e := m.Selected()
	if e == nil {
		return nil
	}

	it := e.Item
	stamp := m.now()
	action := "done"
	if it.Done() {
		it.DoneAt = nil
		action = "undo"
	} else {
		at := stamp
		it.DoneAt = &at
	}
	it.Updated = stamp

	if _, err := m.store.Save(e.Ref.Scope, e.Ref.Area, it); err != nil {
		m.err = err
		return nil
	}
	return m.runEpilogue(commitMessage(action, it))
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

	it := e.Item
	it.Updated = m.now()
	if _, err := m.store.Move(e.Ref, it, e.Ref.Scope, store.Deleted); err != nil {
		m.err = err
		return nil
	}
	return m.runEpilogue(commitMessage("remove", it))
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
