package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// View renders the whole screen, top to bottom: the header, the warnings, the
// open rows, the blank gap, the done section, the prompt and error lines, and
// the footer.
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

	var warn, tail []string
	for _, line := range m.notice("warning: ", m.warn) {
		warn = append(warn, m.styles.warning.Render(line))
	}
	// The filter prompt is drawn on the header's top line; only the add prompt
	// still opens down here.
	if m.prompt.open() && m.prompt.kind != promptFilter {
		label := m.styles.prompt.Render(m.prompt.label + "> ")
		tail = append(tail, label+m.fitTail(m.prompt.value+"█", ansi.StringWidth(label)))
	}
	for _, line := range m.notice("error: ", m.err) {
		tail = append(tail, m.styles.warning.Render(line))
	}

	// The header and the footer are two lines each, and the footer is built last
	// because it reports how much of the list did not fit. In a pane too short
	// for all four and a line of list, the chrome gives way by rank (see
	// keptChrome). A pane showing nothing but chrome says nothing about your
	// todos, and the legend already sheds keys by rank, so shedding whole lines
	// at the last extremity is the same rule carried one step further. The
	// warnings are never shed: they say the list itself is wrong.
	chrome := 4
	if m.height > 0 {
		chrome = min(max(m.height-len(warn)-len(tail)-1, 0), 4)
	}
	kept := m.keptChrome(chrome)
	body, off := m.body(m.capacity(len(warn) + len(tail) + chrome))

	var b strings.Builder
	header := m.header()
	if kept[lineTop] {
		fmt.Fprintln(&b, header[0])
	}
	if kept[lineRule] {
		fmt.Fprintln(&b, header[1])
	}
	for _, line := range warn {
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
	footer := strings.Split(m.footer(off), "\n")
	if kept[lineStatus] {
		fmt.Fprintln(&b, m.styles.footer.Render(footer[0]))
	}
	if kept[lineLegend] {
		fmt.Fprintln(&b, m.styles.footer.Render(footer[1]))
	}
	// No trailing newline. A view that ends with one occupies a line more than
	// it drew, and a view exactly as tall as the pane then scrolls its own top
	// row off — which is the cursor, since the list opens at the newest item.
	return strings.TrimSuffix(b.String(), "\n")
}

// chromeLine is one of the four lines around the list that a short pane sheds
// whole.
type chromeLine int

const (
	// lineTop heads the pane: blank, or the title filter.
	lineTop chromeLine = iota
	// lineRule is the rule under lineTop, carrying the list's name.
	lineRule
	// lineStatus is the footer's counts and filter echo.
	lineStatus
	// lineLegend is the footer's key summary.
	lineLegend
)

// keptChrome picks which n of the four chrome lines a pane has room for.
//
// Normally the header's rule goes first, then the top line, then the
// legend, then the status line: the footer outlasts the header because it is
// what counts the rows the window hides.
//
// A title filter on the top line changes the order, and it outlasts all three.
// While the input is open it is where the keystrokes land, and a prompt you
// cannot see is one you are typing into blind; once kept, it is why rows are
// missing, and the status line's echo of it is the first thing that line
// elides.
func (m *Model) keptChrome(n int) map[chromeLine]bool {
	order := []chromeLine{lineStatus, lineLegend, lineTop, lineRule}
	if m.titleFiltering() {
		order = []chromeLine{lineTop, lineStatus, lineLegend, lineRule}
	}
	kept := make(map[chromeLine]bool, n)
	for _, line := range order[:n] {
		kept[line] = true
	}
	return kept
}

// titleFiltering reports whether the top line belongs to the title filter:
// its input is open, or a query it left behind is still narrowing the list.
func (m *Model) titleFiltering() bool {
	return m.prompt.kind == promptFilter || m.filters.title != ""
}

// header is the two lines over the list: a top line, and a rule under it that
// names the list on screen the way the done section's rule names that section.
// The name is stated nowhere else on the pane, so it heads the screen rather
// than sharing the status line, where it was the first thing elided whenever
// the counts needed the room.
//
// The top line holds a title filter, open or kept. It sits where the eye
// already is when reading the list, rather than on a line above the footer.
// With no filter the line is blank rather than gone, so the list does not jump
// a row each time a filter opens or clears.
func (m *Model) header() []string {
	top := ""
	if m.titleFiltering() {
		// The query keeps its tail rather than its head: what you are typing
		// is at the end of it, and an input that stops showing your keystrokes
		// is worse than one that has scrolled its start away.
		// The slash is the one the footer echoes the filter with, and the key
		// that opened it.
		label := m.styles.prompt.Render("/")
		query := m.filters.title
		if m.prompt.kind == promptFilter {
			query = m.prompt.value + "█"
		}
		top = label + m.fitTail(query, ansi.StringWidth(label))
	}
	return []string{
		top,
		m.styles.rule.Render(labelRule(m.scopeLabel(), m.width)),
	}
}

// labelRule is a rule carrying a label, "── label ──", run out to width. Both
// the header's rule and the done section's are drawn by it, so the two read as
// one kind of line.
//
// A zero width is a pane that has not announced itself, and the rule stops at
// its natural width. A label too wide for the pane is cut from the right with
// an ellipsis, and the leading dashes stay so the line still reads as a rule.
func labelRule(label string, width int) string {
	head := "── " + label + " "
	if width <= 0 {
		return head + "──"
	}
	if w := ansi.StringWidth(head); w <= width {
		return head + strings.Repeat("─", width-w)
	}
	return ansi.Truncate("── "+label, width, "…")
}

// scopeLabel names the list on screen: the project or global name for a single
// scope, and "all scopes" for the merged view.
func (m *Model) scopeLabel() string { return m.view.label() }

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
		lines = append(lines, m.styles.rule.Render(labelRule(doneLabel, m.width)))
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

// emptyLine says why there is nothing on screen. A list emptied by a filter is
// a different thing from an empty list, and the difference is not otherwise
// visible.
func (m *Model) emptyLine() string {
	if !m.filters.none() && len(m.entries) > 0 {
		return "Nothing matches " + m.filters.describe() + "."
	}
	return "Nothing here yet."
}
