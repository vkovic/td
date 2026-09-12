package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// binding is one key and what it does. The same table dispatches the keystroke
// and writes the help, so a key cannot exist without being documented and the
// help cannot describe a key that does nothing.
type binding struct {
	// keys are every keystroke that triggers this, the first being the one the
	// help shows.
	keys []string
	// help says what the key does, in the words the overlay uses.
	help string
	// short is the same thing in one or two words, for the footer legend. An
	// empty short keeps the key out of the legend, which is how the overlay
	// carries more keys than the one line has room for. It is written out
	// rather than cut from help, because cutting produces "move to the".
	short string
	// rank orders the legend's entries by what a narrow pane can least afford
	// to lose, lowest first. It is not the order they are drawn in: the legend
	// selects by rank and renders in table order, so the line gets shorter as
	// the pane narrows without its entries reshuffling.
	rank int
	// run performs the action.
	run func(*Model) tea.Cmd
}

// name is how the help refers to this binding: its keys, slash separated.
func (b binding) name() string { return strings.Join(b.keys, "/") }

// keyMap is every key the TUI answers to.
//
// Order is the order the overlay lists them in: moving about, then the keys
// that change an item, then the keys that change the view, then the rest.
func keyMap() []binding {
	return []binding{
		{keys: []string{"j", "down"}, help: "move down", short: "move", rank: 6,
			run: func(m *Model) tea.Cmd { m.moveCursor(1); return nil }},
		{keys: []string{"k", "up"}, help: "move up",
			run: func(m *Model) tea.Cmd { m.moveCursor(-1); return nil }},

		{keys: []string{"a"}, help: "add an item, then open it in $EDITOR", short: "add", rank: 3,
			run: (*Model).startAdd},
		{keys: []string{"e", "enter"}, help: "open the selected item in $EDITOR", short: "edit", rank: 4,
			run: func(m *Model) tea.Cmd { return m.gate(m.editSelected) }},
		{keys: []string{"x"}, help: "mark the selected item done, or reopen it", short: "done", rank: 5,
			run: func(m *Model) tea.Cmd { return m.gate(m.toggleDone) }},
		{keys: []string{"d"}, help: "move the selected item to the trash", short: "delete", rank: 8,
			run: func(m *Model) tea.Cmd { return m.gate(m.removeItem) }},

		{keys: []string{"/"}, help: "filter by title", short: "filter", rank: 7,
			run: (*Model).startFilter},
		{keys: []string{"t"}, help: "cycle the tag filter", short: "tag", rank: 10,
			run: func(m *Model) tea.Cmd { m.cycleTag(); return nil }},
		{keys: []string{"g"}, help: "pick the list to show: global, all, or a project", short: "scope", rank: 9,
			run: (*Model).openPicker},
		{keys: []string{"i"}, help: "show or hide each item's id", short: "ids", rank: 12,
			run: func(m *Model) tea.Cmd { m.showIDs = !m.showIDs; return nil }},

		{keys: []string{"esc"}, help: "clear the filters",
			run: func(m *Model) tea.Cmd { m.clearFilters(); return nil }},

		{keys: []string{"r"}, help: "record hand edits, sweep, commit and push now", short: "refresh", rank: 11,
			run: func(m *Model) tea.Cmd { return m.gate(m.refresh) }},
		{keys: []string{"?"}, help: "show this help", short: "help", rank: 1,
			run: func(m *Model) tea.Cmd { m.showHelp = !m.showHelp; m.helpTop = 0; return nil }},
		{keys: []string{"q", "ctrl+c"}, help: "quit", short: "quit", rank: 2,
			run: func(m *Model) tea.Cmd { m.quitting = true; return tea.Quit }},
	}
}

// legendSeparator sits between two legend entries.
const legendSeparator = " · "

// legend is the one-line key summary the footer carries, drawn from the same
// table as the overlay so the two cannot disagree about what a key does.
//
// A width of zero means an unknown terminal, and the full legend is returned.
// Otherwise entries are dropped until the line fits, highest rank first: ?
// survives every width, because it is the only thing on screen that says what
// the dropped keys were. Whatever survives is drawn in table order, so the
// legend shortens as the pane narrows instead of reshuffling.
//
// Entries are dropped whole. A legend truncated mid-entry leaves the reader
// unable to tell "/ fil" from "/ filter", which is worse than the key being
// absent and the overlay one press away.
func legend(width int) string {
	var keep []binding
	for _, b := range keyMap() {
		if b.short != "" {
			keep = append(keep, b)
		}
	}
	for width > 0 && legendWidth(keep) > width && len(keep) > 1 {
		worst := 0
		for i, b := range keep {
			if b.rank > keep[worst].rank {
				worst = i
			}
		}
		keep = append(keep[:worst], keep[worst+1:]...)
	}

	parts := make([]string, 0, len(keep))
	for _, b := range keep {
		parts = append(parts, b.keys[0]+" "+b.short)
	}
	return strings.Join(parts, legendSeparator)
}

// legendWidth is how many columns a set of entries takes once rendered.
//
// Measured, not counted. The entries are ASCII, but legendSeparator is " · ":
// four bytes for three columns, so a byte count overstates the line by one per
// gap. That is conservative — the legend under-uses the pane rather than
// overrunning it — which is why it withheld a key that fit and no test that
// checks for overrun ever noticed.
func legendWidth(bindings []binding) int {
	if len(bindings) == 0 {
		return 0
	}
	total := ansi.StringWidth(legendSeparator) * (len(bindings) - 1)
	for _, b := range bindings {
		total += ansi.StringWidth(b.keys[0]) + 1 + ansi.StringWidth(b.short)
	}
	return total
}

// helpView is the overlay: every key, one per line, generated from the table.
func (m *Model) helpView() string {
	lines := m.helpLines()
	if m.height <= 0 || len(lines) <= m.height {
		m.helpTop = 0
		return strings.Join(lines, "\n")
	}
	// The overlay is the one screen that must never be the thing you cannot
	// read: it is where the keys live, including the key that closes it. In a
	// pane too short for it, it scrolls rather than losing its top.
	//
	// The line saying so is pinned to the bottom rather than scrolled with the
	// rest. Letting it scroll puts the affordance below the fold for exactly
	// the reader who needs it — the one in a pane short enough to truncate the
	// overlay — which is the silent-ending bug this whole family started with.
	pinned := m.styles.footer.Render(m.fit("j/k scrolls · ? or esc closes this"))
	body, room := lines[:len(lines)-1], m.height-1
	m.helpTop = min(max(m.helpTop, 0), len(body)-room)
	return strings.Join(append(append([]string{}, body[m.helpTop:m.helpTop+room]...), pinned), "\n")
}

// helpLines is the overlay's content, one line per line on screen, each fitted
// to the pane's width.
func (m *Model) helpLines() []string {
	width := 0
	table := keyMap()
	for _, k := range table {
		if n := len(k.name()); n > width {
			width = n
		}
	}

	lines := []string{m.styles.title.Render(m.fit("td — keys")), ""}
	for _, k := range table {
		lines = append(lines, m.fit(fmt.Sprintf("  %-*s  %s", width, k.name(), k.help)))
	}
	return append(lines,
		"",
		m.styles.footer.Render(m.fit("Every edit goes to $EDITOR: "+m.cfg.Editor)),
		m.styles.footer.Render(m.fit("? or esc closes this")),
	)
}
