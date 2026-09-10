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
	if m.warn != nil {
		fmt.Fprintln(&b, m.styles.warning.Render("warning: "+m.warn.Error()))
	}

	if len(m.shown) == 0 {
		fmt.Fprintln(&b, m.styles.emptyMsg.Render(m.emptyLine()))
	} else {
		b.WriteString(m.rows())
	}

	if m.prompt.open() {
		fmt.Fprintln(&b, m.styles.prompt.Render(m.prompt.label+"> ")+m.prompt.value+"█")
	}
	if m.err != nil {
		fmt.Fprintln(&b, m.styles.warning.Render("error: "+m.err.Error()))
	}
	fmt.Fprintln(&b, m.styles.footer.Render(m.footer()))
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

// footer is what sits under the list: a status line naming the scope, the
// counts, any active filter, what the last epilogue did and the external-change
// note, and under it the key legend, drawn from the same table as the overlay.
func (m *Model) footer() string {
	open := 0
	for _, e := range m.shown {
		if !e.Item.Done() {
			open++
		}
	}
	line := fmt.Sprintf("%s · %d open, %d total", m.scopeLabel(), open, len(m.shown))
	if f := m.filters.describe(); f != "" {
		line += " · filtered " + f
	}
	if m.status != "" {
		line += " · " + m.status
	}
	if m.flashing() {
		line += " · " + externalFlash
	}
	return line + "\n" + legend()
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
