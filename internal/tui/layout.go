package tui

import (
	"fmt"
	"strings"
)

// split divides the list area between the two regions when the whole list will
// not fit in it. The area is spent in the order the reader cannot do without,
// and each claim is met only if the one before it was:
//
//  1. The row the cursor is on, in whichever region holds it. A pane showing
//     one row of list must show the row j and k are moving.
//  2. The first open row, if there are open items. Open is what is still to do,
//     and a pane with room for a row of it must spend a row on it.
//  3. The rule, if both sections have rows. It is reserved against open's other
//     rows, never against either row above: in the four-line pane that reserved
//     it against the cursor's row, the list read "── done ──" and one done row,
//     with no open item on screen at all.
//  4. The rest of open, up to every row it has.
//  5. The rest of done.
//
// Open is therefore served before done past their first rows, so a long open
// list still ends in the rule, which is then the only thing on screen saying a
// done section exists at all. And a cursor walked down into a done section
// squeezed to its rule grows it back a line at open's expense, and gives the
// line up again on the way out.
func split(capacity, open, done int, cursorInDone bool) (openHeight, doneHeight int, ruled bool) {
	ruled = open > 0 && done > 0
	rule := 0
	if ruled {
		rule = 1
	}
	// A capacity of zero is a height Bubble Tea has not sent yet: nothing is
	// windowed, and nothing is padded either.
	if capacity <= 0 || open+rule+done <= capacity {
		return open, done, ruled
	}

	left := capacity
	if cursorInDone && done > 0 {
		doneHeight, left = 1, left-1
	}
	if open > 0 && left > 0 {
		openHeight, left = 1, left-1
	}
	if ruled && left > 0 {
		left--
	} else {
		ruled = false
	}
	if grow := min(open-openHeight, left); grow > 0 {
		openHeight, left = openHeight+grow, left-grow
	}
	doneHeight += min(done-doneHeight, left)
	return openHeight, doneHeight, ruled
}

// region is one windowed section: the lines on screen and how many of its rows
// fell off each end of it.
type region struct {
	lines        []string
	total        int
	above, below int
}

// window slices rows to height, moving top the least that keeps the cursor row
// on screen, so a region holds still until the cursor would otherwise leave it.
// top is the caller's own field, updated in place. A cursor of -1 says the
// cursor is in the other region and this one does not chase it.
//
// A region given no height at all reports every row as below it, and offscreen
// puts them back on the right side of the fold: a squeezed-out open section
// sits above what is drawn, not under it.
func window(rows []string, height, cursor int, top *int) region {
	if height >= len(rows) {
		*top = 0
		return region{lines: rows, total: len(rows)}
	}
	if height <= 0 {
		*top = 0
		return region{total: len(rows), below: len(rows)}
	}
	*top = min(max(*top, 0), len(rows)-height)
	if cursor >= 0 {
		if cursor < *top {
			*top = cursor
		}
		if cursor >= *top+height {
			*top = cursor - height + 1
		}
	}
	return region{
		lines: rows[*top : *top+height],
		total: len(rows),
		above: *top,
		below: len(rows) - *top - height,
	}
}

// hidden is how many rows are off screen, sorted into where a reader would go
// looking for them: above everything on screen, between the two sections, and
// below everything.
type hidden struct{ above, mid, below int }

// any reports whether anything is off screen at all.
func (h hidden) any() bool { return h.above > 0 || h.mid > 0 || h.below > 0 }

// offscreen sorts what the two windows hid into those three places. There is a
// middle to be in only when both sections are drawn: the fold is the rule, or
// the last open row above the first done one. A section the pane draws nothing
// of — no rows and, for done, not even its rule — takes its hidden rows to its
// own side of that fold, because a reader told rows are "between" would be
// looking for a join that is not on screen.
func offscreen(open, done region, ruled bool) hidden {
	if len(done.lines) == 0 && !ruled {
		return hidden{above: open.above, below: open.below + done.total}
	}
	if len(open.lines) == 0 {
		return hidden{above: open.total + done.above, below: done.below}
	}
	h := hidden{above: open.above, mid: open.below + done.above, below: done.below}
	// The rule is drawn but no done row under it: what the done window hid
	// above its own top is still under the rule, and so is below the fold.
	if len(done.lines) == 0 {
		h.below, h.mid = h.below+done.above, h.mid-done.above
	}
	return h
}

// describe says where the rows that are off screen went. Two windows can each
// hide rows, and a row hidden between them is neither above the list nor below
// it: it is in the fold where the open section stops and the done one starts.
// Counting those as "above" or "below" would send the reader scrolling the
// wrong way, or the right way past the row they wanted.
//
// A count of zero is left out rather than printed, because "0 above" is a fact
// about a place nothing is hidden in.
func (h hidden) describe() string {
	var parts []string
	for _, p := range []struct {
		n    int
		word string
	}{{h.above, "above"}, {h.mid, "between"}, {h.below, "below"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.word))
		}
	}
	return strings.Join(parts, ", ")
}
