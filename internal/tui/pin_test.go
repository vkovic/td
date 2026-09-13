package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// TestPinTogglesAndHeadsTheSection: p moves the row to the top of the open
// section and p again sends it back, and neither changes the age the row shows.
func TestPinTogglesAndHeadsTheSection(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "fresh", updated: ago(1)})
	save(t, s, item{id: "bbb", title: "stale", updated: ago(2)})

	m := newModel(t, s)
	m.cursor = indexOf(m, "stale")
	drain(t, m, press(m, "p"))

	e := findTitle(t, m, "stale")
	if !e.Item.Pinned {
		t.Fatal("p did not pin the item")
	}
	if !e.Item.Updated.Equal(ago(2)) {
		t.Errorf("updated is %s, want the pin to leave it at %s", e.Item.Updated, ago(2))
	}
	if got := strings.Join(titles(m), ","); got != "stale,fresh" {
		t.Errorf("the listing is %s, want the pinned item first", got)
	}
	if !strings.Contains(plain(listText(m)), pinGlyph+" stale") {
		t.Errorf("the pinned row carries no %s before its title:\n%s", pinGlyph, plain(listText(m)))
	}

	m.cursor = indexOf(m, "stale")
	drain(t, m, press(m, "p"))
	if findTitle(t, m, "stale").Item.Pinned {
		t.Error("p again did not unpin the item")
	}
	if got := strings.Join(titles(m), ","); got != "fresh,stale" {
		t.Errorf("the listing is %s, want the timestamp order back", got)
	}
}

// TestAPinnedItemMarkedDoneHeadsTheDoneSection: the pin survives x, and ranks
// the item first below the rule — even over a done item completed after it.
func TestAPinnedItemMarkedDoneHeadsTheDoneSection(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "pinned open", updated: ago(3), pinned: true})
	save(t, s, item{id: "bbb", title: "other open", updated: ago(2)})
	later := clock.Add(time.Hour)
	save(t, s, item{id: "ccc", title: "done later", updated: ago(1), doneAt: &later})

	m := newModel(t, s)
	m.cursor = indexOf(m, "pinned open")
	drain(t, m, press(m, "x"))

	e := findTitle(t, m, "pinned open")
	if !e.Item.Done() || !e.Item.Pinned {
		t.Fatalf("x left the item done=%v pinned=%v, want both", e.Item.Done(), e.Item.Pinned)
	}
	if got := strings.Join(titles(m), ","); got != "other open,pinned open,done later" {
		t.Errorf("the listing is %s, want the pinned item heading the done section", got)
	}
	if !crossedRule(t, m, "pinned open") {
		t.Error("the pinned done item is not below the rule")
	}
}

// TestThePinSlotKeepsTitlesAligned: every row reserves the glyph's two columns,
// so a pinned title and an unpinned one start in the same column.
func TestThePinSlotKeepsTitlesAligned(t *testing.T) {
	s := newStore(t)
	save(t, s, item{id: "aaa", title: "pinned row", updated: ago(1), pinned: true})
	save(t, s, item{id: "bbb", title: "plain row", updated: ago(2)})
	save(t, s, item{id: "ccc", title: "done row", updated: ago(3), doneAt: done(ago(3))})

	m := newModel(t, s)
	resize(m, 60)
	for _, showIDs := range []bool{false, true} {
		m.showIDs = showIDs
		column := map[string]int{}
		for _, line := range lines(listText(m)) {
			for _, title := range []string{"pinned row", "plain row", "done row"} {
				if at := strings.Index(line, title); at >= 0 {
					column[title] = ansi.StringWidth(line[:at])
				}
			}
		}
		if len(column) != 3 {
			t.Fatalf("not every title was drawn:\n%s", plain(listText(m)))
		}
		if column["pinned row"] != column["plain row"] || column["plain row"] != column["done row"] {
			t.Errorf("with ids %v, titles start at columns %v, want one column:\n%s",
				showIDs, column, plain(listText(m)))
		}
	}
}
