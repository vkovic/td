// Package tui is td's terminal interface: one list of items in a tmux pane
// beside a Claude Code session, refreshed as the store changes underneath it.
//
// The package calls internal/store and internal/epilogue in process and never
// shells out to the td binary. The skill, the CLI and the TUI do not talk to
// each other; every one of them goes through the store.
package tui

import (
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/epilogue"
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

	mode ScopeMode

	// entries is everything the current scope holds, and shown is what
	// survives the filters. The cursor indexes shown, because the cursor is a
	// thing on screen.
	entries []store.Entry
	shown   []store.Entry
	cursor  int

	// filters narrow what is shown. They are view state, not part of the
	// listing, so a reload re-applies them rather than clearing them.
	filters filters

	// warn holds the joined error from a listing that skipped an unparsable
	// file. It is shown as a warning line: the rest of the list is still worth
	// seeing, and the TUI is how you would go open the broken file and fix it.
	warn error

	// prompt is the inline line editor, when one is open.
	prompt prompt

	// status is what the last epilogue did, and err the last failure that was
	// worth showing rather than fatal. Both are footer text.
	status string
	err    error

	// busy is true while an epilogue is in flight. Only one runs at a time:
	// the epilogue takes a blocking flock on the store, so a second one
	// dispatched while the first holds it would wait on a lock this process
	// already owns — and the keystroke that dispatched it would look ignored.
	busy bool

	// now is the clock, so a test can pin what counts as overdue and what a
	// relative timestamp reads as.
	now func() time.Time

	// watch reports that something under the store changed. It is nil when the
	// model was built without one, which is what a test that drives reloads by
	// hand wants.
	watch *watcher

	// flashUntil is when the "updated externally" note stops showing, and
	// selfWriteUntil is how long a reload is attributed to this pane's own
	// epilogue rather than to someone else. The second is a suppression window
	// on the note alone: the reload itself always happens, because swallowing
	// one would mean missing a change that landed mid-epilogue.
	flashUntil     time.Time
	selfWriteUntil time.Time

	// exec is how the terminal is handed to another program. It is a field so
	// a test can run the editor synchronously rather than going through Bubble
	// Tea's terminal handover, which needs a real one.
	exec func(*exec.Cmd, tea.ExecCallback) tea.Cmd

	styles styles

	width, height int
	quitting      bool

	// showHelp is whether the key overlay is covering the list.
	showHelp bool
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

	// Now overrides the clock. Nil means store.Now, which every surface uses
	// and which truncates to the second — see store.Now for why that matters.
	Now func() time.Time

	// Exec overrides how a child program is run. Nil means tea.ExecProcess.
	Exec func(*exec.Cmd, tea.ExecCallback) tea.Cmd

	// Watch turns on the file watcher. A test that drives reloads by hand
	// leaves it off, so nothing races its own fixtures.
	Watch bool

	// Debounce overrides how long a burst of changes is collected for. Zero
	// means defaultDebounce.
	Debounce time.Duration
}

// New builds a model over an already open store and loads the first listing.
func New(opts Options) (*Model, error) {
	m := &Model{
		store:  opts.Store,
		cfg:    opts.Config,
		scope:  opts.Scope,
		now:    opts.Now,
		exec:   opts.Exec,
		styles: newStyles(opts.Renderer),
		// A store opened on the global scope has no project list to show, so
		// the TUI starts where the CLI would have listed.
		mode: ModeScope,
	}
	if m.now == nil {
		// store.Now, not time.Now: a clock carrying nanoseconds makes every
		// write td does look like a hand edit to the next bump.
		m.now = store.Now
	}
	if m.exec == nil {
		m.exec = tea.ExecProcess
	}
	if opts.Scope.Scope.IsGlobal() {
		m.mode = ModeGlobal
	}
	if err := m.reload(); err != nil {
		return nil, err
	}
	if opts.Watch {
		w, err := newWatcher(opts.Store, opts.Debounce)
		if err != nil {
			return nil, err
		}
		m.watch = w
	}
	return m, nil
}

// Close releases the watcher. A model built without one has nothing to do.
func (m *Model) Close() error {
	if m.watch == nil {
		return nil
	}
	return m.watch.Close()
}

// Init is Bubble Tea's startup hook. The first listing is already loaded, so
// the only thing to start is the wait for the first change.
func (m *Model) Init() tea.Cmd { return m.awaitChange() }

// awaitChange blocks a command goroutine on the watcher until something under
// the store changes. Each pulse produces one message and one fresh wait, which
// is how a channel becomes a stream of Bubble Tea messages.
func (m *Model) awaitChange() tea.Cmd {
	if m.watch == nil {
		return nil
	}
	pulses := m.watch.Pulses()
	return func() tea.Msg {
		if _, ok := <-pulses; !ok {
			return nil
		}
		return storeChangedMsg{}
	}
}

// storeChangedMsg says that something under the store changed. It says nothing
// about what, and it is never acted on beyond re-reading: the watcher must not
// bump, sweep, commit or push, or two panes watching one store would drive each
// other in a loop.
type storeChangedMsg struct{}

// flashClearedMsg retires the "updated externally" note.
type flashClearedMsg struct{}

// externalFlash is what the footer says when the list changed underneath it.
const externalFlash = "updated externally"

// flashFor is how long that note stays up.
const flashFor = 3 * time.Second

// selfWriteFor is how long after this pane's own epilogue a change is still
// attributed to it rather than to someone else.
const selfWriteFor = time.Second

// afterStoreChanged re-reads the list and, when the change was not this pane's
// doing, says so.
func (m *Model) afterStoreChanged() tea.Cmd {
	if err := m.reload(); err != nil {
		m.err = err
	}
	cmds := []tea.Cmd{m.awaitChange()}
	if now := m.now(); now.After(m.selfWriteUntil) {
		m.flashUntil = now.Add(flashFor)
		cmds = append(cmds, tea.Tick(flashFor, func(time.Time) tea.Msg {
			return flashClearedMsg{}
		}))
	}
	return tea.Batch(cmds...)
}

// flashing reports whether the external-change note is still showing.
func (m *Model) flashing() bool { return m.now().Before(m.flashUntil) }

// Update handles one message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.prompt.open() {
			return m, m.handlePromptKey(msg)
		}
		return m, m.handleKey(msg)
	case editorFinishedMsg:
		return m, m.afterEditor(msg)
	case epilogueDoneMsg:
		m.afterEpilogue(msg)
	case storeChangedMsg:
		return m, m.afterStoreChanged()
	case flashClearedMsg:
		m.flashUntil = time.Time{}
	}
	return m, nil
}

// handleKey applies a keystroke, through the same table the help is written
// from. A key that is not in the table does nothing, and a key in the table is
// documented by construction.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	pressed := msg.String()
	// The overlay is modal: it answers only the keys that close it, so nothing
	// is edited by a keystroke aimed at a screen that is covering the list.
	if m.showHelp {
		if pressed == "?" || pressed == "esc" || pressed == "q" {
			m.showHelp = false
		}
		return nil
	}
	for _, b := range keyMap() {
		for _, key := range b.keys {
			if key == pressed {
				return b.run(m)
			}
		}
	}
	return nil
}

// startAdd opens the prompt that asks a new item's title.
func (m *Model) startAdd() tea.Cmd {
	if m.busy {
		m.status = "still working"
		return nil
	}
	m.prompt = prompt{kind: promptAdd, label: "add"}
	return nil
}

// startFilter opens the prompt that narrows by title, offering whatever is
// already filtering so narrowing further does not mean retyping it.
func (m *Model) startFilter() tea.Cmd {
	m.prompt = prompt{kind: promptFilter, label: "filter", value: m.filters.title}
	return nil
}

// editSelected opens the item under the cursor, when there is one.
func (m *Model) editSelected() tea.Cmd {
	if e := m.Selected(); e != nil {
		return m.editItem("edit", *e)
	}
	return nil
}

// nextScope moves to the next list.
func (m *Model) nextScope() tea.Cmd {
	if err := m.cycleScope(); err != nil {
		m.err = err
	}
	return nil
}

// clearFilters puts the whole listing back on screen.
func (m *Model) clearFilters() {
	m.filters = filters{}
	m.applyFilters()
}

// handlePromptKey drives the inline line editor. Enter submits, esc abandons,
// and everything else types.
func (m *Model) handlePromptKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.prompt = prompt{}
	case tea.KeyEnter:
		p := m.prompt
		m.prompt = prompt{}
		return m.submitPrompt(p)
	case tea.KeyBackspace:
		m.prompt.backspace()
	case tea.KeyRunes, tea.KeySpace:
		m.prompt.typed(msg)
	}
	return nil
}

// submitPrompt acts on a finished prompt.
func (m *Model) submitPrompt(p prompt) tea.Cmd {
	switch p.kind {
	case promptAdd:
		return m.addItem(p.value)
	case promptFilter:
		m.filters.title = strings.TrimSpace(p.value)
		m.applyFilters()
	}
	return nil
}

// afterEditor is what happens once the editor has exited: the epilogue runs,
// which is what records a hand edit, sweeps, commits and pushes. It runs even
// when the editor wrote nothing, because the epilogue is a no-op in that case
// and deciding otherwise would mean the TUI reading the file to guess.
func (m *Model) afterEditor(msg editorFinishedMsg) tea.Cmd {
	if msg.err != nil {
		m.err = msg.err
		return nil
	}
	return m.runEpilogue(commitMessage(msg.action, msg.entry.Item))
}

// epilogueDoneMsg carries a finished epilogue back to the event loop.
type epilogueDoneMsg struct {
	res epilogue.Result
	err error
}

// runEpilogue performs the shared tail off the event loop.
//
// It has to be a command rather than a call: the epilogue takes a blocking
// flock on the store, held by whatever CLI process is mid-command, and waiting
// for it on the UI goroutine would freeze the pane.
func (m *Model) runEpilogue(message string) tea.Cmd {
	return m.runEpilogueWith(m.cfg, message)
}

// runEpilogueWith is runEpilogue over a configuration the caller chose, which
// is how the on-demand refresh forces a commit and a push.
func (m *Model) runEpilogueWith(cfg config.Config, message string) tea.Cmd {
	m.busy = true
	// The command runs on another goroutine, so it closes over copies rather
	// than reaching back into the model, which the event loop owns.
	st, now := m.store, m.now
	return func() tea.Msg {
		res, err := epilogue.Run(epilogue.Options{
			Store:   st,
			Config:  cfg,
			Steps:   epilogue.AllSteps,
			Message: message,
			Now:     now,
		})
		return epilogueDoneMsg{res: res, err: err}
	}
}

// afterEpilogue records what the run did and re-reads the list, which is where
// a bump, a sweep and the change itself all become visible at once.
func (m *Model) afterEpilogue(msg epilogueDoneMsg) {
	m.busy = false
	if msg.err != nil {
		m.err = msg.err
		return
	}
	m.err = nil
	m.status = describe(msg.res)
	// The events this run just produced are still in flight. They will still
	// reload the list; they will not be reported as somebody else's doing.
	m.selfWriteUntil = m.now().Add(selfWriteFor)
	if err := m.reload(); err != nil {
		m.err = err
	}
}

// gate dispatches an action unless an epilogue is already running. A pane in
// the middle of a commit refuses another one rather than queuing it, because a
// mutation decided against a list that is about to be re-read is a mutation
// against the wrong item.
func (m *Model) gate(action func() tea.Cmd) tea.Cmd {
	if m.busy {
		m.status = "still working"
		return nil
	}
	return action()
}

// Busy reports whether an epilogue is in flight.
func (m *Model) Busy() bool { return m.busy }

// moveCursor steps the selection, clamping at both ends rather than wrapping:
// a held j must stop at the last item, not cycle back to the first.
func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	m.clampCursor()
}

// clampCursor keeps the cursor on a row that exists, which is also what a
// reload needs after the listing has shrunk underneath it.
func (m *Model) clampCursor() {
	if m.cursor >= len(m.shown) {
		m.cursor = len(m.shown) - 1
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
	m.applyFilters()
	return nil
}

// applyFilters recomputes what is on screen and keeps the cursor on the item it
// was on, rather than on whatever has moved into its old position.
func (m *Model) applyFilters() {
	var was string
	if e := m.Selected(); e != nil {
		was = e.Item.ID
	}
	m.shown = m.filters.apply(m.entries)
	if was != "" {
		for i, e := range m.shown {
			if e.Item.ID == was {
				m.cursor = i
				m.clampCursor()
				return
			}
		}
	}
	m.clampCursor()
}

// currentScope is the scope a non-merged mode lists.
func (m *Model) currentScope() store.Scope {
	if m.mode == ModeGlobal {
		return store.Global
	}
	return m.scope.Scope
}

// Entries is what is on screen, in display order: the loaded listing with the
// filters applied.
func (m *Model) Entries() []store.Entry { return m.shown }

// Loaded is everything the current scope holds, before the filters narrow it.
func (m *Model) Loaded() []store.Entry { return m.entries }

// Cursor is the index of the selected row.
func (m *Model) Cursor() int { return m.cursor }

// Selected is the entry under the cursor, or nil when the list is empty.
func (m *Model) Selected() *store.Entry {
	if m.cursor < 0 || m.cursor >= len(m.shown) {
		return nil
	}
	return &m.shown[m.cursor]
}
