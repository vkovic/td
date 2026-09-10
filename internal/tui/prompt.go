package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vkovic/td/internal/epilogue"
)

// promptKind is which question the inline prompt is asking.
type promptKind int

const (
	// promptNone means no prompt is open.
	promptNone promptKind = iota
	// promptAdd is asking for a new item's title.
	promptAdd
)

// prompt is the one-line editor that opens over the footer. It is deliberately
// small: the only text typed into the TUI is a title or a filter, and every
// other edit goes to $EDITOR.
type prompt struct {
	kind  promptKind
	label string
	value string
}

// open reports whether a prompt is taking keystrokes.
func (p prompt) open() bool { return p.kind != promptNone }

// typed appends a keystroke's runes.
func (p *prompt) typed(msg tea.KeyMsg) {
	if msg.Type == tea.KeySpace {
		p.value += " "
		return
	}
	p.value += string(msg.Runes)
}

// backspace removes the last rune, counting runes rather than bytes so a
// multi-byte character is deleted in one press.
func (p *prompt) backspace() {
	if p.value == "" {
		return
	}
	runes := []rune(p.value)
	p.value = string(runes[:len(runes)-1])
}

// describe says what an epilogue run did, in the few words the footer has room
// for. It reports a push only when the store has somewhere to push to: a run
// with no remote reports Pushed as true, since doing nothing succeeded.
func describe(res epilogue.Result) string {
	var parts []string
	if n := len(res.Bumped); n > 0 {
		parts = append(parts, plural(n, "hand edit", "hand edits")+" recorded")
	}
	if n := len(res.Archived); n > 0 {
		parts = append(parts, plural(n, "item", "items")+" archived")
	}
	if res.Committed {
		parts = append(parts, "committed")
	}
	if len(parts) == 0 {
		return "nothing to do"
	}
	return strings.Join(parts, ", ")
}

// plural renders a count with the right noun, as the CLI does.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
