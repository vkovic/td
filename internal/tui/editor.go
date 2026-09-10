package tui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vkovic/td/internal/store"
)

// editorFinishedMsg arrives when the editor has exited. Its error is the
// editor's own; the item file is whatever the editor left behind.
type editorFinishedMsg struct {
	action string
	entry  store.Entry
	err    error
}

// editItem hands the terminal to the editor over one item's file.
//
// Nothing is written here and nothing is compared afterwards. The store already
// tells td's own writes from a hand edit by comparing a file's modification
// time against the updated timestamp inside it, so quitting the editor without
// saving is a no-op, and a save that changed nothing bumps the item exactly as
// the same save from a shell would. Making the TUI compare contents would be
// the one editing surface that behaves differently from every other.
func (m *Model) editItem(action string, e store.Entry) tea.Cmd {
	c, err := editorCommand(m.cfg.Editor, e.Ref.Path)
	if err != nil {
		return func() tea.Msg { return editorFinishedMsg{action: action, entry: e, err: err} }
	}
	return m.exec(c, func(err error) tea.Msg {
		return editorFinishedMsg{action: action, entry: e, err: err}
	})
}

// editorCommand builds the command that opens path.
//
// The configured editor may carry arguments — "code --wait" and "emacsclient
// -nw" both do — so it is split into words rather than handed to exec.Command
// as a single program name, which would look for a file called "code --wait".
func editorCommand(editor, path string) (*exec.Cmd, error) {
	words, err := splitCommand(editor)
	if err != nil {
		return nil, fmt.Errorf("reading the editor setting %q: %w", editor, err)
	}
	if len(words) == 0 {
		return nil, errors.New("no editor is configured")
	}
	return exec.Command(words[0], append(words[1:], path)...), nil
}

// splitCommand splits a command line into words the way a shell would, honoring
// single quotes, double quotes and backslash escapes. It is deliberately not a
// shell: nothing is expanded, so an editor setting cannot run a substitution.
func splitCommand(s string) ([]string, error) {
	var (
		words []string
		word  strings.Builder
		have  bool
		quote rune
	)
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case quote == '\'':
			// Inside single quotes a backslash is a backslash, as in a shell.
			if c == '\'' {
				quote = 0
			} else {
				word.WriteRune(c)
			}
		case quote == '"':
			if c == '\\' && i+1 < len(runes) {
				i++
				word.WriteRune(runes[i])
			} else if c == '"' {
				quote = 0
			} else {
				word.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote = c
			have = true
		case c == '\\' && i+1 < len(runes):
			i++
			word.WriteRune(runes[i])
			have = true
		case c == ' ' || c == '\t':
			if have || word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
				have = false
			}
		default:
			word.WriteRune(c)
			have = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed %c quote", quote)
	}
	if have || word.Len() > 0 {
		words = append(words, word.String())
	}
	return words, nil
}

// addItem creates an item from a title and opens it in the editor, so the body
// is written in the same gesture that created it.
//
// This is td add's own sequence: a fresh id, the same instant for created and
// updated, and the source recorded — as tui here, because the item was raised
// at this keyboard rather than by Claude or by a shell.
func (m *Model) addItem(title string) tea.Cmd {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	created := m.now()
	it := &store.Item{
		ID:      store.NewID(),
		Title:   title,
		Created: created,
		Updated: created,
		Source:  SourceTUI,
	}
	ref, err := m.store.Save(m.currentAddScope(), store.Active, it)
	if err != nil {
		return func() tea.Msg { return editorFinishedMsg{action: "add", err: err} }
	}
	return m.editItem("add", store.Entry{Item: it, Ref: ref})
}

// SourceTUI is what an item raised in the terminal interface records as its
// source. It says who created the item, never who last touched it: only a
// creation writes the field.
const SourceTUI = "tui"

// currentAddScope is the list a new item is filed in. The merged view spans
// every scope and has no single list of its own, so an add from there goes to
// the scope this directory resolved to, which is where td add would have put it.
func (m *Model) currentAddScope() store.Scope {
	if m.mode == ModeAll {
		return m.scope.Scope
	}
	return m.currentScope()
}

// commitMessage describes a change for git, mirroring the CLI's own so the
// store's history reads the same whichever surface made the change.
func commitMessage(action string, it *store.Item) string {
	if it == nil {
		return ""
	}
	return fmt.Sprintf("td: %s %s %s", action, it.ID, it.Title)
}
