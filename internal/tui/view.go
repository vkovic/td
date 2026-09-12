package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

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
