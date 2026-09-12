package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/vkovic/td/internal/store"
)

// sameMillisecond is a fixture whose ids are what td actually mints: three
// items created close enough together to share every leading character, so only
// the tail tells them apart.
func sameMillisecond(t *testing.T) *store.Store {
	t.Helper()
	s := newStore(t)
	save(t, s, item{id: "64qmbs3r", title: "fuzzy search", updated: ago(1)})
	save(t, s, item{id: "64qmnsxx", title: "ids for items", updated: ago(2)})
	save(t, s, item{id: "64s56z5v", title: "add dialog on top", updated: ago(3),
		doneAt: done(ago(3))})
	return s
}

// TestIDsAreHiddenUntilAsked: the tag is off on start, so the pane a reader
// opens is the pane they had before the key existed.
func TestIDsAreHiddenUntilAsked(t *testing.T) {
	m := newModel(t, sameMillisecond(t))
	resize(m, 60)

	if m.showIDs {
		t.Error("showIDs is on before i was pressed")
	}
	if got := plain(listText(m)); strings.Contains(got, "s3r") {
		t.Errorf("the list carries an id tag with showIDs off:\n%s", got)
	}
}

// TestIDToggleShowsTheShortID: i puts every row's ShortID ahead of its title,
// open rows and done ones alike, and i again takes it away.
func TestIDToggleShowsTheShortID(t *testing.T) {
	m := newModel(t, sameMillisecond(t))
	resize(m, 60)

	press(m, "i")
	if !m.showIDs {
		t.Fatal("i did not turn showIDs on")
	}
	for _, row := range lines(listText(m)) {
		if strings.TrimSpace(row) == "" || strings.Contains(row, doneRule) {
			continue
		}
		if !strings.Contains(row, "] ") {
			t.Fatalf("row has no box to anchor on:\n%s", row)
		}
	}
	got := plain(listText(m))
	for id, title := range map[string]string{"s3r": "fuzzy search", "sxx": "ids for items", "z5v": "add dialog on top"} {
		if !strings.Contains(got, id+" ") {
			t.Errorf("the list does not tag %q with %s:\n%s", title, id, got)
		}
	}

	press(m, "i")
	if m.showIDs {
		t.Fatal("i again did not turn showIDs off")
	}
	if strings.Contains(plain(listText(m)), "s3r") {
		t.Errorf("the tag survived being toggled off:\n%s", plain(listText(m)))
	}
}

// TestIDTagSitsBeforeTheTitle: the tag is a label on the row, between the done
// box and the title, not another trailing field beside the due date.
func TestIDTagSitsBeforeTheTitle(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "64qmbs3r", title: "fuzzy search", tags: []string{"cli"},
		due: "2026-09-30", updated: ago(3)})

	m := newModel(t, s)
	resize(m, 70)
	press(m, "i")

	line := plain(m.row(m.Entries()[0], true))
	box, tag, title := strings.Index(line, "[ ]"), strings.Index(line, "s3r"), strings.Index(line, "fuzzy")
	if !(box < tag && tag < title) {
		t.Errorf("the tag is not between the box and the title:\n%s", line)
	}
}

// TestIDTagSurvivesANarrowPane: the title gives way to the tag, not the other
// way round. A stub of a title with no tag beside it is a row a reader cannot
// name to a program, which is the whole point of the key.
func TestIDTagSurvivesANarrowPane(t *testing.T) {
	s := newStore(t)
	long := strings.Repeat("wire the epilogue ", 7) // 126 columns
	save(t, s, item{id: "64qmbs3r", title: long, tags: []string{"cli"},
		due: "2026-09-30", updated: ago(3)})

	m := newModel(t, s)
	press(m, "i")
	for _, width := range []int{60, 40, 30, 20} {
		resize(m, width)
		line := plain(m.row(m.Entries()[0], true))
		if !strings.Contains(line, "s3r") {
			t.Errorf("at %d columns the row dropped its id tag:\n%s", width, line)
		}
		if got := ansi.StringWidth(line); got > width {
			t.Errorf("at %d columns the row is %d wide:\n%s", width, got, line)
		}
	}
}

// TestIDTagIsResolvableByTheCLI: what the pane prints is what a reader types
// back. The tag is an id suffix, so ResolveRef — every td command's id argument
// — has to find the one item it names.
func TestIDTagIsResolvableByTheCLI(t *testing.T) {
	s := sameMillisecond(t)
	m := newModel(t, s)
	press(m, "i")

	for _, e := range m.Entries() {
		tag := store.ShortID(e.Item.ID)
		got, err := s.ResolveAll(tag, store.Active)
		if err != nil {
			t.Fatalf("ResolveAll(%q), the tag shown for %q: %v", tag, e.Item.Title, err)
		}
		if got.Item.ID != e.Item.ID {
			t.Errorf("ResolveAll(%q) = %q, want %q", tag, got.Item.ID, e.Item.ID)
		}
	}
}
