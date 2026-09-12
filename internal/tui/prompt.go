package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// promptKind is which question the inline prompt is asking.
type promptKind int

const (
	// promptNone means no prompt is open.
	promptNone promptKind = iota
	// promptAdd is asking for a new item's title.
	promptAdd
	// promptFilter is asking what to narrow the list to.
	promptFilter
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
