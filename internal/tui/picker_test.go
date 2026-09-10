package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/vkovic/td/internal/store"
)

// spread is a fixture store holding the global list and three projects, so an
// assertion about the picker's order is about the order and not about the one
// project that happens to exist.
func spread(t *testing.T) *store.Store {
	t.Helper()
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "global one", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "global two", updated: ago(2)})
	save(t, s, item{id: "ccc", title: "acme one", updated: ago(3), scope: store.Scope("acme")})
	save(t, s, item{id: "ddd", title: "td one", updated: ago(4), scope: store.Scope("td")})
	save(t, s, item{id: "eee", title: "td two", updated: ago(5), scope: store.Scope("td")})
	save(t, s, item{id: "fff", title: "zeta one", updated: ago(6), scope: store.Scope("zeta")})
	return s
}

// inProject scopes a model to a project, the way a .td marker resolves one.
func inProject(name string) func(*Options) {
	return func(o *Options) { o.Scope = store.ScopeChoice{Scope: store.Scope(name)} }
}

// TestPickerListsEveryList: global, the merged view, then every project the
// store holds in name order. The order is the point — it is what the cursor
// walks — so it is asserted as a sequence rather than as a set.
func TestPickerListsEveryList(t *testing.T) {
	m := newModel(t, spread(t), inProject("td"))
	press(m, "g")

	want := "global, all scopes, acme, td, zeta"
	if got := strings.Join(pickerLabels(m), ", "); got != want {
		t.Errorf("the picker lists %s, want %s", got, want)
	}
}

// TestPickerListsAProjectWithNothingOpen: a project directory the store holds
// is a list you can reach, whether or not anything is filed in it. It is the
// empty ones you most want to look at from another pane.
func TestPickerListsAProjectWithNothingOpen(t *testing.T) {
	s := spread(t)
	if _, err := s.Save(store.Scope("hollow"), store.Active, &store.Item{
		ID: "ggg", Title: "the only item", Created: ago(1), Updated: ago(1),
		DoneAt: done(ago(1)),
	}); err != nil {
		t.Fatal(err)
	}

	m := newModel(t, s)
	press(m, "g")
	if got := strings.Join(pickerLabels(m), ", "); !strings.Contains(got, "hollow") {
		t.Errorf("the picker lists %s, want the project with nothing open among them", got)
	}
}

// TestPickerListsTheScopeOnScreen: a marker resolves a project before anything
// is filed in it, so the store has no directory for it and Scopes() does not
// name it. Leaving it out would open the picker with its cursor on somebody
// else's list.
func TestPickerListsTheScopeOnScreen(t *testing.T) {
	m := newModel(t, spread(t), inProject("brand-new"))
	press(m, "g")

	want := "global, all scopes, acme, brand-new, td, zeta"
	if got := strings.Join(pickerLabels(m), ", "); got != want {
		t.Errorf("the picker lists %s, want %s", got, want)
	}
	if got := m.picker.rows[m.picker.cursor].label(); got != "brand-new" {
		t.Errorf("the cursor opened on %q, want the list on screen", got)
	}
}

// TestPickerOpensOnTheListOnScreen: the cursor starts on what the pane is
// already showing, whichever of the three kinds of list that is, so enter is
// always the key that changes nothing.
func TestPickerOpensOnTheListOnScreen(t *testing.T) {
	for _, showing := range []string{"td", "global", "all scopes", "zeta"} {
		m := newModel(t, spread(t), inProject("td"))
		if showing != "td" {
			pick(t, m, showing)
		}
		press(m, "g")
		if got := m.picker.rows[m.picker.cursor].label(); got != showing {
			t.Errorf("showing %s, the picker opened its cursor on %q", showing, got)
		}
		press(m, "enter")
		if got := m.scopeLabel(); got != showing {
			t.Errorf("enter on the list already showing moved the pane to %q", got)
		}
	}
}

// TestPickingAList: each kind of pick loads that list and names it in the
// footer. Global and the merged view do what the old three-way cycle did for
// them; a project is what the picker adds.
func TestPickingAList(t *testing.T) {
	for _, want := range []struct {
		label  string
		titles string
	}{
		{label: "global", titles: "global one,global two"},
		{label: "all scopes", titles: "global one,global two,acme one,td one,td two,zeta one"},
		{label: "acme", titles: "acme one"},
		{label: "zeta", titles: "zeta one"},
		{label: "td", titles: "td one,td two"},
	} {
		m := newModel(t, spread(t), inProject("td"))
		pick(t, m, want.label)

		if m.picker.open {
			t.Errorf("picking %s left the picker up", want.label)
		}
		if got := m.scopeLabel(); got != want.label {
			t.Errorf("picking %s left the footer naming %q", want.label, got)
		}
		if got := strings.Join(titles(m), ","); got != want.titles {
			t.Errorf("picking %s shows %s, want %s", want.label, got, want.titles)
		}
	}
}

// TestPickerEscLeavesTheListAlone: cancelling is a no-op, down to the cursor.
// The picker's own cursor moving is not the list's cursor moving.
func TestPickerEscLeavesTheListAlone(t *testing.T) {
	m := newModel(t, spread(t), inProject("td"))
	press(m, "j") // onto the second td item
	was, wasCursor := m.scopeLabel(), m.Cursor()

	press(m, "g")
	press(m, "j")
	press(m, "j")
	press(m, "esc")

	if m.picker.open {
		t.Error("esc left the picker up")
	}
	if got := m.scopeLabel(); got != was {
		t.Errorf("a cancelled pick moved the pane to %q, want it still on %q", got, was)
	}
	if got := m.Cursor(); got != wasCursor {
		t.Errorf("a cancelled pick moved the list cursor to %d, want it still on %d", got, wasCursor)
	}
}

// TestPickerEscDoesNotClearTheFilters: esc means cancel inside the picker. The
// same key clears the filters on the list underneath, and a cancelled pick
// that emptied the filter would be the picker reaching through its own
// overlay.
func TestPickerEscDoesNotClearTheFilters(t *testing.T) {
	m := newModel(t, spread(t), inProject("td"))
	typeInto(m, "/", "one")
	press(m, "enter")
	if m.filters.title != "one" {
		t.Fatalf("the filter did not take: %q", m.filters.title)
	}

	press(m, "g")
	press(m, "esc")

	if m.filters.title != "one" {
		t.Errorf("esc in the picker cleared the filter: %q", m.filters.title)
	}
	// And the key still works on the list itself.
	press(m, "esc")
	if m.filters.title != "" {
		t.Errorf("esc on the list left the filter as %q", m.filters.title)
	}
}

// TestFiltersCarryOverAPick: a pick is a reload, and the filters survive every
// other reload today. They narrow the new list the way they narrowed the old.
func TestFiltersCarryOverAPick(t *testing.T) {
	m := newModel(t, spread(t), inProject("td"))
	typeInto(m, "/", "one")
	press(m, "enter")

	pick(t, m, "all scopes")

	if m.filters.title != "one" {
		t.Errorf("the pick cleared the filter: %q", m.filters.title)
	}
	if got := strings.Join(titles(m), ","); got != "global one,acme one,td one,zeta one" {
		t.Errorf("the picked list shows %s, want the filter applied to it", got)
	}
}

// TestPickerIsModal: while it is up it answers only its own keys, so nothing is
// edited by a keystroke aimed at the list it covers.
func TestPickerIsModal(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "do not touch me", updated: ago(1)})

	m := newModel(t, s, withEditor("false"))
	press(m, "g")
	for _, key := range []string{"x", "d", "a", "t", "r", "/", "?"} {
		if cmd := press(m, key); cmd != nil {
			t.Errorf("%s acted while the picker was up", key)
		}
	}
	if m.Entries()[0].Item.Done() {
		t.Error("x marked an item done behind the picker")
	}
	if m.prompt.open() || m.showHelp {
		t.Error("a key opened another screen behind the picker")
	}
	if !m.picker.open {
		t.Error("a key that is not esc or enter closed the picker")
	}
}

// TestPickedProjectFilesAnAddIntoIt: a filed into the list on screen, which is
// the rule the global view already followed. The merged view is the exception
// and keeps filing into the directory's own scope.
func TestPickedProjectFilesAnAddIntoIt(t *testing.T) {
	for _, want := range []struct {
		pick, scope string
	}{
		{pick: "acme", scope: "acme"},
		{pick: "global", scope: ""},
		{pick: "all scopes", scope: "td"},
		{pick: "td", scope: "td"},
	} {
		s := spread(t)
		m := newModel(t, s, inProject("td"), withEditor("true"))
		pick(t, m, want.pick)

		press(m, "a")
		for _, r := range "raised from the pane" {
			press(m, string(r))
		}
		drain(t, m, press(m, "enter"))

		entries, err := s.List(store.Scope(want.scope), store.Active)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range entries {
			if e.Item.Title == "raised from the pane" {
				found = true
			}
		}
		if !found {
			t.Errorf("showing %s, the add did not land in %q", want.pick, store.Scope(want.scope).String())
		}
	}
}

// TestPickedProjectSurvivesAReload: the pick is view state, like the filters.
// A change landing while another project is on screen re-reads that project,
// not the one the marker resolved.
func TestPickedProjectSurvivesAReload(t *testing.T) {
	s := spread(t)
	m := newModel(t, s, inProject("td"))
	pick(t, m, "acme")

	// Something arrives from outside, as the watcher would report it.
	save(t, s, item{id: "hhh", title: "acme two", updated: ago(0), scope: store.Scope("acme")})
	m.Update(storeChangedMsg{})

	if got := m.scopeLabel(); got != "acme" {
		t.Errorf("the reload moved the pane to %q, want it still on acme", got)
	}
	if got := strings.Join(titles(m), ","); got != "acme two,acme one" {
		t.Errorf("the reload shows %s, want the picked project re-read", got)
	}
}

// TestPickerFitsANarrowPane: the pane td was written for is 60 columns, and a
// project name has no length limit at all.
func TestPickerFitsANarrowPane(t *testing.T) {
	s := spread(t)
	long := strings.Repeat("a-rather-long-project-name-", 4)
	if _, err := s.Save(store.Scope(long), store.Active, &store.Item{
		ID: "ggg", Title: "one", Created: ago(1), Updated: ago(1),
	}); err != nil {
		t.Fatal(err)
	}

	m := newModel(t, s, inProject("td"))
	resize(m, 60)
	press(m, "g")

	for _, line := range lines(m.View()) {
		if got := ansi.StringWidth(line); got > 60 {
			t.Errorf("a picker line is %d columns wide, want 60 or fewer: %q", got, line)
		}
	}
}

// TestPickerWindowsItsRows: a store with more projects than the pane is tall
// scrolls, and the cursor stays on screen — a picker whose cursor walks off
// the bottom is one you cannot see yourself using.
func TestPickerWindowsItsRows(t *testing.T) {
	s := newStore(t)
	for _, name := range []string{"p01", "p02", "p03", "p04", "p05", "p06", "p07", "p08", "p09", "p10"} {
		save(t, s, item{id: "id" + name, title: name + " one", updated: ago(1), scope: store.Scope(name)})
	}

	m := newModel(t, s)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	press(m, "g")

	for i := 0; i < 11; i++ {
		view := plain(m.pickerView())
		got := strings.Split(strings.TrimRight(view, "\n"), "\n")
		if len(got) > 8 {
			t.Fatalf("the picker is %d lines in an 8-line pane:\n%s", len(got), view)
		}
		if !strings.Contains(view, pickerHint) {
			t.Errorf("the picker dropped the line saying how to work it:\n%s", view)
		}
		want := "❯ " + m.picker.rows[m.picker.cursor].label()
		if !strings.Contains(view, want) {
			t.Errorf("after %d presses the cursor is off screen, want %q in:\n%s", i, want, view)
		}
		press(m, "j")
	}

	// And the cursor clamps at the end rather than wrapping, as the list's own
	// cursor does.
	if got := m.picker.cursor; got != len(m.picker.rows)-1 {
		t.Errorf("the picker cursor is on row %d after walking past the end of %d", got, len(m.picker.rows))
	}
	for i := 0; i < 20; i++ {
		press(m, "k")
	}
	if m.picker.cursor != 0 {
		t.Errorf("the picker cursor is on row %d after walking past the start", m.picker.cursor)
	}
}
