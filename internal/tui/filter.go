package tui

import (
	"sort"
	"strings"
	"unicode"

	"github.com/vkovic/td/internal/store"
)

// filters are the view-only narrowing applied on top of the loaded listing.
//
// They never reach the store. A reload re-reads what the scope holds and then
// re-applies these, which is why a change landing while a filter is open does
// not clear it — the filter was never part of what was loaded.
type filters struct {
	// title narrows to items whose title holds its runes in order, ignoring
	// case, the way fzf matches: fdb keeps "fix docs build".
	title string
	// tag narrows to items carrying it, through the same comparison td ls -t
	// uses.
	tag string
}

// none reports whether anything is being narrowed.
func (f filters) none() bool { return f.title == "" && f.tag == "" }

// apply narrows a listing.
//
// Unlike td ls -t, this does not hide done items. The CLI's filter composes
// with an explicit --done; the TUI has no such flag and always shows both
// sections, so a tag only completed items carry shows those items under the
// done rule rather than emptying the screen with no visible reason.
func (f filters) apply(entries []store.Entry) []store.Entry {
	if f.none() {
		return entries
	}
	kept := make([]store.Entry, 0, len(entries))
	for _, e := range entries {
		if _, ok := matchTitle(e.Item.Title, f.title); !ok {
			continue
		}
		if f.tag != "" && !store.HasEveryTag(e.Item, []string{f.tag}) {
			continue
		}
		kept = append(kept, e)
	}
	return kept
}

// matchTitle reports whether query's runes appear in title in order, ignoring
// case, and which of title's runes they landed on. An empty query matches
// every title and lands on none.
//
// Each query rune takes the first title rune after the one the previous rune
// took. That is not always the placement fzf would underline, since fzf scores
// gaps, but rows here are never ranked, so the positions only have to show why
// a row survived, and the leftmost ones always do.
//
// Positions count runes, not bytes, and case is folded one rune at a time, so
// a rune whose lower case encodes to a different length cannot shift them.
func matchTitle(title, query string) ([]int, bool) {
	want := []rune(query)
	if len(want) == 0 {
		return nil, true
	}
	positions := make([]int, 0, len(want))
	for pos, r := range []rune(title) {
		if unicode.ToLower(r) != unicode.ToLower(want[len(positions)]) {
			continue
		}
		positions = append(positions, pos)
		if len(positions) == len(want) {
			return positions, true
		}
	}
	return nil, false
}

// describe names the active filters for the footer, so a list that is missing
// items says why.
func (f filters) describe() string {
	var parts []string
	if f.title != "" {
		parts = append(parts, "/"+f.title)
	}
	if f.tag != "" {
		parts = append(parts, "#"+f.tag)
	}
	return strings.Join(parts, " ")
}

// tagsIn collects every tag the loaded listing carries, in a stable order, so
// cycling with t visits each one once and in the same order every time.
//
// Tags differing only in case are one tag, matching how they are compared, and
// the first spelling seen wins so the footer shows what is written in a file.
func tagsIn(entries []store.Entry) []string {
	seen := make(map[string]string)
	for _, e := range entries {
		for _, tag := range e.Item.Tags {
			key := strings.ToLower(tag)
			if _, ok := seen[key]; !ok {
				seen[key] = tag
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, len(keys))
	for i, key := range keys {
		out[i] = seen[key]
	}
	return out
}

// cycleTag advances the tag filter one step through the tags present, with no
// filter as the step after the last. A tag that has left the listing drops the
// filter rather than stranding it on something invisible.
func (m *Model) cycleTag() {
	tags := tagsIn(m.entries)
	if len(tags) == 0 {
		m.filters.tag = ""
		return
	}
	next := 0
	for i, tag := range tags {
		if strings.EqualFold(tag, m.filters.tag) {
			next = i + 1
			break
		}
	}
	if next >= len(tags) {
		m.filters.tag = ""
	} else {
		m.filters.tag = tags[next]
	}
	m.applyFilters()
}
