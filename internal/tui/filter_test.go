package tui

import (
	"strings"
	"testing"

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
// filtering, so narrowing it further does not mean retyping it.
func TestFilterPromptStartsFromTheActiveFilter(t *testing.T) {
	m := newModel(t, tagged(t))
	typeInto(m, "/", "docs")
	press(m, "enter")
	press(m, "/")
	if m.prompt.value != "docs" {
		t.Errorf("the prompt offers %q, want the active filter", m.prompt.value)
	}
}
