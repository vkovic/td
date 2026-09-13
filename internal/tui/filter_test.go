package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vkovic/td/internal/store"
)

// typeInto opens a prompt with key and types text into it, without submitting.
func typeInto(m *Model, key, text string) {
	press(m, key)
	for _, r := range text {
		press(m, string(r))
	}
}

// tagged is a fixture store with tags spread across open and done items, and
// one tag spelled in two cases.
func tagged(t *testing.T) *store.Store {
	t.Helper()
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "write the docs", tags: []string{"docs"}, updated: ago(1)})
	save(t, s, item{id: "bbb", title: "fix the backend", tags: []string{"Backend", "urgent"}, updated: ago(2)})
	save(t, s, item{id: "ccc", title: "rewrite the docs index", tags: []string{"docs", "backend"}, updated: ago(3)})
	save(t, s, item{id: "ddd", title: "ship the release", updated: ago(4)})
	save(t, s, item{id: "eee", title: "old backend chore", tags: []string{"backend"},
		updated: ago(5), doneAt: done(ago(5))})
	return s
}

// TestTitleFilterNarrows: / filters on the title, and esc puts the list back.
func TestTitleFilterNarrows(t *testing.T) {
	m := newModel(t, tagged(t))
	before := len(m.Entries())

	typeInto(m, "/", "docs")
	press(m, "enter")

	if got := strings.Join(titles(m), ","); got != "write the docs,rewrite the docs index" {
		t.Errorf("the filtered listing is %s, want the two docs items", got)
	}
	if len(m.Loaded()) != before {
		t.Errorf("the filter changed what is loaded: %d, want %d", len(m.Loaded()), before)
	}

	press(m, "esc")
	if got := len(m.Entries()); got != before {
		t.Errorf("esc left %d items, want the original %d", got, before)
	}
}

// TestTitleFilterIgnoresCase: typing what you can see, in whatever case you
// type it, is the point of the key.
func TestTitleFilterIgnoresCase(t *testing.T) {
	m := newModel(t, tagged(t))
	typeInto(m, "/", "BACKEND")
	press(m, "enter")
	if got := strings.Join(titles(m), ","); got != "fix the backend,old backend chore" {
		t.Errorf("the filtered listing is %s, want both backend titles", got)
	}
}

// TestTitleFilterThatMatchesNothingSaysSo: an empty screen with no visible
// cause is the failure mode worth avoiding.
func TestTitleFilterThatMatchesNothingSaysSo(t *testing.T) {
	m := newModel(t, tagged(t))
	typeInto(m, "/", "nothing matches this")
	press(m, "enter")

	if len(m.Entries()) != 0 {
		t.Fatalf("the filter kept %d items", len(m.Entries()))
	}
	view := plain(m.View())
	if !strings.Contains(view, "Nothing matches") {
		t.Errorf("the view does not say why it is empty:\n%s", view)
	}
	if !strings.Contains(view, "/nothing matches this") {
		t.Errorf("the view does not name the filter:\n%s", view)
	}
}

// TestTagCycleVisitsEveryTagAndComesBack: t steps through the tags the loaded
// list carries, in a stable order, with no filter as the step after the last.
func TestTagCycleVisitsEveryTagAndComesBack(t *testing.T) {
	m := newModel(t, tagged(t))

	// backend, docs and urgent, sorted, with Backend and backend as one tag.
	want := []string{"Backend", "docs", "urgent", ""}
	for i, tag := range want {
		press(m, "t")
		if m.filters.tag != tag {
			t.Fatalf("step %d of the cycle is %q, want %q", i, m.filters.tag, tag)
		}
	}
	press(m, "t")
	if m.filters.tag != "Backend" {
		t.Errorf("the cycle did not come round again: %q", m.filters.tag)
	}
}

// TestTagFilterMatchesTheCLIsComparison: the TUI narrows through
// store.HasEveryTag, so a tag matches whatever case it is written in, exactly
// as td ls -t backend matches an item tagged Backend.
func TestTagFilterMatchesTheCLIsComparison(t *testing.T) {
	s := tagged(t)
	m := newModel(t, s)

	press(m, "t") // Backend
	got := titles(m)

	// The same comparison, applied by hand to the same listing.
	var want []string
	for _, e := range m.Loaded() {
		if store.HasEveryTag(e.Item, []string{"backend"}) {
			want = append(want, e.Item.Title)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the tag filter kept %v, want %v", got, want)
	}
	if len(want) != 3 {
		t.Fatalf("the fixture carries %d backend items, want 3", len(want))
	}
}

// TestTagFilterKeepsDoneItems: this is the one place the TUI filter differs
// from td ls -t on purpose. The CLI hides done items unless --done is passed;
// the TUI has no such flag and always shows both sections, so a tag only a
// completed item carries shows that item under the rule rather than emptying
// the screen for a reason nothing on screen explains.
func TestTagFilterKeepsDoneItems(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "still open", tags: []string{"other"}, updated: ago(1)})
	save(t, s, item{id: "bbb", title: "finished chore", tags: []string{"chore"},
		updated: ago(2), doneAt: done(ago(2))})

	m := newModel(t, s)
	for m.filters.tag != "chore" {
		press(m, "t")
	}
	if got := strings.Join(titles(m), ","); got != "finished chore" {
		t.Errorf("the tag filter kept %s, want the done item", got)
	}
}

// TestTagCycleOnAListWithNoTags: there is nothing to cycle through.
func TestTagCycleOnAListWithNoTags(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "untagged", updated: ago(1)})
	m := newModel(t, s)
	press(m, "t")
	if m.filters.tag != "" {
		t.Errorf("t set the filter to %q on a list with no tags", m.filters.tag)
	}
	if len(m.Entries()) != 1 {
		t.Errorf("t hid the untagged item")
	}
}

// TestFiltersCombine: a title filter and a tag filter narrow together rather
// than replacing each other.
func TestFiltersCombine(t *testing.T) {
	m := newModel(t, tagged(t))
	typeInto(m, "/", "docs")
	press(m, "enter")
	for m.filters.tag != "Backend" {
		press(m, "t")
	}
	if got := strings.Join(titles(m), ","); got != "rewrite the docs index" {
		t.Errorf("the combined filters kept %s, want the one item carrying both", got)
	}
	if got := m.filters.describe(); !strings.Contains(got, "/docs") || !strings.Contains(got, "#Backend") {
		t.Errorf("the footer describes the filters as %q, want both named", got)
	}
}

// TestFilterSurvivesAReload: a change landing while a filter is open leaves the
// filter and the cursor where they were. The filter is view state; a reload
// re-reads the store, which never knew about it.
func TestFilterSurvivesAReload(t *testing.T) {
	s := tagged(t)
	m := newModel(t, s)

	typeInto(m, "/", "docs")
	press(m, "enter")
	press(m, "j") // onto the second docs item
	was := m.Selected().Item.ID
	if was == "" {
		t.Fatal("nothing selected")
	}

	// Something arrives from outside, as the watcher would report it.
	save(t, s, item{id: "fff", title: "a new unrelated item", updated: ago(0)})
	m.Update(storeChangedMsg{})

	if m.filters.title != "docs" {
		t.Errorf("the reload cleared the filter: %q", m.filters.title)
	}
	if got := strings.Join(titles(m), ","); got != "write the docs,rewrite the docs index" {
		t.Errorf("the reload changed the filtered listing to %s", got)
	}
	if got := m.Selected().Item.ID; got != was {
		t.Errorf("the reload moved the cursor to %s, want it still on %s", got, was)
	}
}

// TestCursorFollowsItsItemThroughAFilter: narrowing keeps the cursor on the
// item it was on when that item survives the filter.
func TestCursorFollowsItsItemThroughAFilter(t *testing.T) {
	m := newModel(t, tagged(t))
	for m.Selected().Item.Title != "rewrite the docs index" {
		press(m, "j")
	}
	was := m.Selected().Item.ID

	typeInto(m, "/", "docs")
	press(m, "enter")

	if got := m.Selected().Item.ID; got != was {
		t.Errorf("the filter moved the cursor to %s, want it still on %s", got, was)
	}
}

// TestFilterPromptStartsFromTheActiveFilter: reopening / offers what is already
// filtering, so narrowing it further does not mean retyping it, and a
// backspace widens the list at once.
func TestFilterPromptStartsFromTheActiveFilter(t *testing.T) {
	m := newModel(t, tagged(t))
	typeInto(m, "/", "docsi")
	press(m, "enter")
	if got := strings.Join(titles(m), ","); got != "rewrite the docs index" {
		t.Fatalf("the kept filter shows %s", got)
	}
	press(m, "/")
	if m.prompt.value != "docsi" {
		t.Errorf("the prompt offers %q, want the active filter", m.prompt.value)
	}
	press(m, "backspace")
	if got := strings.Join(titles(m), ","); got != "write the docs,rewrite the docs index" {
		t.Errorf("backspace left %s on screen, want both docs items without pressing enter", got)
	}
}

// fuzzy is a fixture store whose titles tell a subsequence match from a
// substring one, with a done item among them.
func fuzzy(t *testing.T) *store.Store {
	t.Helper()
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "fix docs build", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "feed the dog", updated: ago(2)})
	save(t, s, item{id: "ccc", title: "Find Dead Branches", updated: ago(3), doneAt: done(ago(3))})
	save(t, s, item{id: "ddd", title: "ship the release", updated: ago(4)})
	return s
}

// TestMatchTitle: the runes of the query in order, any case, anywhere in the
// title, landing on the first rune that fits each.
func TestMatchTitle(t *testing.T) {
	for _, tc := range []struct {
		title, query string
		want         []int
		ok           bool
	}{
		{"fix docs build", "fdb", []int{0, 4, 9}, true},
		{"fix docs build", "FDB", []int{0, 4, 9}, true},
		{"fix docs build", "bdf", nil, false},
		{"fix docs build", "fix d", []int{0, 1, 2, 3, 4}, true},
		{"fix docs build", "", nil, true},
		{"ça va", "av", []int{1, 3}, true},
		{"short", "shorter", nil, false},
	} {
		got, ok := matchTitle(tc.title, tc.query)
		if ok != tc.ok || fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("matchTitle(%q, %q) = %v, %v; want %v, %v", tc.title, tc.query, got, ok, tc.want, tc.ok)
		}
	}
}

// TestTitleFilterNarrowsAsYouType: every keystroke redraws the list, before
// enter, and a match is a subsequence of the title in either section.
func TestTitleFilterNarrowsAsYouType(t *testing.T) {
	m := newModel(t, fuzzy(t))

	press(m, "/")
	if len(m.Entries()) != 4 {
		t.Fatalf("opening the input hid rows: %v", titles(m))
	}
	for _, step := range []struct{ key, want string }{
		{"f", "fix docs build,feed the dog,Find Dead Branches"},
		{"d", "fix docs build,feed the dog,Find Dead Branches"},
		{"b", "fix docs build,Find Dead Branches"},
		{"backspace", "fix docs build,feed the dog,Find Dead Branches"},
	} {
		press(m, step.key)
		if got := strings.Join(titles(m), ","); got != step.want {
			t.Errorf("after %q the list is %s, want %s", step.key, got, step.want)
		}
	}
	if !m.prompt.open() {
		t.Error("the input closed without enter")
	}
}

// TestEveryKeyTypesIntoTheFilter: j, k and q are letters in a title, so while
// the input is open they go into the query and neither move the cursor nor
// quit.
func TestEveryKeyTypesIntoTheFilter(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "jkq one", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "two", updated: ago(2)})
	m := newModel(t, s)

	typeInto(m, "/", "jkq")
	press(m, "down")
	if m.prompt.value != "jkq" {
		t.Errorf("the query is %q, want jkq", m.prompt.value)
	}
	if m.quitting || m.Cursor() != 0 {
		t.Errorf("a key in the input acted on the list: quitting %v, cursor %d", m.quitting, m.Cursor())
	}
	if got := strings.Join(titles(m), ","); got != "jkq one" {
		t.Errorf("the list is %s, want the one title holding jkq", got)
	}
}

// TestTheFilterHeadsThePane: the input covers the list's name while open, the
// kept query stays there after enter, the footer echoes it throughout, and the
// name comes back once the title filter goes.
func TestTheFilterHeadsThePane(t *testing.T) {
	m := newModel(t, tagged(t))
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})

	typeInto(m, "/", "docs")
	got := drawn(m.View())
	if got[0] != "/docs█" {
		t.Errorf("with the input open the top line is %q, want /docs█", got[0])
	}
	if status := got[len(got)-2]; !strings.Contains(status, "· filtered /docs") {
		t.Errorf("the status line does not echo the filter while typing: %q", status)
	}

	press(m, "enter")
	got = drawn(m.View())
	if got[0] != "/docs" {
		t.Errorf("after enter the top line is %q, want the kept query", got[0])
	}
	if status := got[len(got)-2]; !strings.Contains(status, "· filtered /docs") {
		t.Errorf("the status line does not echo the kept filter: %q", status)
	}

	press(m, "esc")
	if got := drawn(m.View())[0]; got != "global" {
		t.Errorf("after clearing the filter the top line is %q, want the list's name", got)
	}

	// Enter on an emptied input is a cleared filter, not a kept empty one.
	typeInto(m, "/", "docs")
	press(m, "enter")
	press(m, "/")
	for range 4 {
		press(m, "backspace")
	}
	press(m, "enter")
	if m.filters.title != "" || drawn(m.View())[0] != "global" {
		t.Errorf("enter on an empty query left %q and the top line %q", m.filters.title, drawn(m.View())[0])
	}
}

// TestEscInTheFilterDropsOnlyTheTitle: esc abandons what the input built and
// nothing else, so a tag filter set before it is still narrowing afterwards.
func TestEscInTheFilterDropsOnlyTheTitle(t *testing.T) {
	m := newModel(t, tagged(t))
	for m.filters.tag != "Backend" {
		press(m, "t")
	}
	typeInto(m, "/", "docs")
	if got := strings.Join(titles(m), ","); got != "rewrite the docs index" {
		t.Fatalf("the filters did not combine while typing: %s", got)
	}

	press(m, "esc")
	if m.prompt.open() {
		t.Error("esc left the input open")
	}
	if m.filters.title != "" || m.filters.tag != "Backend" {
		t.Errorf("esc left the filters as %+v, want only the tag", m.filters)
	}
	if got := strings.Join(titles(m), ","); got != "fix the backend,rewrite the docs index,old backend chore" {
		t.Errorf("after esc the list is %s, want every backend item", got)
	}
}

// TestMatchedRunesAreUnderlined: the runes the query landed on are underlined,
// in an open row and in a done one, on top of the style the row already has.
func TestMatchedRunesAreUnderlined(t *testing.T) {
	m := newModel(t, fuzzy(t))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	typeInto(m, "/", "fdb")
	rows := listText(m)

	for _, want := range []struct{ what, text string }{
		{"the open row's f", m.styles.title.Underline(true).Render("f")},
		{"the open row's b", m.styles.title.Underline(true).Render("b")},
		{"the open row's unmatched run", m.styles.title.Render("ix ")},
		{"the done row's F", m.styles.done.Underline(true).Render("F")},
		{"the done row's B", m.styles.done.Underline(true).Render("B")},
	} {
		if !strings.Contains(rows, want.text) {
			t.Errorf("%s is not rendered as %q in:\n%q", want.what, want.text, rows)
		}
	}

	press(m, "esc")
	if strings.Contains(listText(m), m.styles.title.Underline(true).Render("f")) {
		t.Error("an underline outlived the filter")
	}
}

// TestTheFilterLineGivesWayLast: in a pane too short for all the chrome, the
// top line holding the filter outlasts the rule, the legend and the status
// line, whether the input is open or its query was kept.
func TestTheFilterLineGivesWayLast(t *testing.T) {
	s := newStore(t)
	for i := range 30 {
		save(t, s, item{id: fmt.Sprintf("a%02d", i), title: fmt.Sprintf("item number %02d", i), updated: ago(i + 1)})
	}
	rule := strings.Repeat("─", 60)

	for _, kept := range []bool{false, true} {
		m := newModel(t, s)
		typeInto(m, "/", "item")
		if kept {
			press(m, "enter")
		}
		for height, want := range map[int][]string{
			1: {"item number 00"},
			2: {"/item", "item number 00"},
			3: {"/item", "item number 00", "open, "},
			4: {"/item", "item number 00", "open, ", "q quit"},
			5: {"/item", rule, "item number 00", "open, ", "q quit"},
		} {
			m.Update(tea.WindowSizeMsg{Width: 60, Height: height})
			got := drawn(m.View())
			if len(got) != len(want) {
				t.Errorf("kept %v, height %d: the view is %d lines, want %d:\n%s", kept, height, len(got), len(want), strings.Join(got, "\n"))
				continue
			}
			for i := range want {
				if !strings.Contains(got[i], want[i]) {
					t.Errorf("kept %v, height %d: line %d is %q, want %q", kept, height, i, got[i], want[i])
				}
			}
		}
	}
}
