package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/vkovic/td/internal/store"
)

// minTitle is the narrowest a title is ever squeezed to. A row cut below this
// says nothing that tells it apart from the row above it.
const minTitle = 8

// cell is one of the pieces that follow the title, with what it costs to lose.
// A pane too narrow for all of them drops whole cells, highest cost last to
// survive, rather than letting the row overrun and be clipped by the terminal.
type cell struct {
	text string
	// drop orders what goes first when the row will not fit: higher goes
	// sooner. The age is vaguest, the due date is the one that is actually
	// urgent, so they sit at opposite ends.
	drop int
}

// row renders one item: the cursor, a done marker, the title, its tags, its due
// date and how long ago it was updated. Empty fields take no space at all, so a
// list with no tags carries no gap where the tags would be.
//
// The title is the first cell to give way, and below a stub of a title the
// trailing cells start going too. Nothing is left to the terminal to clip: a
// clipped row loses its right-hand end with nothing on screen to say so, which
// is how a 40-column pane came to show "due 2026-" and no age at all.
func (m *Model) row(e store.Entry, selected bool) string {
	marker := "  "
	if selected {
		marker = m.styles.cursor.Render("❯ ")
	}

	box := "[ ]"
	if e.Item.Done() {
		box = "[x]"
	}

	before := []string{marker, box}
	// The id goes ahead of the title, the one place it reads as a label on the
	// row rather than as another trailing field competing with the due date. It
	// is not a droppable cell: a pane too narrow for it is a pane where the tag
	// is the only way left to say which item a stub of a title belongs to.
	if m.showIDs {
		before = append(before, m.styles.id.Render(store.ShortID(e.Item.ID)))
	}
	// Only the merged view needs the label: in a single-scope view every row
	// would carry the same one, which is what the status line already says.
	// It sits before the title, where td ls --all puts its SCOPE column.
	if m.view.merged {
		before = append(before, m.styles.scope.Render(e.Ref.Scope.String()))
	}

	var after []cell
	if len(e.Item.Tags) > 0 {
		after = append(after, cell{text: m.styles.tags.Render("#" + strings.Join(e.Item.Tags, " #")), drop: 2})
	}
	if e.Item.Due != nil {
		text := "due " + e.Item.Due.String()
		style := m.styles.due
		if m.overdue(e.Item) {
			style = m.styles.overdue
		}
		after = append(after, cell{text: style.Render(text), drop: 1})
	}
	if age := relative(e.Item.Updated, m.now()); age != "" {
		after = append(after, cell{text: m.styles.updated.Render(age), drop: 3})
	}
	after = m.affordable(before, after)

	style := m.styles.title
	if e.Item.Done() {
		style = m.styles.done
	}
	texts := append([]string{}, before...)
	texts = append(texts, m.fitTitle(style.Render(e.Item.Title), before, after))
	for _, c := range after {
		texts = append(texts, c.text)
	}
	// The last resort, for a pane too narrow even for the marker and a stub:
	// clip with an ellipsis rather than let the terminal clip silently.
	return m.fit(strings.Join(texts, " "))
}

// affordable drops trailing cells, costliest-to-lose last, until the row has
// room for a stub of a title. The cells that survive keep their own order, so
// a narrowing pane loses cells from a stable layout rather than rearranging
// what is left.
//
// One consequence looks like a bug in a screenshot and is not: a narrower pane
// can show more title than a wider one. At 50 columns the tags still fit and
// the title reads "wire the epi…"; at 40 the tags are dropped and their 13
// columns go to the title, which reads "wire the epilogue…". Losing a whole
// cell frees more than the column it cost.
func (m *Model) affordable(before []string, after []cell) []cell {
	if m.width <= 0 {
		return after
	}
	for len(after) > 0 && m.width-spent(before, after) < minTitle {
		worst := 0
		for i, c := range after {
			if c.drop > after[worst].drop {
				worst = i
			}
		}
		after = append(after[:worst:worst], after[worst+1:]...)
	}
	return after
}

// spent is what every cell but the title costs, including the single space
// between each pair of cells and the title's own two.
func spent(before []string, after []cell) int {
	total := len(before) + len(after)
	for _, text := range before {
		total += ansi.StringWidth(text)
	}
	for _, c := range after {
		total += ansi.StringWidth(c.text)
	}
	return total
}

// fitTitle trims a rendered title to whatever the other cells leave of the
// pane's width. Before the first WindowSizeMsg the width is unknown, and a row
// renders at whatever length it comes to.
//
// The trim runs on the styled title, never on the raw one. Lip Gloss wraps a
// done title's strikethrough around every rune, so its rendered bytes are
// several times its display width; slicing the raw string and styling after
// would look right in every test that strips the styling and corrupt exactly
// the rows that carry it.
func (m *Model) fitTitle(title string, before []string, after []cell) string {
	if m.width <= 0 {
		return title
	}
	room := max(m.width-spent(before, after), minTitle)
	if ansi.StringWidth(title) <= room {
		return title
	}
	return ansi.Truncate(title, room, "…")
}

// overdue reports whether an open item's due date has passed. A done item is
// never overdue: finishing it late is not a thing the list needs to nag about.
//
// Dates are compared by day, not by instant, so an item due today stops being
// merely due only when tomorrow starts.
func (m *Model) overdue(it *store.Item) bool {
	if it.Due == nil || it.Done() {
		return false
	}
	now := m.now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	due := it.Due.Time
	return due.Before(today)
}

// relative renders a timestamp as an age, which is what a list refreshed in
// place needs: "3m ago" tells you something changed, a wall clock does not.
// Anything older than a week reads as a date instead, where the exact day is
// more use than a large number of days.
func relative(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
