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

// View renders the whole screen.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.showHelp {
		return m.helpView()
	}

	var b strings.Builder
	for _, line := range m.notice("warning: ", m.warn) {
		fmt.Fprintln(&b, m.styles.warning.Render(line))
	}

	if len(m.shown) == 0 {
		fmt.Fprintln(&b, m.styles.emptyMsg.Render(m.fit(m.emptyLine())))
	} else {
		b.WriteString(m.rows())
	}

	if m.prompt.open() {
		// The prompt keeps its tail rather than its head: what you are typing
		// is at the end of it, and a prompt that stops showing your keystrokes
		// is worse than one that has scrolled its start away.
		label := m.styles.prompt.Render(m.prompt.label + "> ")
		value := m.fitTail(m.prompt.value+"█", ansi.StringWidth(label))
		fmt.Fprintln(&b, label+value)
	}
	for _, line := range m.notice("error: ", m.err) {
		fmt.Fprintln(&b, m.styles.warning.Render(line))
	}
	// Rendered a line at a time: Lip Gloss pads every line of a multi-line
	// block out to the widest one, which would trail the status line with
	// however many spaces the legend is longer by.
	for _, line := range strings.Split(m.footer(), "\n") {
		fmt.Fprintln(&b, m.styles.footer.Render(line))
	}
	return b.String()
}

// rows renders one line per item, with the rule dropped in where the listing
// crosses from the open items to the done ones. SortEntries has already put
// every open item ahead of every done one, so the crossing happens exactly once.
func (m *Model) rows() string {
	var b strings.Builder
	ruled := false
	for i, e := range m.shown {
		if !ruled && e.Item.Done() && i > 0 {
			fmt.Fprintln(&b, m.styles.rule.Render(doneRule))
			ruled = true
		}
		fmt.Fprintln(&b, m.row(e, i == m.cursor))
	}
	return b.String()
}

// minTitle is the narrowest a title is ever squeezed to. A pane too narrow to
// hold the other cells and this much title overruns by the difference: a row
// cut to a column of ellipses says nothing at all, and a stub of a title is
// what makes one row tell itself apart from the next.
const minTitle = 8

// row renders one item: the cursor, a done marker, the title, its tags, its due
// date and how long ago it was updated. Empty fields take no space at all, so a
// list with no tags carries no gap where the tags would be.
//
// The title is the one cell that gives way when the row is wider than the pane.
// Everything else is short, fixed, and the answer to a question — when is this
// due, how long has it sat there — so a row wraps into two lines only when the
// pane is too narrow to hold even a stub of a title.
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
	if m.mode == ModeAll {
		before = append(before, m.styles.scope.Render(e.Ref.Scope.String()))
	}
	var after []string
	if len(e.Item.Tags) > 0 {
		after = append(after, m.styles.tags.Render("#"+strings.Join(e.Item.Tags, " #")))
	}
	if e.Item.Due != nil {
		text := "due " + e.Item.Due.String()
		if m.overdue(e.Item) {
			after = append(after, m.styles.overdue.Render(text))
		} else {
			after = append(after, m.styles.due.Render(text))
		}
	}
	after = append(after, m.styles.updated.Render(relative(e.Item.Updated, m.now())))

	style := m.styles.title
	if e.Item.Done() {
		style = m.styles.done
	}
	title := m.fitTitle(style.Render(e.Item.Title), before, after)

	return strings.Join(append(append(before, title), after...), " ")
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
func (m *Model) fitTitle(title string, before, after []string) string {
	if m.width <= 0 {
		return title
	}
	spent := len(before) + len(after) // one space between every pair of cells
	for _, cell := range append(append([]string{}, before...), after...) {
		spent += ansi.StringWidth(cell)
	}
	room := max(m.width-spent, minTitle)
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

// footer is what sits under the list: a status line naming the scope, the
// counts, any active filter, what the last epilogue did and the external-change
// note, and under it the key legend, drawn from the same table as the overlay.
//
// Both lines are fitted to the pane. Neither is bounded by anything but the
// user: the filter echo is whatever was typed into /, and a project name has
// no length limit at all, so a status line wider than the pane is ordinary use
// rather than an edge case.
func (m *Model) footer() string {
	open := 0
	for _, e := range m.shown {
		if !e.Item.Done() {
			open++
		}
	}
	line := fmt.Sprintf("%s · %d open, %d total", m.scopeLabel(), open, len(m.shown))
	if f := m.filters.describe(); f != "" {
		// Trimmed before the line is assembled: the filter sits in the middle,
		// so trimming the whole line from the right would eat the status and
		// the flash to keep filter text nobody can read anyway.
		line += " · filtered " + m.fitFilter(f, ansi.StringWidth(line))
	}
	if m.status != "" {
		line += " · " + m.status
	}
	if m.flashing() {
		line += " · " + externalFlash
	}
	if m.width > 0 {
		line = ansi.Truncate(line, m.width, "…")
	}
	return line + "\n" + legend(m.width)
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
	room := max(m.width-spent-len(" · filtered "), minFilterEcho)
	if ansi.StringWidth(f) <= room {
		return f
	}
	return ansi.Truncate(f, room, "…")
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
func (m *Model) scopeLabel() string {
	if m.mode == ModeAll {
		return "all scopes"
	}
	return m.currentScope().String()
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
