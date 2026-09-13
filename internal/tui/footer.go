package tui

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// minFilterEcho is how much of an active filter the status line keeps when the
// pane is too narrow for all of it. The filter text is the explanation for why
// rows are missing, so it is the last thing the line gives up.
const minFilterEcho = 12

// footer is what sits under the list: a status line carrying the counts, any
// active filter, what the last epilogue did and the external-change note, and
// under it the key legend, drawn from the same table as the overlay. The list's
// name is not on it; that heads the pane (see header).
//
// Both lines are fitted to the pane. The status line is not bounded by
// anything but the user — the filter echo is whatever was typed into / — so a
// status line wider than the pane is ordinary use rather than an edge case.
//
// A line that will not fit sheds by rank, the way the legend sheds keys and
// the filter echo gives way to the status around it. The total goes first: it
// is the open count plus a done section that is on screen anyway, and it sits
// in front of the off-screen counts, which are the clause that makes the
// window honest and are the last thing that should pay for it. Only a line
// still too wide after that is truncated whole.
func (m *Model) footer(off hidden) string {
	open := 0
	for _, e := range m.shown {
		if !e.Item.Done() {
			open++
		}
	}
	tally := fmt.Sprintf("%d open, %d total", open, len(m.shown))

	var rest string
	// A list that just stops at the bottom of the pane looks like the whole
	// list. Saying what is off-screen is what makes the window visible.
	if off.any() {
		rest += " · " + off.describe()
	}
	if f := m.filters.describe(); f != "" {
		// Trimmed before the line is assembled: the filter sits in the middle,
		// so trimming the whole line from the right would eat the status and
		// the flash to keep filter text nobody can read anyway.
		rest += " · filtered " + m.fitFilter(f, ansi.StringWidth(tally+rest))
	}
	if m.status != "" {
		rest += " · " + m.status
	}
	if m.flashing() {
		rest += " · " + externalFlash
	}

	// Nothing is shed at a width of zero, which is a terminal that has not
	// announced itself rather than a narrow one.
	if m.width > 0 && ansi.StringWidth(tally+rest) > m.width {
		tally = fmt.Sprintf("%d open", open)
	}
	line := tally + rest
	if m.width > 0 {
		line = ansi.Truncate(line, m.width, "…")
	}
	return line + "\n" + legend(m.width)
}

// fitTail trims a line to the pane from the left, keeping its end. spent is
// what has already been drawn on the line ahead of it.
func (m *Model) fitTail(line string, spent int) string {
	room := m.width - spent
	if m.width <= 0 || room < minTitle || ansi.StringWidth(line) <= room {
		return line
	}
	// TruncateLeft drops cells and then prepends the marker, so the marker's
	// own column has to come out of the cells dropped.
	return ansi.TruncateLeft(line, ansi.StringWidth(line)-room+1, "…")
}

// fitFilter trims the echoed filter to what the status line can spare, keeping
// enough of it to recognise. spent is what the line costs before the filter.
func (m *Model) fitFilter(f string, spent int) string {
	if m.width <= 0 {
		return f
	}
	room := max(m.width-spent-ansi.StringWidth(" · filtered "), minFilterEcho)
	if ansi.StringWidth(f) <= room {
		return f
	}
	return ansi.Truncate(f, room, "…")
}
