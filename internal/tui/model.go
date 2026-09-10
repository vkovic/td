// Package tui is td's terminal interface: one list of items in a tmux pane
// beside a Claude Code session, refreshed as the store changes underneath it.
//
// The package calls internal/store and internal/epilogue in process and never
// shells out to the td binary. The skill, the CLI and the TUI do not talk to
// each other; every one of them goes through the store.
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/store"
)

// ScopeMode is which lists the TUI is showing: the scope resolved for the
// working directory, the global list, or every scope merged.
type ScopeMode int

const (
	// ModeScope shows the scope a .td marker or the scope flags resolved to.
	ModeScope ScopeMode = iota
	// ModeGlobal shows the global list.
	ModeGlobal
	// ModeAll merges every scope, as td ls --all does.
	ModeAll
)

// String names the mode as the footer reports it.
func (m ScopeMode) String() string {
	switch m {
	case ModeGlobal:
		return "global"
	case ModeAll:
		return "all"
	default:
		return "scope"
	}
}

// Model is the Bubble Tea model: the loaded listing, where the cursor sits, and
// what the store and configuration behind it are.
type Model struct {
	store *store.Store
	cfg   config.Config
	scope store.ScopeChoice

	mode    ScopeMode
	entries []store.Entry
	cursor  int

	// warn holds the joined error from a listing that skipped an unparsable
	// file. It is shown as a warning line: the rest of the list is still worth
	// seeing, and the TUI is how you would go open the broken file and fix it.
	warn error

	// now is the clock, so a test can pin what counts as overdue and what a
	// relative timestamp reads as.
	now func() time.Time

	styles styles

	width, height int
	quitting      bool
}

// Options are what New needs. Store, Config and Scope come from the same
// resolution every td command already performs.
type Options struct {
	Store  *store.Store
	Config config.Config
	Scope  store.ScopeChoice

	// Renderer decides how styles are emitted. A nil renderer uses Lip Gloss's
	// default, which reads the real terminal; a test passes one with a forced
	// color profile so a style is observable in the rendered string.
	Renderer *lipgloss.Renderer

	// Now overrides the clock. Nil means time.Now.
	Now func() time.Time
}

// New builds a model over an already open store and loads the first listing.
func New(opts Options) (*Model, error) {
	m := &Model{
		store:  opts.Store,
		cfg:    opts.Config,
		scope:  opts.Scope,
		now:    opts.Now,
		styles: newStyles(opts.Renderer),
		// A store opened on the global scope has no project list to show, so
		// the TUI starts where the CLI would have listed.
		mode: ModeScope,
	}
	if m.now == nil {
		m.now = time.Now
	}
	if opts.Scope.Scope.IsGlobal() {
		m.mode = ModeGlobal
	}
	if err := m.reload(); err != nil {
		return nil, err
	}
	return m, nil
}

// Init is Bubble Tea's startup hook. The first listing is already loaded, so
// there is nothing to do until the watcher arrives.
func (m *Model) Init() tea.Cmd { return nil }

// Update handles one message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

// handleKey applies a keystroke.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return tea.Quit
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	}
	return nil
}

// moveCursor steps the selection, clamping at both ends rather than wrapping:
// a held j must stop at the last item, not cycle back to the first.
func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	m.clampCursor()
}

// clampCursor keeps the cursor on a row that exists, which is also what a
// reload needs after the listing has shrunk underneath it.
func (m *Model) clampCursor() {
	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// reload re-reads the listing for the current mode and re-sorts it.
//
// A listing that skipped an unparsable file comes back with entries and an
// error together. That is a warning, exactly as it is for td ls, and only a
// listing that produced nothing at all is a failure.
func (m *Model) reload() error {
	var (
		entries []store.Entry
		err     error
	)
	if m.mode == ModeAll {
		entries, err = m.store.ListAll(store.Active)
	} else {
		entries, err = m.store.List(m.currentScope(), store.Active)
	}
	if err != nil {
		if len(entries) == 0 {
			return err
		}
		m.warn = err
	} else {
		m.warn = nil
	}

	store.SortEntries(entries)
	m.entries = entries
	m.clampCursor()
	return nil
}

// currentScope is the scope a non-merged mode lists.
func (m *Model) currentScope() store.Scope {
	if m.mode == ModeGlobal {
		return store.Global
	}
	return m.scope.Scope
}

// Entries is the loaded listing, in display order.
func (m *Model) Entries() []store.Entry { return m.entries }

// Cursor is the index of the selected row.
func (m *Model) Cursor() int { return m.cursor }

// Selected is the entry under the cursor, or nil when the list is empty.
func (m *Model) Selected() *store.Entry {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return nil
	}
	return &m.entries[m.cursor]
}
