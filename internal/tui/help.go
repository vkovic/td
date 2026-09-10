package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
		{keys: []string{"j", "down"}, help: "move down", short: "move",
			run: func(m *Model) tea.Cmd { m.moveCursor(1); return nil }},
		{keys: []string{"k", "up"}, help: "move up",
			run: func(m *Model) tea.Cmd { m.moveCursor(-1); return nil }},

		{keys: []string{"a"}, help: "add an item, then open it in $EDITOR", short: "add",
			run: (*Model).startAdd},
		{keys: []string{"e", "enter"}, help: "open the selected item in $EDITOR", short: "edit",
			run: func(m *Model) tea.Cmd { return m.gate(m.editSelected) }},
		{keys: []string{"x"}, help: "mark the selected item done, or reopen it", short: "done",
			run: func(m *Model) tea.Cmd { return m.gate(m.toggleDone) }},
		{keys: []string{"d"}, help: "move the selected item to the trash", short: "delete",
			run: func(m *Model) tea.Cmd { return m.gate(m.removeItem) }},

		{keys: []string{"/"}, help: "filter by title", short: "filter",
			run: (*Model).startFilter},
		{keys: []string{"t"}, help: "cycle the tag filter", short: "tag",
			run: func(m *Model) tea.Cmd { m.cycleTag(); return nil }},
		{keys: []string{"g"}, help: "cycle the scope: this project, global, all", short: "scope",
			run: (*Model).nextScope},

		{keys: []string{"esc"}, help: "clear the filters",
			run: func(m *Model) tea.Cmd { m.clearFilters(); return nil }},

		{keys: []string{"r"}, help: "record hand edits, sweep, commit and push now", short: "refresh",
			run: func(m *Model) tea.Cmd { return m.gate(m.refresh) }},
		{keys: []string{"?"}, help: "show this help", short: "help",
			run: func(m *Model) tea.Cmd { m.showHelp = !m.showHelp; return nil }},
		{keys: []string{"q", "ctrl+c"}, help: "quit", short: "quit",
			run: func(m *Model) tea.Cmd { m.quitting = true; return tea.Quit }},
	}
}

// legend is the one-line key summary the footer carries, drawn from the same
// table as the overlay so the two cannot disagree about what a key does.
func legend() string {
	var parts []string
	for _, b := range keyMap() {
		if b.short != "" {
			parts = append(parts, b.keys[0]+" "+b.short)
		}
	}
	return strings.Join(parts, " · ")
}

// helpView is the overlay: every key, one per line, generated from the table.
func (m *Model) helpView() string {
	var b strings.Builder
	fmt.Fprintln(&b, m.styles.title.Render("td — keys"))
	fmt.Fprintln(&b)

	width := 0
	table := keyMap()
	for _, k := range table {
		if n := len(k.name()); n > width {
			width = n
		}
	}
	for _, k := range table {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, k.name(), k.help)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, m.styles.footer.Render("Every edit goes to $EDITOR: "+m.cfg.Editor))
	fmt.Fprintln(&b, m.styles.footer.Render("? or esc closes this"))
	return b.String()
}
