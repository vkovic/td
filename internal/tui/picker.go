package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vkovic/td/internal/store"
)

// picker is the scope overlay: every list the pane can move to, where its
// cursor sits, and the first row on screen.
//
// Its cursor is its own. The list underneath keeps the cursor it had, so a
// cancelled pick leaves the pane exactly as it was found.
type picker struct {
	open   bool
	rows   []scopeView
	cursor int
	top    int
}

// pickerHint is the line pinned to the bottom of the overlay, saying how to
// work it. It is the picker's counterpart to the help overlay's closing line.
const pickerHint = "j/k moves · enter picks · esc cancels"

// openPicker raises the overlay over the list, with the cursor on the list
// that is showing.
func (m *Model) openPicker() tea.Cmd {
	rows, err := m.pickerRows()
	if err != nil {
		m.err = err
		return nil
	}
	m.picker = picker{open: true, rows: rows}
	for i, v := range rows {
		if v == m.view {
			m.picker.cursor = i
		}
	}
	return nil
}

// pickerRows is what the overlay lists: the global list, the merged view, then
// every project the store holds, in name order.
//
// The scope on screen is listed even when the store has no directory for it,
// which is a project whose marker resolved before anything was filed in it.
// Leaving it out would open the picker with its cursor on somebody else's
// list, and make enter — the key that means "the one I am looking at" — move
// the pane somewhere the reader never chose.
func (m *Model) pickerRows() ([]scopeView, error) {
	scopes, err := m.store.Scopes()
	if err != nil {
		return nil, err
	}
	projects := make([]string, 0, len(scopes))
	seen := make(map[store.Scope]bool)
	for _, scope := range scopes {
		if scope.IsGlobal() || seen[scope] {
			continue
		}
		seen[scope] = true
		projects = append(projects, string(scope))
	}
	if current := m.view.scope; !m.view.merged && !current.IsGlobal() && !seen[current] {
		projects = append(projects, string(current))
		sort.Strings(projects)
	}

	rows := []scopeView{{scope: store.Global}, {merged: true}}
	for _, name := range projects {
		rows = append(rows, scopeView{scope: store.Scope(name)})
	}
	return rows, nil
}

// handlePickerKey drives the overlay. Every other key is swallowed, as the
// help overlay swallows them: a keystroke aimed at a covered list must not
// reach it.
func (m *Model) handlePickerKey(pressed string) tea.Cmd {
	switch pressed {
	case "esc":
		m.picker = picker{}
	case "j", "down":
		m.movePicker(1)
	case "k", "up":
		m.movePicker(-1)
	case "enter":
		return m.pickScope()
	}
	return nil
}

// movePicker steps the overlay's cursor, clamping at both ends rather than
// wrapping, exactly as the list's own cursor does.
func (m *Model) movePicker(delta int) {
	m.picker.cursor = min(max(m.picker.cursor+delta, 0), len(m.picker.rows)-1)
}

// pickScope shows the list under the overlay's cursor and closes the overlay.
//
// The list's own cursor is left where it was, and the filters with it. Picking
// a list is the same kind of move the old three-way cycle was: what is loaded
// changes, and the view state on top of it does not.
func (m *Model) pickScope() tea.Cmd {
	if m.picker.cursor < 0 || m.picker.cursor >= len(m.picker.rows) {
		m.picker = picker{}
		return nil
	}
	picked := m.picker.rows[m.picker.cursor]
	m.picker = picker{}
	if picked == m.view {
		return nil
	}
	m.view = picked
	if err := m.reload(); err != nil {
		m.err = err
	}
	return nil
}

// pickerView renders the overlay: a heading, one line per list, and the hint
// pinned to the bottom.
//
// The rows are windowed the way the help overlay's are, with one difference:
// this overlay has a cursor, so the window follows it rather than holding
// still. A store with forty projects in a pane fourteen lines tall must not be
// a picker whose cursor walks off the bottom.
//
// Only an unknown height renders the whole thing unwindowed. Bubble Tea sends
// the size after the model is built, and a pane one row tall is a real pane —
// treating the two the same rendered thirty-five lines into three, where the
// terminal keeps the tail, the heading and the cursor row both go, and j/k
// look dead because nothing on screen moves.
func (m *Model) pickerView() string {
	head := []string{m.styles.title.Render(m.fit("td — lists")), ""}
	pinned := m.styles.footer.Render(m.fit(pickerHint))

	rows := make([]string, len(m.picker.rows))
	for i, v := range m.picker.rows {
		rows[i] = m.pickerRow(v, i == m.picker.cursor)
	}

	if m.height <= 0 || len(head)+len(rows)+1 <= m.height {
		m.picker.top = 0
		return strings.Join(assemble(head, rows, pinned), "\n")
	}

	// What each part is worth when they cannot all fit. The hint is pinned,
	// because a picker you cannot work is worse than one you cannot read the
	// title of. The rows keep a line as long as there is one, because the
	// cursor is the only thing on this screen that moves. So the heading is
	// what gives way, and it gives way from its blank line first.
	head = head[:min(len(head), max(m.height-2, 0))]
	room := max(m.height-len(head)-1, 0)
	if room == 0 {
		m.picker.top = 0
		return pinned
	}

	m.picker.top = min(max(m.picker.top, 0), len(rows)-room)
	if m.picker.cursor < m.picker.top {
		m.picker.top = m.picker.cursor
	}
	if m.picker.cursor >= m.picker.top+room {
		m.picker.top = m.picker.cursor - room + 1
	}
	return strings.Join(assemble(head, rows[m.picker.top:m.picker.top+room], pinned), "\n")
}

// assemble joins the overlay's three parts into one set of lines, copied into
// a slice of its own rather than appended onto the heading — the heading is
// re-sliced above, and appending past its end would write over the line it had
// just given up.
func assemble(head, rows []string, pinned string) []string {
	lines := make([]string, 0, len(head)+len(rows)+1)
	lines = append(lines, head...)
	lines = append(lines, rows...)
	return append(lines, pinned)
}

// pickerRow renders one list's line, marked the way a selected item is marked
// in the list itself.
func (m *Model) pickerRow(v scopeView, selected bool) string {
	marker := "  "
	if selected {
		marker = m.styles.cursor.Render("❯ ")
	}
	return m.fit(marker + v.label())
}
