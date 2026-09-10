package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/vkovic/td/internal/store"
)

// TestEveryKeyIsDocumented: the table dispatches the keystroke and writes the
// help, so a key added without a help entry fails here rather than shipping
// undocumented.
func TestEveryKeyIsDocumented(t *testing.T) {
	for _, b := range keyMap() {
		if len(b.keys) == 0 {
			t.Errorf("a binding has no keys: %+v", b)
			continue
		}
		if strings.TrimSpace(b.help) == "" {
			t.Errorf("%s has no help", b.name())
		}
		if b.run == nil {
			t.Errorf("%s does nothing", b.name())
		}
	}
}

// TestNoKeyIsBoundTwice: two bindings answering the same keystroke would make
// which one runs depend on the table's order.
func TestNoKeyIsBoundTwice(t *testing.T) {
	seen := map[string]string{}
	for _, b := range keyMap() {
		for _, key := range b.keys {
			if was, ok := seen[key]; ok {
				t.Errorf("%q is bound by both %s and %s", key, was, b.name())
			}
			seen[key] = b.name()
		}
	}
}

// TestOverlayListsExactlyTheKeyTable: the overlay is generated, so it cannot
// describe a key that does nothing or miss one that does something.
func TestOverlayListsExactlyTheKeyTable(t *testing.T) {
	m := newModel(t, newStore(t))
	press(m, "?")
	view := plain(m.View())

	for _, b := range keyMap() {
		if !strings.Contains(view, b.name()) {
			t.Errorf("the overlay does not list %s:\n%s", b.name(), view)
		}
		if !strings.Contains(view, b.help) {
			t.Errorf("the overlay does not carry %s's help %q", b.name(), b.help)
		}
	}

	// And nothing else: every line between the heading and the closing notes
	// has to be a key from the table.
	table := map[string]bool{}
	for _, b := range keyMap() {
		table[b.name()] = true
	}
	for _, line := range lines(view) {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(line, "  ") {
			continue
		}
		if !table[fields[0]] {
			t.Errorf("the overlay lists %q, which is not in the key table", fields[0])
		}
	}
}

// TestLegendComesFromTheSameTable: the footer's one-line legend is generated
// too, so it cannot drift from the overlay.
func TestLegendComesFromTheSameTable(t *testing.T) {
	got := legend()
	for _, b := range keyMap() {
		if b.short == "" {
			continue
		}
		if !strings.Contains(got, b.keys[0]+" "+b.short) {
			t.Errorf("the legend is missing %s: %q", b.name(), got)
		}
	}
	// A key kept out of the legend on purpose is not in it.
	for _, b := range keyMap() {
		if b.short != "" {
			continue
		}
		if strings.Contains(got, " "+b.keys[0]+" ") {
			t.Errorf("%s has no short label but appears in the legend: %q", b.name(), got)
		}
	}
}

// TestOverlayIsModal: while the help is up it answers only the keys that close
// it, so a keystroke aimed at a covered list does not edit anything.
func TestOverlayIsModal(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "do not touch me", updated: ago(1)})

	m := newModel(t, s, withEditor("false"))
	press(m, "?")
	for _, key := range []string{"j", "x", "d", "a", "t", "g", "r"} {
		if cmd := press(m, key); cmd != nil {
			t.Errorf("%s acted while the overlay was up", key)
		}
	}
	if m.Cursor() != 0 {
		t.Errorf("the cursor moved to %d behind the overlay", m.Cursor())
	}
	if m.Entries()[0].Item.Done() {
		t.Error("x marked an item done behind the overlay")
	}
	if !m.showHelp {
		t.Error("a key that is not a close key closed the overlay")
	}

	press(m, "?")
	if m.showHelp {
		t.Error("? did not close the overlay")
	}
}

// TestOverlayClosesOnEscAndQ: q closes the overlay rather than quitting, so
// leaving the help does not leave td.
func TestOverlayClosesOnEscAndQ(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		m := newModel(t, newStore(t))
		press(m, "?")
		if cmd := press(m, key); cmd != nil {
			t.Errorf("%s from the overlay returned a command, want it to just close", key)
		}
		if m.showHelp {
			t.Errorf("%s did not close the overlay", key)
		}
	}
}

// TestOverlayNamesTheEditor: $EDITOR is the only editing surface, so the help
// says which one it will actually open.
func TestOverlayNamesTheEditor(t *testing.T) {
	m := newModel(t, newStore(t), withEditor("my-editor --wait"))
	press(m, "?")
	if view := plain(m.View()); !strings.Contains(view, "my-editor --wait") {
		t.Errorf("the overlay does not name the editor:\n%s", view)
	}
}

// TestFooterCarriesTheScopeFilterStatusAndFlash: the status line is the only
// place any of the four is stated.
func TestFooterCarriesTheScopeFilterStatusAndFlash(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "write the docs", tags: []string{"docs"}, updated: ago(1)})
	save(t, s, item{id: "bbb", title: "ship it", updated: ago(2)})

	m := newModel(t, s, func(o *Options) {
		o.Scope = store.ScopeChoice{Scope: store.Scope("acme")}
	})
	// The items above are in the global list, so switch to the merged view to
	// see them from a project scope.
	press(m, "g")
	press(m, "g")

	typeInto(m, "/", "docs")
	press(m, "enter")
	m.status = "committed"
	m.flashUntil = clock.Add(time.Minute)

	footer := m.footer()
	for _, want := range []string{"all scopes", "1 open, 1 total", "filtered /docs", "committed", externalFlash} {
		if !strings.Contains(footer, want) {
			t.Errorf("the footer is missing %q:\n%s", want, footer)
		}
	}
	if !strings.Contains(footer, legend()) {
		t.Errorf("the footer does not carry the key legend:\n%s", footer)
	}
}
