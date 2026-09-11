package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// pick moves to a list through the picker, the way a person does: g, then the
// cursor onto the row with that label, then enter.
func pick(t *testing.T, m *Model, label string) {
	t.Helper()
	press(m, "g")
	if !m.picker.open {
		t.Fatalf("g did not open the picker")
	}
	for i, row := range m.picker.rows {
		if row.label() == label {
			for m.picker.cursor < i {
				press(m, "j")
			}
			for m.picker.cursor > i {
				press(m, "k")
			}
			press(m, "enter")
			return
		}
	}
	t.Fatalf("the picker does not list %q, only %s", label, strings.Join(pickerLabels(m), ", "))
}

// pickerLabels names every row the open picker lists, in order.
func pickerLabels(m *Model) []string {
	out := make([]string, len(m.picker.rows))
	for i, row := range m.picker.rows {
		out[i] = row.label()
	}
	return out
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

// listText is every row the list would occupy, un-windowed and with no gap in
// it: the open rows and then the done ones, as the pane draws them when the
// height is unbounded.
func listText(m *Model) string {
	open, done := m.sections()
	rows := append(append([]string{}, open...), done...)
	if len(rows) == 0 {
		return ""
	}
	return strings.Join(rows, "\n") + "\n"
}

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

// TestTheViewNeverExceedsThePaneHeight: the other axis. A pane that renders
// more lines than it has does not scroll — it hands the terminal the choice of
// which end to keep, and the terminal keeps the tail. SortEntries puts the
// newest item first, so what a td user loses that way is the item they just
// added, which is the whole premise of the tool.
func TestTheViewNeverExceedsThePaneHeight(t *testing.T) {
	s := newStore(t)
	for i := range 30 {
		save(t, s, item{id: fmt.Sprintf("a%02d", i), title: fmt.Sprintf("item number %02d", i), updated: ago(i + 1)})
	}
	doneAt := ago(50)
	save(t, s, item{id: "zzz", title: "a finished one", updated: ago(60), doneAt: &doneAt})

	m := newModel(t, s)
	for _, height := range []int{40, 24, 12, 8, 6, 4} {
		m.Update(tea.WindowSizeMsg{Width: 60, Height: height})
		for _, cursor := range []int{0, 5, 15, 30} {
			m.cursor = cursor
			m.clampCursor()
			got := strings.Count(m.View(), "\n")
			if got > height {
				t.Errorf("at height %d with the cursor at %d the view is %d lines", height, cursor, got)
			}
		}
	}
}

// TestAVeryShortPaneStillShowsAnItem: the list's last line outranks the
// footer. Flooring the leftover at one line was not enough, because the chrome
// was subtracted first — at two rows the status line and the legend took both
// and the list rendered nothing at all, while the footer went on reporting a
// window whose visible part was off screen.
func TestAVeryShortPaneStillShowsAnItem(t *testing.T) {
	s := newStore(t)
	for i := range 30 {
		save(t, s, item{id: fmt.Sprintf("a%02d", i), title: fmt.Sprintf("item number %02d", i), updated: ago(i + 1)})
	}

	m := newModel(t, s)
	for _, height := range []int{5, 4, 3, 2, 1} {
		m.Update(tea.WindowSizeMsg{Width: 60, Height: height})
		view := m.View()
		if got := strings.Count(view, "\n") + 1; got > height {
			t.Errorf("at height %d the view is %d lines", height, got)
		}
		// item 00 is the newest — ago(i+1) makes i=0 the most recent — so it
		// is what the cursor opens on and the one line of list must show.
		if !strings.Contains(plain(view), "item number 00") {
			t.Errorf("a %d-row pane shows no item at all:\n%s", height, plain(view))
		}
	}
}

// TestTheCursorIsAlwaysOnScreen: the symptom that made the missing window
// invisible. The pane opened with the cursor on the newest item, above the top
// edge, so no ❯ appeared anywhere and j looked like it did nothing — it took
// 22 presses before the cursor swam into view.
func TestTheCursorIsAlwaysOnScreen(t *testing.T) {
	s := newStore(t)
	for i := range 30 {
		save(t, s, item{id: fmt.Sprintf("a%02d", i), title: fmt.Sprintf("item number %02d", i), updated: ago(i + 1)})
	}

	m := newModel(t, s)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})

	// "On screen" means inside the pane, so every check here asserts the view
	// fits the height as well as carrying the cursor. Without that, a view
	// holding every one of thirty rows contains a ❯ and still shows the user
	// nothing — which is exactly the bug this pins.
	onScreen := func(what string) {
		t.Helper()
		view := plain(m.View())
		if n := strings.Count(m.View(), "\n"); n > 12 {
			t.Fatalf("%s: the view is %d lines in a 12-row pane", what, n)
		}
		if !strings.Contains(view, "❯") {
			t.Fatalf("%s: no cursor on screen:\n%s", what, view)
		}
	}

	onScreen("on open")
	// And after every step of a walk from the top to the bottom and back.
	for _, key := range []string{"j", "k"} {
		for range len(m.Entries()) + 2 {
			press(m, key)
			onScreen(key)
			// And it is the selected item that is on screen, not just some ❯.
			selected := plain(m.row(m.Entries()[m.cursor], true))
			if !strings.Contains(plain(m.View()), strings.TrimSpace(selected)) {
				t.Fatalf("%s left the selected item off screen:\n%s", key, plain(m.View()))
			}
		}
	}
}

// TestTheFooterSaysWhatIsOffScreen: a list that just stops at the bottom of
// the pane looks like the whole list, which is how this went three milestones
// without being noticed.
func TestTheFooterSaysWhatIsOffScreen(t *testing.T) {
	s := newStore(t)
	for i := range 30 {
		save(t, s, item{id: fmt.Sprintf("a%02d", i), title: fmt.Sprintf("item number %02d", i), updated: ago(i + 1)})
	}

	m := newModel(t, s)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	if got := plain(m.View()); !strings.Contains(got, "below") {
		t.Errorf("the footer does not say the list continues past the pane:\n%s", got)
	}

	// A list that fits says nothing about a window, because there is not one.
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 60})
	if got := plain(m.View()); strings.Contains(got, "below") {
		t.Errorf("the footer reports a window for a list that fits:\n%s", got)
	}
}

// stack files a listing of open items and done ones, newest first within each
// section and titled so a rendered line names both which section it belongs to
// and where in that section it sits.
func stack(t *testing.T, s *store.Store, opens, dones int) {
	t.Helper()
	for i := range opens {
		save(t, s, item{id: fmt.Sprintf("a%02d", i), title: fmt.Sprintf("open %02d", i), updated: ago(i + 1)})
	}
	for i := range dones {
		finished := ago(i + 100)
		save(t, s, item{id: fmt.Sprintf("z%02d", i), title: fmt.Sprintf("done %02d", i), updated: finished, doneAt: &finished})
	}
}

// drawn is every line the view put on screen, blank ones included and styling
// stripped. The gap between the sections is made of blank lines, so the
// trimming helpers would hide the one thing these tests are about.
func drawn(view string) []string { return strings.Split(plain(view), "\n") }

// visibleRows is the index in m.shown of every item row among some rendered
// lines, in the order they were drawn. A row is the only line carrying a
// checkbox, which is what tells one from the rule, the gap and the chrome.
func visibleRows(t *testing.T, m *Model, rendered []string) []int {
	t.Helper()
	var out []int
	for _, line := range rendered {
		line = plain(line)
		if !strings.Contains(line, "[ ]") && !strings.Contains(line, "[x]") {
			continue
		}
		found := -1
		for i, e := range m.Entries() {
			if strings.Contains(line, e.Item.Title) {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("a row on screen names no item in the listing: %q", line)
		}
		out = append(out, found)
	}
	return out
}

// offScreenPattern reads the counts out of the footer's "3 above, 2 between,
// 4 below". Only the clauses with something to report are printed.
var offScreenPattern = regexp.MustCompile(`(\d+) (above|between|below)`)

// offScreen is what the footer says is not on screen.
func offScreen(view string) hidden {
	var h hidden
	for _, match := range offScreenPattern.FindAllStringSubmatch(plain(view), -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		switch match[2] {
		case "above":
			h.above = n
		case "between":
			h.mid = n
		case "below":
			h.below = n
		}
	}
	return h
}

// TestTheFooterSitsOnTheLastLineOfThePane: the pane is filled, not merely
// fitted. A list short enough to leave room used to end the view early, which
// left the footer floating in the middle of the pane with the rest of it blank
// — and a status line halfway up reads as the end of the screen rather than as
// the end of the list.
func TestTheFooterSitsOnTheLastLineOfThePane(t *testing.T) {
	s := newStore(t)
	stack(t, s, 2, 1)

	m := newModel(t, s)
	for _, height := range []int{24, 12, 6, 4, 3, 2} {
		m.Update(tea.WindowSizeMsg{Width: 60, Height: height})
		got := drawn(m.View())
		if len(got) != height {
			t.Errorf("at height %d the view is %d lines, want the pane filled:\n%s", height, len(got), strings.Join(got, "\n"))
			continue
		}
		// Two lines of footer while there is room for both, and the status
		// line alone once the legend has been shed.
		want := "q quit"
		if height == 2 {
			want = "global ·"
		}
		if last := got[len(got)-1]; !strings.Contains(last, want) {
			t.Errorf("at height %d the last line is %q, want the footer's %q", height, last, want)
		}
	}
}

// TestTheDoneSectionSitsAtTheBottomOfTheList: open at the top, done against
// the bottom of the list area, and the spare height as blank lines between
// them. The done section is the part you stop reading, so it belongs where you
// stop looking.
func TestTheDoneSectionSitsAtTheBottomOfTheList(t *testing.T) {
	s := newStore(t)
	stack(t, s, 2, 2)

	m := newModel(t, s)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	got := drawn(m.View())
	if len(got) != 14 {
		t.Fatalf("the view is %d lines in a 14-row pane:\n%s", len(got), strings.Join(got, "\n"))
	}

	// Counting back from the footer, which holds the last two lines: the done
	// rows, the rule above them, and the gap above that.
	last := len(got) - 1
	for i, want := range map[int]string{last - 1: "global ·", last - 2: "done 01", last - 3: "done 00", last - 4: doneRule} {
		if !strings.Contains(got[i], want) {
			t.Errorf("line %d is %q, want %q", i, got[i], want)
		}
	}
	if !strings.Contains(got[0], "open 00") || !strings.Contains(got[1], "open 01") {
		t.Errorf("the open rows are not at the top of the pane:\n%s", strings.Join(got, "\n"))
	}
	for i := 2; i <= last-5; i++ {
		if strings.TrimSpace(got[i]) != "" {
			t.Errorf("line %d is %q, want a blank line of the gap", i, got[i])
		}
	}
}

// TestThePromptSitsBetweenTheDoneSectionAndTheFooter: the prompt describes
// itself as opening over the footer, so it is pinned there rather than
// following the list up the pane.
func TestThePromptSitsBetweenTheDoneSectionAndTheFooter(t *testing.T) {
	s := newStore(t)
	stack(t, s, 2, 1)

	m := newModel(t, s)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	press(m, "a")

	got := drawn(m.View())
	if len(got) != 12 {
		t.Fatalf("the view is %d lines in a 12-row pane:\n%s", len(got), strings.Join(got, "\n"))
	}
	last := len(got) - 1
	for i, want := range map[int]string{last: "q quit", last - 1: "global ·", last - 2: "add>", last - 3: "done 00", last - 4: doneRule} {
		if !strings.Contains(got[i], want) {
			t.Errorf("line %d is %q, want %q", i, got[i], want)
		}
	}
}

// TestAWarningKeepsTheListBetweenItAndTheFooter: the head grows down from the
// top and the footer stays on the bottom, so the warning costs the list a line
// and moves nothing else.
func TestAWarningKeepsTheListBetweenItAndTheFooter(t *testing.T) {
	s := newStore(t)
	stack(t, s, 2, 1)
	broken := filepath.Join(s.Dir(store.Global, store.Active), "zzz-broken.md")
	if err := os.WriteFile(broken, []byte("---\nid: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newModel(t, s)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	got := drawn(m.View())
	if len(got) != 12 {
		t.Fatalf("the view is %d lines in a 12-row pane:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.HasPrefix(got[0], "warning: ") {
		t.Errorf("the first line is %q, want the warning", got[0])
	}
	if !strings.Contains(got[1], "open 00") {
		t.Errorf("the list does not start under the warning: %q", got[1])
	}
	last := len(got) - 1
	for i, want := range map[int]string{last: "q quit", last - 1: "global ·", last - 2: "done 00", last - 3: doneRule} {
		if !strings.Contains(got[i], want) {
			t.Errorf("line %d is %q, want %q", i, got[i], want)
		}
	}
}

// TestTheEmptyListKeepsItsMessageAtTheTop: nothing to window, and the two ends
// of the pane still belong to the message and the footer.
func TestTheEmptyListKeepsItsMessageAtTheTop(t *testing.T) {
	s := newStore(t)

	m := newModel(t, s)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	got := drawn(m.View())
	if len(got) != 10 {
		t.Fatalf("the view is %d lines in a 10-row pane:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[0], "Nothing here yet.") {
		t.Errorf("the first line is %q, want the empty message", got[0])
	}
	if !strings.Contains(got[len(got)-1], "q quit") {
		t.Errorf("the last line is %q, want the legend", got[len(got)-1])
	}
	for i := 1; i <= len(got)-3; i++ {
		if strings.TrimSpace(got[i]) != "" {
			t.Errorf("line %d is %q, want a blank line", i, got[i])
		}
	}
}

// TestOverflowGivesTheHeightToOpenFirst: a list too long for the pane spends
// its height on the open items and leaves done the rule alone. The rule is
// then the only thing on screen saying a done section exists, which is why it
// is reserved rather than taken by the twentieth open row.
func TestOverflowGivesTheHeightToOpenFirst(t *testing.T) {
	s := newStore(t)
	stack(t, s, 20, 5)

	m := newModel(t, s)
	// Ten lines of list: nine open rows and the rule.
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	got := drawn(m.View())
	if len(got) != 12 {
		t.Fatalf("the view is %d lines in a 12-row pane:\n%s", len(got), strings.Join(got, "\n"))
	}
	for i := range 9 {
		if want := fmt.Sprintf("open %02d", i); !strings.Contains(got[i], want) {
			t.Errorf("line %d is %q, want %q", i, got[i], want)
		}
	}
	if !strings.Contains(got[9], doneRule) {
		t.Errorf("line 9 is %q, want the done rule", got[9])
	}
	if strings.Contains(plain(m.View()), "done 0") {
		t.Errorf("a done row is drawn in a pane with no room for one:\n%s", strings.Join(got, "\n"))
	}
	// And nothing is padded: an overflowing list area is already full.
	for i, line := range got {
		if strings.TrimSpace(line) == "" {
			t.Errorf("line %d is blank, want no gap while rows are hidden:\n%s", i, strings.Join(got, "\n"))
		}
	}
}

// TestTheDoneSectionGrowsBackForTheCursor: j walked into a done section
// squeezed to its rule has to be able to see where it landed, so done takes a
// line back off open — and gives it up again on the way out.
func TestTheDoneSectionGrowsBackForTheCursor(t *testing.T) {
	s := newStore(t)
	stack(t, s, 20, 5)

	m := newModel(t, s)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	if got := plain(m.View()); strings.Contains(got, "done 0") {
		t.Fatalf("a done row is on screen before the cursor reaches one:\n%s", got)
	}

	// Straight onto the first done item, which is the row after the last open
	// one.
	m.cursor = 20
	got := drawn(m.View())
	if !strings.Contains(got[9], "done 00") {
		t.Errorf("the cursor's own row is not on screen:\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(got[8], doneRule) {
		t.Errorf("line 8 is %q, want the rule above the done row", got[8])
	}
	if !strings.Contains(got[7], "open 07") {
		t.Errorf("open did not give up exactly one row: line 7 is %q", got[7])
	}

	// Back out again, and open has its line back.
	m.cursor = 0
	got = drawn(m.View())
	if !strings.Contains(got[8], "open 08") || !strings.Contains(got[9], doneRule) {
		t.Errorf("open did not take back the line the cursor borrowed:\n%s", strings.Join(got, "\n"))
	}

	// And every step of the walk down and back keeps the selected row drawn.
	m.cursor = 0
	for _, key := range []string{"j", "k"} {
		for range len(m.Entries()) + 2 {
			press(m, key)
			view := m.View()
			if n := len(drawn(view)); n > 12 {
				t.Fatalf("%s: the view is %d lines in a 12-row pane", key, n)
			}
			selected := strings.TrimSpace(plain(m.row(m.Entries()[m.cursor], true)))
			if !strings.Contains(plain(view), selected) {
				t.Fatalf("%s left the selected item off screen:\n%s", key, plain(view))
			}
		}
	}
}

// TestTheFooterSaysWhichSideOfTheRuleRowsAreOn: two windows can each hide
// rows, and a row hidden between them is neither above the list nor below it.
// It is in the fold where the open section stops and the done one starts, and
// a reader told it was "below" would scroll past it looking for it.
func TestTheFooterSaysWhichSideOfTheRuleRowsAreOn(t *testing.T) {
	s := newStore(t)
	stack(t, s, 20, 5)

	m := newModel(t, s)
	// Ten lines of list against twenty-six of listing, so both windows have
	// something to hide.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})

	// The cursor at the top: nine open rows and the rule, so the rest of the
	// open section is in the fold and the whole done section is under it.
	if got := plain(m.View()); !strings.Contains(got, "· 11 between, 5 below") {
		t.Errorf("the footer does not count the fold:\n%s", got)
	}
	// Down to the last open row: the open window has scrolled, so it hides
	// rows on both sides of what it draws.
	m.cursor = 11
	if got := plain(m.View()); !strings.Contains(got, "· 3 above, 8 between, 5 below") {
		t.Errorf("the footer does not count all three places:\n%s", got)
	}
	// And into the done section, which gives the fold a row back.
	m.cursor = 20
	if got := plain(m.View()); !strings.Contains(got, "· 3 above, 9 between, 4 below") {
		t.Errorf("the footer miscounts once the cursor is in the done section:\n%s", got)
	}

	// A fold nothing is drawn on either side of is not a fold. With the whole
	// open section squeezed out, its rows are above what is on screen.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 3})
	if got := plain(m.View()); !strings.Contains(got, "· 20 above, 4 below") {
		t.Errorf("rows squeezed out of a section are not counted on its own side:\n%s", got)
	}

	// And a list with no done section has no fold to count into.
	t.Run("no done section", func(t *testing.T) {
		s := newStore(t)
		stack(t, s, 20, 0)
		m := newModel(t, s)
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 5})
		if got := plain(m.View()); !strings.Contains(got, "· 17 below") || strings.Contains(got, "between") {
			t.Errorf("a list with nothing done reports a fold:\n%s", got)
		}
	})
}

// TestTheSplitLadder: how a list area too small for both sections is divided,
// at every height where the division is forced and with the cursor in each
// section. The listing behind it has more of both than any of these areas can
// hold, so every cell is the split's own decision rather than a short section
// running out of rows.
//
// The cell that was wrong: at two lines with the cursor in done, the rule was
// reserved against the open row rather than against open's other rows, and the
// list read "── done ──" and the cursor's done row — a screen with no open
// item on it at all, in a pane with room for one.
func TestTheSplitLadder(t *testing.T) {
	const open, done = 20, 5
	for _, c := range []struct {
		area         int
		cursorInDone bool
		openHeight   int
		doneHeight   int
		ruled        bool
	}{
		{area: 1, openHeight: 1},
		{area: 1, cursorInDone: true, doneHeight: 1},
		{area: 2, openHeight: 1, ruled: true},
		{area: 2, cursorInDone: true, openHeight: 1, doneHeight: 1},
		{area: 3, openHeight: 2, ruled: true},
		{area: 3, cursorInDone: true, openHeight: 1, doneHeight: 1, ruled: true},
		{area: 4, openHeight: 3, ruled: true},
		{area: 4, cursorInDone: true, openHeight: 2, doneHeight: 1, ruled: true},
		{area: 5, openHeight: 4, ruled: true},
		{area: 5, cursorInDone: true, openHeight: 3, doneHeight: 1, ruled: true},
	} {
		where := "open"
		if c.cursorInDone {
			where = "done"
		}
		openHeight, doneHeight, ruled := split(c.area, open, done, c.cursorInDone)
		if openHeight != c.openHeight || doneHeight != c.doneHeight || ruled != c.ruled {
			t.Errorf("an area of %d with the cursor in %s splits into %d open, %d done, rule %t; want %d open, %d done, rule %t",
				c.area, where, openHeight, doneHeight, ruled, c.openHeight, c.doneHeight, c.ruled)
			continue
		}
		// Every cell fills the area exactly: a line neither section is given
		// is a line the pane draws nothing on.
		rule := 0
		if ruled {
			rule = 1
		}
		if got := openHeight + rule + doneHeight; got != c.area {
			t.Errorf("an area of %d with the cursor in %s is divided into %d lines", c.area, where, got)
		}
		// And the region holding the cursor has a row to draw it on.
		if c.cursorInDone && doneHeight < 1 {
			t.Errorf("an area of %d leaves the done section no room for the cursor's row", c.area)
		}
		if !c.cursorInDone && openHeight < 1 {
			t.Errorf("an area of %d leaves the open section no room for the cursor's row", c.area)
		}
	}
}

// TestAFourLinePaneShowsARowOfEachSection: the smallest pane that can show
// both sections, which is the ladder's broken cell seen from the outside. The
// rule took the line the open row needed, so a pane beside a full-height
// Claude Code session showed the done marker and one done item and nothing of
// what was still to do.
func TestAFourLinePaneShowsARowOfEachSection(t *testing.T) {
	s := newStore(t)
	stack(t, s, 20, 5)

	m := newModel(t, s)
	// Four rows: two of footer, two of list.
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 4})
	m.cursor = 20 // the first done item

	got := drawn(m.View())
	if len(got) != 4 {
		t.Fatalf("the view is %d lines in a 4-row pane:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[0], "open 00") {
		t.Errorf("the first line is %q, want the open row", got[0])
	}
	if !strings.Contains(got[1], "done 00") {
		t.Errorf("the second line is %q, want the row the cursor is on", got[1])
	}
	if strings.Contains(plain(m.View()), doneRule) {
		t.Errorf("the rule took a line one of the rows needed:\n%s", strings.Join(got, "\n"))
	}
}

// TestEachSectionScrollsOnItsOwn: two windows, two scroll offsets. Sharing one
// between them looks right in a screenshot and drifts in use — the done window
// scrolling to follow the cursor drags the open rows along under it, so rows
// nobody navigated away from leave the screen.
func TestEachSectionScrollsOnItsOwn(t *testing.T) {
	s := newStore(t)
	stack(t, s, 12, 6)

	m := newModel(t, s)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})

	// Down to the first done item, which is the row after the last open one.
	for range 12 {
		press(m, "j")
	}
	if m.cursor != 12 {
		t.Fatalf("the walk ended on row %d, want the first done item", m.cursor)
	}
	was := drawn(m.View())[0]
	if !strings.Contains(was, "open ") {
		t.Fatalf("the first line is %q, want an open row", was)
	}

	// On down the done section. The open window was not asked to move, so it
	// shows what it showed.
	for range len(m.Entries()) - 13 {
		press(m, "j")
		if got := drawn(m.View())[0]; got != was {
			t.Fatalf("with the cursor on row %d the open window starts at %q, want %q", m.cursor, got, was)
		}
	}
	// And the done window did follow, all the way to the last row.
	selected := strings.TrimSpace(plain(m.row(m.Entries()[m.cursor], true)))
	if !strings.Contains(plain(m.View()), selected) {
		t.Fatalf("the done window did not follow the cursor:\n%s", plain(m.View()))
	}
}

// TestEveryRowIsShownOrCounted: the conservation law across the two windows.
// Between them they draw each row at most once, in the listing's order, and
// whatever they do not draw the footer counts — so no row can be dropped by
// both windows or drawn by both.
func TestEveryRowIsShownOrCounted(t *testing.T) {
	s := newStore(t)
	stack(t, s, 12, 6)

	m := newModel(t, s)
	resize(m, 100)
	for capacity := range 25 {
		for cursor := range len(m.Entries()) {
			m.cursor = cursor
			lines, off := m.body(capacity)
			rows := visibleRows(t, m, lines)
			for i := 1; i < len(rows); i++ {
				if rows[i] <= rows[i-1] {
					t.Fatalf("at capacity %d cursor %d the rows are drawn %v, want each once in the listing's order", capacity, cursor, rows)
				}
			}
			if got := len(rows) + off.above + off.mid + off.below; got != len(m.Entries()) {
				t.Fatalf("at capacity %d cursor %d, %d rows drawn and %+v hidden, want %d rows accounted for", capacity, cursor, len(rows), off, len(m.Entries()))
			}
			var rules, gap int
			for _, line := range lines {
				switch {
				case strings.Contains(plain(line), doneRule):
					rules++
				case strings.TrimSpace(plain(line)) == "":
					gap++
				}
			}
			if len(lines) != len(rows)+rules+gap {
				t.Fatalf("at capacity %d cursor %d the list area holds %d lines, want %d rows plus %d rules plus %d blank", capacity, cursor, len(lines), len(rows), rules, gap)
			}
			if capacity > 0 && len(lines) != capacity {
				t.Fatalf("at capacity %d cursor %d the list area holds %d lines, want it filled", capacity, cursor, len(lines))
			}
			if gap > 0 && off.above+off.mid+off.below > 0 {
				t.Fatalf("at capacity %d cursor %d the list area is padded while %+v rows are hidden", capacity, cursor, off)
			}
		}
	}
}

// TestTheViewFitsEveryHeightDownToOne: the whole layout, at every height a
// pane can have and with the cursor on every row. The pane is never exceeded,
// the row the cursor is on is always drawn, and while the status line survives
// it accounts for every row that is not.
func TestTheViewFitsEveryHeightDownToOne(t *testing.T) {
	s := newStore(t)
	stack(t, s, 12, 6)

	m := newModel(t, s)
	for height := 1; height <= 26; height++ {
		for cursor := range len(m.Entries()) {
			m.cursor = cursor
			m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
			view := m.View()
			got := drawn(view)
			if len(got) > height {
				t.Fatalf("at height %d cursor %d the view is %d lines:\n%s", height, cursor, len(got), strings.Join(got, "\n"))
			}
			selected := strings.TrimSpace(plain(m.row(m.Entries()[cursor], true)))
			if !strings.Contains(plain(view), selected) {
				t.Fatalf("at height %d the cursor's row is off screen:\n%s", height, plain(view))
			}
			// A one-row pane has shed the footer, and a pane with nothing to
			// report has nothing to say about it.
			if height < 2 {
				continue
			}
			off := offScreen(view)
			if n := len(visibleRows(t, m, got)); n+off.above+off.mid+off.below != len(m.Entries()) {
				t.Fatalf("at height %d cursor %d, %d rows drawn and %+v hidden, want %d rows accounted for", height, cursor, n, off, len(m.Entries()))
			}
		}
	}
}

// TestTheHelpOverlayFitsThePane: the overlay is the one screen that must never
// be the thing you cannot read — it holds the keys, including the key that
// closes it. In a 12-row pane it used to render from "/ filter by title" down,
// losing its title and the first six keys off the top.
func TestTheHelpOverlayFitsThePane(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "one", updated: ago(1)})

	m := newModel(t, s)
	for _, height := range []int{40, 12, 8, 5} {
		m.Update(tea.WindowSizeMsg{Width: 60, Height: height})
		press(m, "?")
		if got := strings.Count(m.View(), "\n"); got > height {
			t.Errorf("at height %d the overlay is %d lines", height, got)
		}
		// The top of the overlay is its top, not whatever the terminal kept.
		if !strings.Contains(plain(m.View()), "td — keys") {
			t.Errorf("at height %d the overlay opens past its own title:\n%s", height, plain(m.View()))
		}
		press(m, "?")
	}

	// The line that says the overlay scrolls is pinned, not scrolled: letting
	// it scroll hides the affordance from the only reader who needs it.
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 11})
	press(m, "?")
	if got := plain(m.View()); !strings.Contains(got, "j/k scrolls") {
		t.Errorf("the overlay does not say it scrolls where it has to:\n%s", got)
	}
	press(m, "?")

	// And it scrolls, so every key is reachable in a pane too short for it.
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	press(m, "?")
	for range 20 {
		press(m, "j")
	}
	if got := plain(m.View()); !strings.Contains(got, "quit") {
		t.Errorf("scrolling the overlay never reaches the last key:\n%s", got)
	}
	if got := strings.Count(m.View(), "\n"); got > 8 {
		t.Errorf("the scrolled overlay is %d lines, want 8 or fewer", got)
	}
}

// TestNoRowEverExceedsTheWidth: the invariant, at every width worth having.
// A row wider than the pane is not a row that overruns — Bubble Tea clips it —
// so what an overrun actually costs is the right-hand end of the line, with
// nothing on screen to say anything was dropped. That is what a 40-column pane
// used to show: "due 2026-" and no age at all.
func TestNoRowEverExceedsTheWidth(t *testing.T) {
	s := newStore(t)
	long := strings.Repeat("wire the epilogue ", 7)
	save(t, s, item{id: "aaa", title: long, tags: []string{"cli", "epilogue"}, due: "2026-09-30", updated: ago(3)})
	doneAt := ago(1)
	save(t, s, item{id: "bbb", title: long, updated: ago(3), doneAt: &doneAt})
	save(t, s, item{id: "ccc", title: long, updated: ago(3), scope: store.Scope("a-rather-long-project-name")})

	m := newModel(t, s)
	for _, width := range []int{200, 80, 60, 50, 40, 30, 20, 10} {
		resize(m, width)
		for _, merged := range []bool{false, true} {
			if merged {
				m.view = scopeView{merged: true}
				if err := m.reload(); err != nil {
					t.Fatal(err)
				}
			}
			for i, e := range m.Entries() {
				// Measured on the styled row, which is what the terminal is
				// handed: a done title's strikethrough is per rune, so its
				// bytes run to several times its width.
				row := m.row(e, i == 0)
				if got := ansi.StringWidth(row); got > width {
					t.Errorf("at width %d (merged %v) a row is %d columns: %q", width, merged, got, ansi.Strip(row))
				}
			}
		}
	}
}

// TestNarrowPaneDropsCellsBeforeTheTitleDisappears: below the width everything
// fits in, whole cells go rather than the title being ground down to nothing —
// the age first, because "3h ago" is the vaguest thing on the row, and the due
// date last, because it is the one that is actually urgent.
func TestNarrowPaneDropsCellsBeforeTheTitleDisappears(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: strings.Repeat("wire the epilogue ", 7),
		tags: []string{"cli", "epilogue"}, due: "2026-09-30", updated: ago(3)})

	m := newModel(t, s)
	row := func(width int) string {
		resize(m, width)
		return plain(m.row(m.Entries()[0], false))
	}

	if got := row(60); !strings.Contains(got, "3h ago") || !strings.Contains(got, "#cli") || !strings.Contains(got, "due 2026-09-30") {
		t.Errorf("a 60-column pane dropped a cell it had room for: %q", got)
	}
	if got := row(50); strings.Contains(got, "3h ago") || !strings.Contains(got, "due 2026-09-30") {
		t.Errorf("at 50 the age should go first and the due date should stay: %q", got)
	}
	if got := row(40); strings.Contains(got, "#cli") || !strings.Contains(got, "due 2026-09-30") {
		t.Errorf("at 40 the tags should go and the due date should stay: %q", got)
	}
	// Whatever survives keeps its own place, so a narrowing pane loses cells
	// from a stable layout rather than rearranging what is left.
	if got := row(30); !strings.HasSuffix(strings.TrimSpace(got), "due 2026-09-30") {
		t.Errorf("at 30 the due date should still trail the title: %q", got)
	}
	if got := row(20); !strings.Contains(got, "wire") {
		t.Errorf("at 20 the row should still show what the item is: %q", got)
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
	if got := plain(listText(m)); strings.Contains(got, "global") || strings.Contains(got, "acme") {
		t.Errorf("a single-scope view labelled its rows, which says the same thing on every one:\n%s", got)
	}

	pick(t, m, "all scopes")
	if m.scopeLabel() != "all scopes" {
		t.Fatalf("the picker landed on %q, want the merged view", m.scopeLabel())
	}
	// The label belongs to its own row, not to whichever row sorted first.
	for _, line := range strings.Split(strings.TrimRight(plain(listText(m)), "\n"), "\n") {
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
	footer := m.footer(hidden{})
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
	if !strings.Contains(m.footer(hidden{}), "acme") {
		t.Errorf("the footer does not name the project: %q", m.footer(hidden{}))
	}
}
