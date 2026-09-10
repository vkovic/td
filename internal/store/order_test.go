package store

import (
	"strings"
	"testing"
	"time"
)

// ts builds a timestamp a fixed number of hours past a fixed base, so a test
// can say "older" and "newer" without spelling out a date each time.
func ts(hours int) time.Time {
	return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(hours) * time.Hour)
}

// entry builds one entry for the ordering tests. A nil doneAt leaves the item
// open.
func entry(id string, updated, created time.Time, doneAt *time.Time) Entry {
	return Entry{Item: &Item{ID: id, Title: id, Updated: updated, Created: created, DoneAt: doneAt}}
}

// ptr returns a pointer to t, for the done_at field.
func ptr(t time.Time) *time.Time { return &t }

// ids names the order a slice came out in, so a failure reads as a sequence.
func ids(entries []Entry) string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Item.ID
	}
	return strings.Join(out, ",")
}

// TestSortEntries drives every comparison tier SortEntries applies, one case
// per tier, each case differing only in the field that tier reads.
func TestSortEntries(t *testing.T) {
	base := ts(0)
	tests := []struct {
		name    string
		entries []Entry
		want    string
	}{
		{
			name: "open before done",
			entries: []Entry{
				entry("done1", base, base, ptr(ts(9))),
				entry("open1", base, base, nil),
			},
			want: "open1,done1",
		},
		{
			name: "done items by done_at descending",
			entries: []Entry{
				entry("older", base, base, ptr(ts(1))),
				entry("newer", base, base, ptr(ts(5))),
			},
			want: "newer,older",
		},
		{
			name: "open items by updated descending",
			entries: []Entry{
				entry("stale", ts(1), base, nil),
				entry("fresh", ts(5), base, nil),
			},
			want: "fresh,stale",
		},
		{
			name: "created breaks an updated tie",
			entries: []Entry{
				entry("old", ts(5), ts(1), nil),
				entry("new", ts(5), ts(3), nil),
			},
			want: "new,old",
		},
		{
			name: "id breaks every remaining tie",
			entries: []Entry{
				entry("aaa", ts(5), ts(5), nil),
				entry("zzz", ts(5), ts(5), nil),
			},
			want: "zzz,aaa",
		},
		{
			name:    "an empty slice sorts to nothing",
			entries: []Entry{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SortEntries(tt.entries)
			if got := ids(tt.entries); got != tt.want {
				t.Errorf("SortEntries ordered %s, want %s", got, tt.want)
			}
		})
	}
}

// TestSortEntriesFullListing puts every tier in one slice, because the tiers
// only matter in combination: a done item must not outrank an open one however
// recently it was updated.
func TestSortEntriesFullListing(t *testing.T) {
	entries := []Entry{
		entry("done-old", ts(9), ts(9), ptr(ts(2))),
		entry("open-stale", ts(1), ts(1), nil),
		entry("done-new", ts(1), ts(1), ptr(ts(8))),
		entry("open-fresh", ts(7), ts(7), nil),
	}
	SortEntries(entries)
	if got, want := ids(entries), "open-fresh,open-stale,done-new,done-old"; got != want {
		t.Errorf("SortEntries ordered %s, want %s", got, want)
	}
}

// TestHasEveryTag: several tags narrow the list, and the match ignores case.
func TestHasEveryTag(t *testing.T) {
	tests := []struct {
		name string
		have []string
		want []string
		ok   bool
	}{
		{name: "no tags wanted matches anything", have: nil, want: nil, ok: true},
		{name: "one wanted tag present", have: []string{"work", "urgent"}, want: []string{"work"}, ok: true},
		{name: "every wanted tag present", have: []string{"work", "urgent"}, want: []string{"work", "urgent"}, ok: true},
		{name: "one wanted tag missing", have: []string{"work"}, want: []string{"work", "urgent"}, ok: false},
		{name: "the match ignores case", have: []string{"Work"}, want: []string{"wORK"}, ok: true},
		{name: "an untagged item fails any wanted tag", have: nil, want: []string{"work"}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasEveryTag(&Item{Tags: tt.have}, tt.want); got != tt.ok {
				t.Errorf("HasEveryTag(%v, %v) = %v, want %v", tt.have, tt.want, got, tt.ok)
			}
		})
	}
}
