package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/vkovic/td/internal/store"
)

// styles are every style the list uses, built once against one renderer so a
// test can force a color profile and see the styling in the rendered string.
type styles struct {
	title    lipgloss.Style
	done     lipgloss.Style
	tags     lipgloss.Style
	due      lipgloss.Style
	overdue  lipgloss.Style
	scope    lipgloss.Style
	updated  lipgloss.Style
	cursor   lipgloss.Style
	rule     lipgloss.Style
	warning  lipgloss.Style
	footer   lipgloss.Style
	prompt   lipgloss.Style
	emptyMsg lipgloss.Style
}

// newStyles builds the palette. A nil renderer means Lip Gloss's default, which
// reads the real terminal.
func newStyles(r *lipgloss.Renderer) styles {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	dim := lipgloss.AdaptiveColor{Light: "244", Dark: "244"}
	return styles{
		title:    r.NewStyle(),
		done:     r.NewStyle().Foreground(dim).Strikethrough(true),
		tags:     r.NewStyle().Foreground(lipgloss.Color("5")),
		due:      r.NewStyle().Foreground(lipgloss.Color("4")),
		overdue:  r.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		scope:    r.NewStyle().Foreground(lipgloss.Color("2")),
		updated:  r.NewStyle().Foreground(dim),
		cursor:   r.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		rule:     r.NewStyle().Foreground(dim),
		warning:  r.NewStyle().Foreground(lipgloss.Color("3")),
		footer:   r.NewStyle().Foreground(dim),
		prompt:   r.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		emptyMsg: r.NewStyle().Foreground(dim),
	}
}

// doneRule separates the open section from the done one. It is drawn only when
// both sections have rows, so a list of nothing but open items carries no rule.
const doneRule = "── done ──"

// View renders the whole screen, top to bottom: the warnings, the open rows,
// the blank gap, the done section, the prompt and error lines, and the footer.
//
// Everything is bounded by the pane in both directions. The list is windowed
// to whatever height the chrome leaves it, because a pane that renders more
// lines than it has hands the terminal the choice of which end to keep — and
// the terminal keeps the tail, which for a list sorted newest-first is the
// oldest items and never the one just added.
//
// The list area is filled rather than merely fitted: whatever it does not need
// becomes the gap between the two sections. That is what holds the done
// section against the bottom of the list and the footer against the bottom of
// the pane, instead of both floating up under a short list.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.picker.open {
		return m.pickerView()
	}
	if m.showHelp {
		return m.helpView()
	}

	var head, tail []string
	for _, line := range m.notice("warning: ", m.warn) {
		head = append(head, m.styles.warning.Render(line))
	}
	if m.prompt.open() {
		// The prompt keeps its tail rather than its head: what you are typing
		// is at the end of it, and a prompt that stops showing your keystrokes
		// is worse than one that has scrolled its start away.
		label := m.styles.prompt.Render(m.prompt.label + "> ")
		tail = append(tail, label+m.fitTail(m.prompt.value+"█", ansi.StringWidth(label)))
	}
	for _, line := range m.notice("error: ", m.err) {
		tail = append(tail, m.styles.warning.Render(line))
	}

	// The footer is two lines, and is built last because it reports how much of
	// the list did not fit. In a pane too short for both it and a line of list,
	// it is the footer that gives way — the legend first, then the status
	// line. A pane showing nothing but chrome says nothing about your todos,
	// and the legend already sheds keys by rank, so shedding itself at the
	// last extremity is the same rule carried one step further.
	footerLines := 2
	for footerLines > 0 && m.height > 0 && m.height-len(head)-len(tail)-footerLines < 1 {
		footerLines--
	}
	body, off := m.body(m.capacity(len(head) + len(tail) + footerLines))

	var b strings.Builder
	for _, line := range head {
		fmt.Fprintln(&b, line)
	}
	for _, line := range body {
		fmt.Fprintln(&b, line)
	}
	for _, line := range tail {
		fmt.Fprintln(&b, line)
	}
	// Rendered a line at a time: Lip Gloss pads every line of a multi-line
	// block out to the widest one, which would trail the status line with
	// however many spaces the legend is longer by.
	for _, line := range strings.Split(m.footer(off), "\n")[:footerLines] {
		fmt.Fprintln(&b, m.styles.footer.Render(line))
	}
	// No trailing newline. A view that ends with one occupies a line more than
	// it drew, and a view exactly as tall as the pane then scrolls its own top
	// row off — which is the cursor, since the list opens at the newest item.
	return strings.TrimSuffix(b.String(), "\n")
}

// capacity is how many lines the list itself may occupy: the pane, less the
// chrome above and below it. Zero means the height is not known yet — Bubble
// Tea sends it after the model is built — and the list is not windowed at all.
//
// At least one line is always given to the list. A pane too short for its own
// chrome is already unusable; showing nothing of the list would not help.
func (m *Model) capacity(chrome int) int {
	if m.height <= 0 {
		return 0
	}
	return max(m.height-chrome, 1)
}

// body is the list area, filled to capacity: the open rows at the top, the
// done section at the bottom, and the blank gap between them. It reports what
// the two windows left off screen, which is what the footer counts.
//
// The regions window separately, each moving the least that puts the cursor
// back on screen, so scrolling one does not drag the other.
func (m *Model) body(capacity int) (lines []string, off hidden) {
	openRows, doneRows := m.sections()
	// m.shown is sorted open-before-done, so the cursor's index into it is
	// also its index into the open rows, or past their end into the done ones.
	openCursor, doneCursor := m.cursor, -1
	if m.cursor >= len(openRows) {
		openCursor, doneCursor = -1, m.cursor-len(openRows)
	}
	openHeight, doneHeight, ruled := split(capacity, len(openRows), len(doneRows), doneCursor >= 0)
	openReg := window(openRows, openHeight, openCursor, &m.openTop)
	doneReg := window(doneRows, doneHeight, doneCursor, &m.doneTop)

	used := len(openReg.lines) + len(doneReg.lines)
	if ruled {
		used++
	}
	lines = append(lines, openReg.lines...)
	// The gap is what is left over, and there is something left over only when
	// nothing was windowed: an overflowing list area is already full.
	for range max(capacity-used, 0) {
		lines = append(lines, "")
	}
	if ruled {
		lines = append(lines, m.styles.rule.Render(doneRule))
	}
	lines = append(lines, doneReg.lines...)
	return lines, offscreen(openReg, doneReg, ruled)
}

// sections renders the list as the two regions the pane draws it in: the open
// rows and the done ones, each in m.shown's order.
//
// An empty list has the line that says why in place of its open rows, so it
// reads from the top of the list area with the footer still on the bottom.
func (m *Model) sections() (open, done []string) {
	if len(m.shown) == 0 {
		return []string{m.styles.emptyMsg.Render(m.fit(m.emptyLine()))}, nil
	}
	for i, e := range m.shown {
		row := m.row(e, i == m.cursor)
		if e.Item.Done() {
			done = append(done, row)
		} else {
			open = append(open, row)
		}
	}
	return open, done
}

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
	if open > 0 && openHeight == 0 && left > 0 {
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

// minFilterEcho is how much of an active filter the status line keeps when the
// pane is too narrow for all of it. The filter text is the explanation for why
// rows are missing, so it is the last thing the line gives up.
const minFilterEcho = 12

// minScopeLabel is how much of the list's name the status line keeps once the
// name is what will not fit. Ten columns is "all scopes", the one label nobody
// can rename, and enough of a project name to tell one list from another.
const minScopeLabel = 10

// footer is what sits under the list: a status line naming the scope, the
// counts, any active filter, what the last epilogue did and the external-change
// note, and under it the key legend, drawn from the same table as the overlay.
//
// Both lines are fitted to the pane. Neither is bounded by anything but the
// user: the filter echo is whatever was typed into /, and a project name has
// no length limit at all, so a status line wider than the pane is ordinary use
// rather than an edge case.
//
// A line that will not fit sheds by rank, the way the legend sheds keys and
// the filter echo gives way to the status around it. The total goes first: it
// is the open count plus a done section that is on screen anyway, and it sits
// in front of the off-screen counts, which are the clause that makes the
// window honest and are the last thing that should pay for it. Then the scope
// label is elided down to minScopeLabel. Only a line still too wide after both
// is truncated whole, which is what used to happen first and took the counts
// with it: at sixty columns a project named in ten characters truncated to
// "· 4 between, 10 b…".
func (m *Model) footer(off hidden) string {
	open := 0
	for _, e := range m.shown {
		if !e.Item.Done() {
			open++
		}
	}
	label := m.scopeLabel()
	tally := fmt.Sprintf(" · %d open, %d total", open, len(m.shown))

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
		rest += " · filtered " + m.fitFilter(f, ansi.StringWidth(label+tally+rest))
	}
	if m.status != "" {
		rest += " · " + m.status
	}
	if m.flashing() {
		rest += " · " + externalFlash
	}

	// Nothing is shed at a width of zero, which is a terminal that has not
	// announced itself rather than a narrow one.
	if m.width > 0 && ansi.StringWidth(label+tally+rest) > m.width {
		tally = fmt.Sprintf(" · %d open", open)
	}
	if m.width > 0 && ansi.StringWidth(label+tally+rest) > m.width {
		label = m.fitLabel(label, ansi.StringWidth(tally+rest))
	}
	line := label + tally + rest
	if m.width > 0 {
		line = ansi.Truncate(line, m.width, "…")
	}
	return line + "\n" + legend(m.width)
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

// notice renders one error as the lines the pane draws for it, each carrying
// the prefix and each fitted to the width.
//
// One error is often several: a listing that met three unparsable files
// reports an errors.Join, whose Error() is its causes separated by newlines.
// Prefixing that whole string once left every line after the first unlabelled,
// reading as if the pane had printed a bare filename at somebody.
func (m *Model) notice(prefix string, err error) []string {
	if err == nil {
		return nil
	}
	causes := strings.Split(strings.TrimRight(err.Error(), "\n"), "\n")
	lines := make([]string, 0, len(causes))
	for _, cause := range causes {
		lines = append(lines, m.fit(prefix+cause))
	}
	return lines
}

// fit trims a whole line to the pane, from the right. A width of zero is a
// terminal that has not announced itself yet, and the line is left alone.
func (m *Model) fit(line string) string {
	if m.width <= 0 || ansi.StringWidth(line) <= m.width {
		return line
	}
	return ansi.Truncate(line, m.width, "…")
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

// fitLabel trims the scope label to the room the rest of the status line leaves
// it, keeping enough to tell one list from another. spent is what the line
// costs after the label.
//
// The room is what is actually free, not the floor: a name elided to the floor
// whenever it is a column too long would leave the rest of the line blank to
// buy nothing.
func (m *Model) fitLabel(label string, spent int) string {
	if m.width <= 0 {
		return label
	}
	room := max(m.width-spent, minScopeLabel)
	if ansi.StringWidth(label) <= room {
		return label
	}
	return ansi.Truncate(label, room, "…")
}

// emptyLine says why there is nothing on screen. A list emptied by a filter is
// a different thing from an empty list, and the difference is not otherwise
// visible.
func (m *Model) emptyLine() string {
	if !m.filters.none() && len(m.entries) > 0 {
		return "Nothing matches " + m.filters.describe() + "."
	}
	return "Nothing here yet."
}

// scopeLabel names the list on screen: the project or global name for a single
// scope, and "all scopes" for the merged view.
func (m *Model) scopeLabel() string { return m.view.label() }

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
