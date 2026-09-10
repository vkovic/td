package store

import (
	"sort"
	"strings"
)

// SortEntries orders a listing: open items first, most recently updated first,
// with created breaking a tie; then done items, most recently completed first.
// Ids break any remaining tie, so the order is stable run to run.
//
// Timestamps are stored to the second, so two items touched in the same second
// fall through to the id, which is why the tiebreaks matter at all.
//
// The CLI's td ls and the TUI both order through here, so the two cannot drift.
func SortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].Item, entries[j].Item
		if a.Done() != b.Done() {
			return !a.Done() // open items come first
		}
		if a.Done() {
			if !a.DoneAt.Equal(*b.DoneAt) {
				return a.DoneAt.After(*b.DoneAt)
			}
		}
		if !a.Updated.Equal(b.Updated) {
			return a.Updated.After(b.Updated)
		}
		if !a.Created.Equal(b.Created) {
			return a.Created.After(b.Created)
		}
		// Ids encode creation time, so the newer id first keeps this consistent
		// with the created tiebreak above when both land in the same second.
		return a.ID > b.ID
	})
}

// HasEveryTag reports whether it carries all of want. Several tags narrow the
// list rather than widening it, so td ls -t and the TUI's tag filter agree.
func HasEveryTag(it *Item, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range it.Tags {
			if strings.EqualFold(h, w) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
