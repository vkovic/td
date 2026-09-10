package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/store"
)

// clock is the instant every test renders against, so "3 days ago" and
// "overdue" mean a fixed thing rather than whatever today happens to be.
var clock = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

// at freezes the model's clock.
func at(t time.Time) func() time.Time { return func() time.Time { return t } }

// ago is an instant a number of hours before the fixed clock.
func ago(hours int) time.Time { return clock.Add(-time.Duration(hours) * time.Hour) }

// isolate keeps git from reading the developer's own configuration, following
// the same conventions as cmd/td's harness.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-system"))
	t.Setenv("GIT_AUTHOR_NAME", "td test")
	t.Setenv("GIT_AUTHOR_EMAIL", "td@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "td test")
	t.Setenv("GIT_COMMITTER_EMAIL", "td@example.invalid")
}

// newStore opens a store in a temp TD_ROOT with git isolated.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	isolate(t)
	root := filepath.Join(t.TempDir(), ".td")
	t.Setenv(store.EnvRoot, root)
	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

// item is one fixture item, written the way td writes one.
type item struct {
	id      string
	title   string
	tags    []string
	due     string
	updated time.Time
	created time.Time
	doneAt  *time.Time
	scope   store.Scope
}

// save files a fixture item and returns where it landed.
func save(t *testing.T, s *store.Store, f item) store.Ref {
	t.Helper()
	it := &store.Item{
		ID:      f.id,
		Title:   f.title,
		Tags:    f.tags,
		Created: f.created,
		Updated: f.updated,
		DoneAt:  f.doneAt,
	}
	if it.Created.IsZero() {
		it.Created = f.updated
	}
	if f.due != "" {
		d, err := store.ParseDate(f.due)
		if err != nil {
			t.Fatalf("ParseDate(%q): %v", f.due, err)
		}
		it.Due = &d
	}
	ref, err := s.Save(f.scope, store.Active, it)
	if err != nil {
		t.Fatalf("Save(%s): %v", f.id, err)
	}
	return ref
}

// done is a pointer to a completion instant, for the doneAt field.
func done(t time.Time) *time.Time { return &t }

// testRenderer forces a color profile, so a style is visible in the rendered
// string rather than being stripped for a non-terminal writer.
func testRenderer() *lipgloss.Renderer {
	r := lipgloss.NewRenderer(io.Discard)
	// Lip Gloss strips color for a writer that is not a terminal, which would
	// make every style render as plain text and every style assertion pass
	// vacuously. Forcing the profile is what makes styling observable here.
	r.SetColorProfile(termenv.ANSI)
	return r
}

// newModel builds a model over s, scoped to the global list unless told
// otherwise, with the clock frozen.
func newModel(t *testing.T, s *store.Store, opts ...func(*Options)) *Model {
	t.Helper()
	o := Options{
		Store:    s,
		Config:   config.Default(),
		Scope:    store.ScopeChoice{Scope: store.Global},
		Renderer: testRenderer(),
		Now:      at(clock),
	}
	for _, fn := range opts {
		fn(&o)
	}
	m, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// press sends a keystroke to the model, as Bubble Tea would.
func press(m *Model, key string) tea.Cmd {
	var msg tea.KeyMsg
	if len(key) == 1 {
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	} else {
		msg = tea.KeyMsg{Type: keyTypes[key]}
	}
	_, cmd := m.Update(msg)
	return cmd
}

// keyTypes maps the named keys a test presses to their Bubble Tea types.
var keyTypes = map[string]tea.KeyType{
	"up": tea.KeyUp, "down": tea.KeyDown, "enter": tea.KeyEnter, "esc": tea.KeyEsc,
}

// titles names the loaded listing in display order, so a failure reads as a
// sequence rather than a struct dump.
func titles(m *Model) []string {
	out := make([]string, len(m.Entries()))
	for i, e := range m.Entries() {
		out[i] = e.Item.Title
	}
	return out
}

// lines splits a rendered view into its lines with the styling stripped, for
// the assertions about structure. A style can span every rune of a word — the
// strikethrough on a done title does — so matching text against a styled line
// has to go through here.
func lines(view string) []string {
	return strings.Split(strings.TrimRight(ansi.Strip(view), "\n"), "\n")
}

// plain is a whole view with its styling stripped.
func plain(view string) string { return ansi.Strip(view) }

// resize hands the model a terminal size, the way Bubble Tea does on start and
// on every resize after it.
func resize(m *Model, width int) {
	m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
}

// TestRowsFitTheWidth: a title longer than the pane is elided to fit, and the
// cells that answer a question — the tags, the due date, the age — survive
// intact, because a row that drops them to keep 30 more characters of title
// has kept the wrong thing.
func TestRowsFitTheWidth(t *testing.T) {
	s := newStore(t)
	long := strings.Repeat("wire the epilogue ", 7) // 126 columns
	save(t, s, item{id: "aaa", title: long, tags: []string{"cli"}, due: "2026-09-30", updated: ago(3)})

	m := newModel(t, s)
	resize(m, 60)

	line := plain(m.row(m.Entries()[0], true))
	if got := ansi.StringWidth(line); got != 60 {
		t.Errorf("the row is %d columns wide, want 60:\n%s", got, line)
	}
	for _, want := range []string{"#cli", "due 2026-09-30", "3h ago"} {
		if !strings.Contains(line, want) {
			t.Errorf("the fit dropped %q from the row:\n%s", want, line)
		}
	}
	if !strings.Contains(line, "…") {
		t.Errorf("the title was not elided:\n%s", line)
	}

	// A pane with room to spare leaves the title alone.
	resize(m, 200)
	if line := plain(m.row(m.Entries()[0], true)); !strings.Contains(line, long) {
		t.Errorf("a wide pane elided a title that fits:\n%s", line)
	}
}

// TestDoneRowsFitTheWidth: the fit has to run on the styled title. Lip Gloss
// wraps a done title's strikethrough around every rune, so the rendered string
// is several times its display width — a fit measured in bytes, or applied
// before styling, looks right everywhere except the rows that carry styling.
func TestDoneRowsFitTheWidth(t *testing.T) {
	s := newStore(t)
	long := strings.Repeat("wire the epilogue ", 7)
	doneAt := ago(1)
	save(t, s, item{id: "aaa", title: long, updated: ago(3), doneAt: &doneAt})

	m := newModel(t, s)
	resize(m, 60)

	// Measured on the styled output, before plain() strips it: that is what
	// the terminal is given.
	styled := m.row(m.Entries()[0], false)
	if got := ansi.StringWidth(styled); got != 60 {
		t.Errorf("the done row is %d columns wide, want 60:\n%q", got, styled)
	}
	if len(styled) <= 60 {
		t.Fatalf("the done row carries no styling, so this test proves nothing: %q", styled)
	}
	if !strings.Contains(plain(styled), "…") {
		t.Errorf("the done title was not elided:\n%s", plain(styled))
	}
}

// TestNarrowPaneKeepsAStubOfTitle: below the width the fixed cells already
// need, the row overruns — but by as little as a title stub costs, not by the
// whole title. A row cut to a column of ellipses tells one item from another
// not at all.
func TestNarrowPaneKeepsAStubOfTitle(t *testing.T) {
	s := newStore(t)
	long := strings.Repeat("wire the epilogue ", 7)
	save(t, s, item{id: "aaa", title: long, tags: []string{"cli", "epilogue"}, due: "2026-09-30", updated: ago(3)})

	m := newModel(t, s)
	resize(m, 30)

	line := plain(m.row(m.Entries()[0], false))
	if got := ansi.StringWidth(line); got > 60 {
		t.Errorf("a 30-column pane rendered a %d-column row, want the title cut back to a stub:\n%s", got, line)
	}
	if !strings.HasPrefix(strings.TrimSpace(line), "[ ] wire") {
		t.Errorf("the row lost its title entirely:\n%s", line)
	}
}

// TestRowsAreUnfittedBeforeTheFirstSize: Bubble Tea sends the size after the
// model is built, and a row rendered before it arrives has no width to fit to.
func TestRowsAreUnfittedBeforeTheFirstSize(t *testing.T) {
	s := newStore(t)
	long := strings.Repeat("wire the epilogue ", 7)
	save(t, s, item{id: "aaa", title: long, updated: ago(3)})

	m := newModel(t, s)
	if line := plain(m.row(m.Entries()[0], false)); !strings.Contains(line, long) {
		t.Errorf("a row was fitted before any width was known:\n%s", line)
	}
}

// TestMergedViewLabelsEachScope: the merged view mixes lists, so a row has to
// say which list it came from — the same thing td ls --all does with its SCOPE
// column. A single-scope view says it once, in the status line, and does not
// repeat it on every row.
func TestMergedViewLabelsEachScope(t *testing.T) {
	s := newStore(t)
	// Titles that carry neither scope name, so an assertion about a label is
	// about the label and not about the title next to it.
	save(t, s, item{id: "aaa", title: "first item", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "second item", updated: ago(2), scope: store.Scope("acme")})

	m := newModel(t, s)
	if m.scopeLabel() != "global" {
		t.Fatalf("the pane opened on %q, want the global list", m.scopeLabel())
	}
	if got := plain(m.rows()); strings.Contains(got, "global") || strings.Contains(got, "acme") {
		t.Errorf("a single-scope view labelled its rows, which says the same thing on every one:\n%s", got)
	}

	press(m, "g") // onto the merged view
	if m.scopeLabel() != "all scopes" {
		t.Fatalf("g landed on %q, want the merged view", m.scopeLabel())
	}
	// The label belongs to its own row, not to whichever row sorted first.
	for _, line := range strings.Split(strings.TrimRight(plain(m.rows()), "\n"), "\n") {
		switch {
		case strings.Contains(line, "first item") && !strings.Contains(line, "global"):
			t.Errorf("the global row carries no scope: %s", line)
		case strings.Contains(line, "second item") && !strings.Contains(line, "acme"):
			t.Errorf("the acme row carries no scope: %s", line)
		}
	}
}

// TestListOrder: the TUI shows what td ls would show, in the same order, open
// items first and done items after them.
func TestListOrder(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "stale open", updated: ago(48)})
	save(t, s, item{id: "bbb", title: "fresh open", updated: ago(1)})
	save(t, s, item{id: "ccc", title: "old done", updated: ago(72), doneAt: done(ago(70))})
	save(t, s, item{id: "ddd", title: "new done", updated: ago(96), doneAt: done(ago(2))})

	m := newModel(t, s)
	got := strings.Join(titles(m), ",")
	if want := "fresh open,stale open,new done,old done"; got != want {
		t.Errorf("listing order is %s, want %s", got, want)
	}
}

// TestDoneRuleSitsBetweenTheSections: the rule marks the one place the listing
// crosses from open to done, and is absent when there is no crossing.
func TestDoneRuleSitsBetweenTheSections(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "open one", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "open two", updated: ago(2)})
	save(t, s, item{id: "ccc", title: "done one", updated: ago(3), doneAt: done(ago(3))})

	m := newModel(t, s)
	got := lines(m.View())

	ruleAt := -1
	for i, line := range got {
		if strings.Contains(line, doneRule) {
			ruleAt = i
		}
	}
	if ruleAt < 0 {
		t.Fatalf("no done rule in:\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(got[ruleAt-1], "open two") {
		t.Errorf("the line above the rule is %q, want the last open item", got[ruleAt-1])
	}
	if !strings.Contains(got[ruleAt+1], "done one") {
		t.Errorf("the line below the rule is %q, want the first done item", got[ruleAt+1])
	}
}

// TestDoneRuleAbsentWithoutBothSections: a list of only open items, and a list
// of only done items, each render as one uninterrupted section.
func TestDoneRuleAbsentWithoutBothSections(t *testing.T) {
	t.Run("only open items", func(t *testing.T) {
		s := newStore(t)
		save(t, s, item{id: "aaa", title: "open one", updated: ago(1)})
		save(t, s, item{id: "bbb", title: "open two", updated: ago(2)})
		if view := newModel(t, s).View(); strings.Contains(view, doneRule) {
			t.Errorf("a list with no done items drew the rule:\n%s", view)
		}
	})
	t.Run("only done items", func(t *testing.T) {
		s := newStore(t)
		save(t, s, item{id: "ccc", title: "done one", updated: ago(3), doneAt: done(ago(3))})
		if view := newModel(t, s).View(); strings.Contains(view, doneRule) {
			t.Errorf("a list with no open items drew the rule:\n%s", view)
		}
	})
}

// TestOverdueStyling: a due date that has passed is styled as a warning, and
// one that has not is styled as an ordinary due date.
func TestOverdueStyling(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "late", due: "2026-09-09", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "soon", due: "2026-09-11", updated: ago(2)})
	save(t, s, item{id: "ccc", title: "today", due: "2026-09-10", updated: ago(3)})

	m := newModel(t, s)
	view := m.View()

	overdue := m.styles.overdue.Render("due 2026-09-09")
	if !strings.Contains(view, overdue) {
		t.Errorf("an item due yesterday is not styled overdue:\n%s", view)
	}
	for _, date := range []string{"due 2026-09-11", "due 2026-09-10"} {
		if strings.Contains(view, m.styles.overdue.Render(date)) {
			t.Errorf("%s is styled overdue but has not passed:\n%s", date, view)
		}
		if !strings.Contains(view, m.styles.due.Render(date)) {
			t.Errorf("%s is not styled as a due date:\n%s", date, view)
		}
	}
}

// TestOverdueIgnoresDoneItems: a done item that was finished late is not nagged
// about, because there is nothing left to do about it.
func TestOverdueIgnoresDoneItems(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "late but finished", due: "2026-09-01",
		updated: ago(1), doneAt: done(ago(1))})

	m := newModel(t, s)
	if view := m.View(); strings.Contains(view, m.styles.overdue.Render("due 2026-09-01")) {
		t.Errorf("a done item is styled overdue:\n%s", view)
	}
}

// TestCursorClampsAtBothEnds: j stops on the last row and k on the first,
// rather than wrapping around.
func TestCursorClampsAtBothEnds(t *testing.T) {
	s := newStore(t)
	for _, f := range []item{
		{id: "aaa", title: "one", updated: ago(1)},
		{id: "bbb", title: "two", updated: ago(2)},
		{id: "ccc", title: "three", updated: ago(3)},
	} {
		save(t, s, f)
	}

	m := newModel(t, s)
	if m.Cursor() != 0 {
		t.Fatalf("the cursor starts at %d, want 0", m.Cursor())
	}
	for i := 0; i < 5; i++ {
		press(m, "j")
	}
	if got, want := m.Cursor(), 2; got != want {
		t.Errorf("j past the end left the cursor at %d, want %d", got, want)
	}
	for i := 0; i < 5; i++ {
		press(m, "k")
	}
	if got, want := m.Cursor(), 0; got != want {
		t.Errorf("k past the start left the cursor at %d, want %d", got, want)
	}
}

// TestCursorOnAnEmptyList: the cursor has nowhere to go and nothing is selected.
func TestCursorOnAnEmptyList(t *testing.T) {
	m := newModel(t, newStore(t))
	press(m, "j")
	if m.Cursor() != 0 {
		t.Errorf("the cursor moved to %d on an empty list", m.Cursor())
	}
	if m.Selected() != nil {
		t.Errorf("an empty list has a selection: %v", m.Selected())
	}
}

// TestQuitAsksToQuit: q returns Bubble Tea's quit command.
func TestQuitAsksToQuit(t *testing.T) {
	m := newModel(t, newStore(t))
	if cmd := press(m, "q"); cmd == nil {
		t.Fatal("q returned no command, want tea.Quit")
	}
}

// TestUnparsableFileWarnsAndKeepsTheRest: one broken file must not cost the
// listing, because the TUI is where you would go to open it and fix it.
func TestUnparsableFileWarnsAndKeepsTheRest(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "readable", updated: ago(1)})

	broken := filepath.Join(s.Dir(store.Global, store.Active), "zzz-broken.md")
	if err := os.WriteFile(broken, []byte("---\nid: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newModel(t, s)
	view := plain(m.View())
	if !strings.Contains(view, "warning:") {
		t.Errorf("no warning line for an unparsable file:\n%s", view)
	}
	if !strings.Contains(view, "zzz-broken.md") {
		t.Errorf("the warning does not name the file:\n%s", view)
	}
	if !strings.Contains(view, "readable") {
		t.Errorf("the readable item is missing from the listing:\n%s", view)
	}
}

// TestEveryBrokenFileGetsItsOwnWarningLine: a listing that met several broken
// files reports one errors.Join, whose Error() is newline separated. Prefixing
// that string once left every line after the first unlabelled, reading as if
// the pane had printed a bare filename at somebody.
func TestEveryBrokenFileGetsItsOwnWarningLine(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "readable", updated: ago(1)})
	for _, name := range []string{"zzz-broken-one.md", "zzz-broken-two.md"} {
		path := filepath.Join(s.Dir(store.Global, store.Active), name)
		if err := os.WriteFile(path, []byte("---\nid: [unclosed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := newModel(t, s)

	// Wide enough to carry the whole of both messages: each names its own file
	// and each is labelled.
	resize(m, 200)
	named := map[string]bool{}
	for _, line := range strings.Split(plain(m.View()), "\n") {
		if !strings.Contains(line, "broken") {
			continue
		}
		if !strings.HasPrefix(line, "warning: ") {
			t.Errorf("a broken file is reported on an unlabelled line: %q", line)
		}
		for _, name := range []string{"zzz-broken-one.md", "zzz-broken-two.md"} {
			if strings.Contains(line, name) {
				named[name] = true
			}
		}
	}
	if len(named) != 2 {
		t.Errorf("%d of the two broken files were named on a warning line", len(named))
	}

	// And at the width td was written for, every one of those lines is still
	// labelled and still fits.
	resize(m, 60)
	warned := 0
	for _, line := range strings.Split(plain(m.View()), "\n") {
		if !strings.HasPrefix(line, "warning: ") {
			continue
		}
		warned++
		if got := ansi.StringWidth(line); got > 60 {
			t.Errorf("a warning line is %d columns wide, want 60 or fewer: %q", got, line)
		}
	}
	if warned != 2 {
		t.Errorf("%d warning lines, want one per broken file", warned)
	}
	if !strings.Contains(plain(m.View()), "readable") {
		t.Error("the readable item is missing from the listing")
	}
}

// TestRelative renders an age rather than a wall clock, because a list that
// refreshes in place is read for what changed.
func TestRelative(t *testing.T) {
	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{name: "seconds", when: clock.Add(-30 * time.Second), want: "just now"},
		{name: "minutes", when: clock.Add(-5 * time.Minute), want: "5m ago"},
		{name: "hours", when: ago(3), want: "3h ago"},
		{name: "days", when: ago(72), want: "3d ago"},
		{name: "past a week it is a date", when: ago(24 * 30), want: "2026-08-11"},
		{name: "no timestamp renders as nothing", when: time.Time{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := relative(tt.when, clock); got != tt.want {
				t.Errorf("relative = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFooterNamesTheScopeAndCounts: the footer is the only place the scope is
// stated, so a pane left open beside a project says which list it is showing.
func TestFooterNamesTheScopeAndCounts(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "open", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "shut", updated: ago(2), doneAt: done(ago(2))})

	m := newModel(t, s)
	footer := m.footer()
	if !strings.Contains(footer, "global") {
		t.Errorf("the footer does not name the scope: %q", footer)
	}
	if !strings.Contains(footer, "1 open, 2 total") {
		t.Errorf("the footer counts wrong: %q", footer)
	}
}

// TestProjectScopeListsOnlyThatProject: a model resolved to a project shows
// that project's items and not the global list's.
func TestProjectScopeListsOnlyThatProject(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "global item", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "project item", updated: ago(2), scope: store.Scope("acme")})

	m := newModel(t, s, func(o *Options) {
		o.Scope = store.ScopeChoice{Scope: store.Scope("acme")}
	})
	if got, want := strings.Join(titles(m), ","), "project item"; got != want {
		t.Errorf("the project listing is %s, want %s", got, want)
	}
	if !strings.Contains(m.footer(), "acme") {
		t.Errorf("the footer does not name the project: %q", m.footer())
	}
}
