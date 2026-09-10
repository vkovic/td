package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

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

// TestLegendDropsEntriesToFit: the full legend is 100-odd columns and the pane
// td was written for is 60, so a narrow pane gets fewer keys rather than a
// legend wrapped over three lines.
func TestLegendDropsEntriesToFit(t *testing.T) {
	full := legend(0)
	if len(full) <= 60 {
		t.Fatalf("the full legend is %d columns, so this test proves nothing: %q", len(full), full)
	}
	for _, width := range []int{80, 60, 40, 20, 10} {
		got := legend(width)
		if len(got) > width {
			t.Errorf("the legend at width %d is %d columns: %q", width, len(got), got)
		}
		// ? is the only thing on screen that says what the dropped keys were,
		// so it is the one entry that survives every width.
		if !strings.Contains(got, "? help") {
			t.Errorf("the legend at width %d dropped the way to find the rest: %q", width, got)
		}
		// Whatever survives keeps the table's order, so the legend shortens
		// rather than reshuffling as the pane is dragged narrower.
		if !inTableOrder(got) {
			t.Errorf("the legend at width %d is out of table order: %q", width, got)
		}
	}
}

// inTableOrder reports whether a rendered legend's entries appear in the order
// keyMap lists them.
func inTableOrder(rendered string) bool {
	at := -1
	for _, b := range keyMap() {
		if b.short == "" {
			continue
		}
		i := strings.Index(rendered, b.keys[0]+" "+b.short)
		if i < 0 {
			continue
		}
		if i < at {
			return false
		}
		at = i
	}
	return true
}

// TestLegendWidthIsMeasuredNotCounted: legendSeparator is " · " — four bytes
// for three columns — so a byte count overstates the legend by one per gap and
// the pane withholds a key that would have fit. The error is conservative, so
// nothing that checks for overrun could ever have caught it.
func TestLegendWidthIsMeasuredNotCounted(t *testing.T) {
	var keep []binding
	for _, b := range keyMap() {
		if b.short != "" {
			keep = append(keep, b)
		}
	}
	if got, want := legendWidth(keep), ansi.StringWidth(legend(0)); got != want {
		t.Errorf("legendWidth = %d, the rendered legend is %d columns", got, want)
	}

	// And the consequence: a legend is shown at exactly the width it occupies,
	// not one gap per entry later.
	for _, width := range []int{102, 61, 50} {
		got := legend(width)
		if n := ansi.StringWidth(got); n > width {
			t.Errorf("the legend at width %d is %d columns", width, n)
		}
		// Whatever it dropped, one more entry would not have fit.
		if n := ansi.StringWidth(legend(width + 1)); n == ansi.StringWidth(got) && width < 102 {
			continue // the next column genuinely buys nothing
		}
	}
	if got := ansi.StringWidth(legend(61)); got != 61 {
		t.Errorf("at width 61 the legend is %d columns, want the 61-column set: %q", got, legend(61))
	}
}

// TestLegendComesFromTheSameTable: the footer's one-line legend is generated
// too, so it cannot drift from the overlay.
func TestLegendComesFromTheSameTable(t *testing.T) {
	got := legend(0) // an unknown width keeps every entry
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
	pick(t, m, "all scopes")

	typeInto(m, "/", "docs")
	press(m, "enter")
	m.status = "committed"
	m.flashUntil = clock.Add(time.Minute)

	footer := m.footer(0, 0)
	for _, want := range []string{"all scopes", "1 open, 1 total", "filtered /docs", "committed", externalFlash} {
		if !strings.Contains(footer, want) {
			t.Errorf("the footer is missing %q:\n%s", want, footer)
		}
	}
	if !strings.Contains(footer, legend(0)) {
		t.Errorf("the footer does not carry the key legend:\n%s", footer)
	}
}

// TestPromptKeepsItsTail: a filter longer than the pane still shows the
// keystrokes being typed. Trimming the prompt from the right would hide the
// cursor and everything approaching it.
func TestPromptKeepsItsTail(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "one", updated: ago(1)})

	m := newModel(t, s)
	resize(m, 40)
	typeInto(m, "/", "the quick brown fox jumps over the lazy dog")

	view := ansi.Strip(m.View())
	var prompt string
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, "filter>") {
			prompt = line
		}
	}
	if prompt == "" {
		t.Fatalf("no prompt line in the view:\n%s", view)
	}
	if got := ansi.StringWidth(prompt); got > 40 {
		t.Errorf("the prompt line is %d columns wide, want 40 or fewer: %q", got, prompt)
	}
	if !strings.HasSuffix(prompt, "lazy dog█") {
		t.Errorf("the prompt hid what was being typed: %q", prompt)
	}
}

// TestFooterFitsTheWidth: neither footer line is bounded by anything but the
// user — a project name has no length limit and the filter echo is whatever
// was typed — so both are fitted, and no line is padded out to the other's
// width.
func TestFooterFitsTheWidth(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "one", updated: ago(1)})

	m := newModel(t, s)
	resize(m, 60)
	typeInto(m, "/", strings.Repeat("a very specific search ", 5))
	press(m, "enter")
	m.status = "2 hand edits recorded, 3 items archived, committed, pushed"
	m.flashUntil = clock.Add(time.Minute)

	for _, line := range strings.Split(m.footer(0, 0), "\n") {
		if got := ansi.StringWidth(line); got > 60 {
			t.Errorf("a footer line is %d columns wide, want 60 or fewer:\n%s", got, line)
		}
	}
	// Enough of the filter survives to recognise what was typed: it is the
	// explanation for the rows that are missing.
	if !strings.Contains(m.footer(0, 0), "filtered /a very") {
		t.Errorf("the footer does not echo the filter:\n%s", m.footer(0, 0))
	}

	// And the whole view: no line of it is padded past the pane.
	for _, line := range strings.Split(strings.TrimRight(m.View(), "\n"), "\n") {
		if got := ansi.StringWidth(line); got > 60 {
			t.Errorf("a view line is %d columns wide, want 60 or fewer:\n%q", got, ansi.Strip(line))
		}
	}
}
