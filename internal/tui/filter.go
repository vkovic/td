package tui

import (
	"sort"
	"strings"

	"github.com/vkovic/td/internal/store"
)

// filters are the view-only narrowing applied on top of the loaded listing.
//
// They never reach the store. A reload re-reads what the scope holds and then
// re-applies these, which is why a change landing while a filter is open does
// not clear it — the filter was never part of what was loaded.
type filters struct {
	// title narrows to items whose title contains it, case-insensitively.
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
		if f.title != "" && !strings.Contains(strings.ToLower(e.Item.Title), strings.ToLower(f.title)) {
			continue
		}
		if f.tag != "" && !store.HasEveryTag(e.Item, []string{f.tag}) {
			continue
		}
		kept = append(kept, e)
	}
	return kept
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

// cycleScope moves to the next list: the directory's own scope, then the global
// list, then every scope merged.
//
// A store opened on the global list has no separate project stop, so it cycles
// between global and all rather than showing the same list twice.
func (m *Model) cycleScope() error {
	switch {
	case m.mode == ModeScope:
		m.mode = ModeGlobal
	case m.mode == ModeGlobal:
		m.mode = ModeAll
	case m.scope.Scope.IsGlobal():
		m.mode = ModeGlobal
	default:
		m.mode = ModeScope
	}
	return m.reload()
}
