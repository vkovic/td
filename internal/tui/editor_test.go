package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/store"
)

// syncExec runs a child program on the spot instead of handing it the terminal
// through Bubble Tea, which needs a real one. The editor still runs for real —
// only the terminal handover is skipped.
func syncExec(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	return func() tea.Msg { return fn(c.Run()) }
}

// script writes an executable shell script and returns a command line that runs
// it, for use as the configured editor.
func script(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// withEditor points the model's configured editor at a command line.
func withEditor(cmdline string) func(*Options) {
	return func(o *Options) {
		o.Config.Editor = cmdline
		o.Exec = syncExec
	}
}

// drain runs a command and feeds every message it produces back into the model,
// following the chain the event loop would: the editor finishes, the epilogue
// runs, the list reloads.
func drain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 10; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		_, cmd = m.Update(msg)
	}
}

// commitCount is how many commits the store's repository holds.
func commitCount(t *testing.T, s *store.Store) int {
	t.Helper()
	out, err := exec.Command("git", "-C", s.Root(), "rev-list", "--count", "HEAD").Output()
	if err != nil {
		// A repository with no commits yet has no HEAD to count.
		return 0
	}
	return atoi(t, strings.TrimSpace(string(out)))
}

// atoi parses a count, failing the test rather than the caller.
func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("not a count: %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// body reads an item file's markdown body back off disk.
func body(t *testing.T, s *store.Store, path string) string {
	t.Helper()
	it, err := s.Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	return it.Body
}

// updatedAt reads an item's updated timestamp back off disk.
func updatedAt(t *testing.T, s *store.Store, path string) time.Time {
	t.Helper()
	it, err := s.Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	return it.Updated
}

// TestSplitCommand: the configured editor may carry arguments, so it is split
// the way a shell splits a command line — without being a shell.
func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
		err  bool
	}{
		{name: "a bare command", in: "vi", want: []string{"vi"}},
		{name: "a command with a flag", in: "code --wait", want: []string{"code", "--wait"}},
		{name: "runs of spaces collapse", in: "  emacsclient   -nw  ", want: []string{"emacsclient", "-nw"}},
		{name: "double quotes hold a space", in: `"/Applications/My Editor" -w`, want: []string{"/Applications/My Editor", "-w"}},
		{name: "single quotes hold a space", in: `'/opt/my editor'`, want: []string{"/opt/my editor"}},
		{name: "a backslash escapes a space", in: `/opt/my\ editor`, want: []string{"/opt/my editor"}},
		{name: "an empty setting has no words", in: "   ", want: nil},
		{name: "an unclosed quote is an error", in: `"vi`, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitCommand(tt.in)
			if tt.err {
				if err == nil {
					t.Fatalf("splitCommand(%q) = %v, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitCommand(%q): %v", tt.in, err)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("splitCommand(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestEditorCommandAppendsThePath: the file to edit is the last argument, after
// whatever the editor setting already carried.
func TestEditorCommandAppendsThePath(t *testing.T) {
	c, err := editorCommand("code --wait", "/tmp/item.md")
	if err != nil {
		t.Fatalf("editorCommand: %v", err)
	}
	if got, want := strings.Join(c.Args, " "), "code --wait /tmp/item.md"; got != want {
		t.Errorf("editor command is %q, want %q", got, want)
	}
}

// TestEditorCommandNeedsAnEditor: an empty setting is an error rather than a
// silent no-op, because the keystroke did ask for something.
func TestEditorCommandNeedsAnEditor(t *testing.T) {
	if _, err := editorCommand("", "/tmp/item.md"); err == nil {
		t.Error("editorCommand with no editor = nil, want an error")
	}
}

// TestEditWritesBumpsAndCommits: e opens the item, and what the editor wrote is
// recorded, re-dated and committed by the epilogue that follows it.
func TestEditWritesBumpsAndCommits(t *testing.T) {
	s := newStore(t)
	ref := save(t, s, item{id: "aaa", title: "edit me", updated: ago(48)})

	m := newModel(t, s, withEditor(script(t, "append", `echo "a line the editor added" >> "$1"`)))
	before := commitCount(t, s)

	drain(t, m, press(m, "e"))

	if got := body(t, s, ref.Path); !strings.Contains(got, "a line the editor added") {
		t.Errorf("the body is %q, want the editor's line in it", got)
	}
	if got := updatedAt(t, s, ref.Path); !got.After(ago(48)) {
		t.Errorf("updated is %s, want it bumped past the original %s", got, ago(48))
	}
	if got, want := commitCount(t, s), before+1; got != want {
		t.Errorf("the edit left %d commits, want %d", got, want)
	}
}

// TestEditThatWritesNothingChangesNothing: quitting the editor without saving
// is a no-op, because the store tells its own writes from a hand edit by
// modification time and nothing moved.
func TestEditThatWritesNothingChangesNothing(t *testing.T) {
	s := newStore(t)
	ref := save(t, s, item{id: "aaa", title: "leave me", updated: ago(48)})

	m := newModel(t, s, withEditor("true"))
	// Commit the fixture first, so the count below measures the edit alone.
	drain(t, m, m.runEpilogue(""))

	before := commitCount(t, s)
	was := updatedAt(t, s, ref.Path)

	drain(t, m, press(m, "e"))

	if got := updatedAt(t, s, ref.Path); !got.Equal(was) {
		t.Errorf("updated moved to %s from %s after an editor that wrote nothing", got, was)
	}
	if got := commitCount(t, s); got != before {
		t.Errorf("an editor that wrote nothing left %d commits, want %d", got, before)
	}
}

// TestEditRunsAMultiWordEditor: an editor setting carrying arguments reaches
// the child process as arguments, not as part of the program name.
func TestEditRunsAMultiWordEditor(t *testing.T) {
	s := newStore(t)
	ref := save(t, s, item{id: "aaa", title: "edit me", updated: ago(48)})

	// The script records the arguments it was handed, so the split is visible
	// in the item rather than inferred.
	sh := script(t, "record", `echo "args: $1 $2" >> "$2"`)
	m := newModel(t, s, withEditor(sh+" --wait"))

	drain(t, m, press(m, "e"))

	if got := body(t, s, ref.Path); !strings.Contains(got, "args: --wait "+ref.Path) {
		t.Errorf("the editor saw %q, want --wait as its own argument before the path", got)
	}
}

// TestEditOnAnEmptyListDoesNothing: there is no item under the cursor, so the
// keystroke has nothing to open.
func TestEditOnAnEmptyListDoesNothing(t *testing.T) {
	m := newModel(t, newStore(t), withEditor("false"))
	if cmd := press(m, "e"); cmd != nil {
		t.Error("e on an empty list returned a command, want nothing to open")
	}
}

// TestAddCreatesOpensAndCommits: a takes a title, creates the item as td add
// would, opens it so the body is written in the same gesture, and commits.
func TestAddCreatesOpensAndCommits(t *testing.T) {
	s := newStore(t)
	m := newModel(t, s, withEditor(script(t, "append", `echo "the body" >> "$1"`)))
	before := commitCount(t, s)

	press(m, "a")
	if !m.prompt.open() {
		t.Fatal("a did not open a prompt")
	}
	for _, r := range "write the docs" {
		press(m, string(r))
	}
	drain(t, m, press(m, "enter"))

	if len(m.Entries()) != 1 {
		t.Fatalf("the list holds %d items after an add, want 1", len(m.Entries()))
	}
	it := m.Entries()[0].Item
	if it.Title != "write the docs" {
		t.Errorf("the title is %q, want %q", it.Title, "write the docs")
	}
	if it.Source != SourceTUI {
		t.Errorf("source is %q, want %q", it.Source, SourceTUI)
	}
	if it.ID == "" {
		t.Error("the new item has no id")
	}
	if !strings.Contains(it.Body, "the body") {
		t.Errorf("the body is %q, want what the editor wrote", it.Body)
	}
	if got, want := commitCount(t, s), before+1; got != want {
		t.Errorf("the add left %d commits, want %d", got, want)
	}
}

// TestAddWithNoTitleAddsNothing: an empty prompt is an abandoned one.
func TestAddWithNoTitleAddsNothing(t *testing.T) {
	s := newStore(t)
	m := newModel(t, s, withEditor("true"))
	press(m, "a")
	drain(t, m, press(m, "enter"))
	if len(m.Entries()) != 0 {
		t.Errorf("an empty title added %d items", len(m.Entries()))
	}
}

// TestPromptEscapeAbandons: esc closes the prompt and adds nothing.
func TestPromptEscapeAbandons(t *testing.T) {
	s := newStore(t)
	m := newModel(t, s, withEditor("true"))
	press(m, "a")
	for _, r := range "never mind" {
		press(m, string(r))
	}
	press(m, "esc")
	if m.prompt.open() {
		t.Error("esc left the prompt open")
	}
	if len(m.Entries()) != 0 {
		t.Errorf("an abandoned prompt added %d items", len(m.Entries()))
	}
}

// TestPromptTakesTheKeysThatWouldOtherwiseAct: while a prompt is open, j and q
// are text, not navigation and not quit.
func TestPromptTakesTheKeysThatWouldOtherwiseAct(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "one", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "two", updated: ago(2)})

	m := newModel(t, s, withEditor("true"))
	press(m, "a")
	for _, r := range "jq" {
		if cmd := press(m, string(r)); cmd != nil {
			t.Fatalf("%q in a prompt returned a command, want it typed", r)
		}
	}
	if m.prompt.value != "jq" {
		t.Errorf("the prompt holds %q, want %q", m.prompt.value, "jq")
	}
	if m.Cursor() != 0 {
		t.Errorf("j in a prompt moved the cursor to %d", m.Cursor())
	}
}

// TestPromptBackspaceDeletesARune: a multi-byte character goes in one press.
func TestPromptBackspaceDeletesARune(t *testing.T) {
	p := prompt{kind: promptAdd, value: "café"}
	p.backspace()
	if p.value != "caf" {
		t.Errorf("backspace left %q, want %q", p.value, "caf")
	}
}

// TestAddFilesIntoTheProjectScope: an add from a project's list goes into that
// project, which is where td add in the same directory would have put it.
func TestAddFilesIntoTheProjectScope(t *testing.T) {
	s := newStore(t)
	m := newModel(t, s, withEditor("true"), func(o *Options) {
		o.Scope = store.ScopeChoice{Scope: store.Scope("acme")}
	})
	press(m, "a")
	for _, r := range "ship it" {
		press(m, string(r))
	}
	drain(t, m, press(m, "enter"))

	if len(m.Entries()) != 1 {
		t.Fatalf("the project list holds %d items, want 1", len(m.Entries()))
	}
	if got := m.Entries()[0].Ref.Scope; got != store.Scope("acme") {
		t.Errorf("the item landed in scope %q, want acme", got)
	}
}

// TestEpilogueRespectsAutoCommit: a mutating key runs the same tail the CLI
// runs, so a store configured not to commit still does not.
func TestEpilogueRespectsAutoCommit(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "edit me", updated: ago(48)})

	m := newModel(t, s, func(o *Options) {
		o.Config = config.Default()
		o.Config.AutoCommit = false
	}, withEditor(script(t, "append", `echo "x" >> "$1"`)))
	before := commitCount(t, s)

	drain(t, m, press(m, "e"))

	if got := commitCount(t, s); got != before {
		t.Errorf("auto_commit off still left %d commits, want %d", got, before)
	}
}
