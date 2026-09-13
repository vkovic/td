package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestPinAndUnpin: td pin moves an item to the head of the list without
// touching updated, leaves one commit, and is idempotent; td unpin puts the
// item back where its timestamps say.
func TestPinAndUnpin(t *testing.T) {
	h := newHarness(t)
	older := h.mutation("add", "Added first").Items[0]
	newer := h.mutation("add", "Added second").Items[0].ID

	if got := ids(h.lsJSON().Items); got[0] != newer {
		t.Fatalf("order before pinning = %v, want the newer item first", got)
	}

	commits := h.commits()
	res := h.mutation("pin", older.ID)
	if res.Action != "pin" {
		t.Errorf("action = %q, want pin", res.Action)
	}
	if !res.Items[0].Pinned {
		t.Error("td pin reported the item unpinned")
	}
	if !res.Items[0].Updated.Equal(older.Updated) {
		t.Errorf("updated = %s, want it left at %s", res.Items[0].Updated, older.Updated)
	}
	if got := h.commits(); got != commits+1 {
		t.Errorf("commit count = %d, want one more than %d", got, commits)
	}

	listed := h.lsJSON().Items
	if listed[0].ID != older.ID || !listed[0].Pinned || listed[1].Pinned {
		t.Errorf("td ls = %+v, want the pinned item first and the only one pinned", listed)
	}

	// Pinning it again is not an error and not a commit.
	commits = h.commits()
	if res := h.mutation("pin", older.ID); !res.Items[0].Pinned || res.Epilogue.Committed {
		t.Errorf("td pin on a pinned item = %+v, want it reported pinned with no commit", res)
	}
	if got := h.commits(); got != commits {
		t.Errorf("commit count = %d after a repeated pin, want %d", got, commits)
	}

	if res := h.mutation("unpin", older.ID); res.Action != "unpin" || res.Items[0].Pinned {
		t.Errorf("td unpin = %+v, want action unpin and the item unpinned", res)
	}
	if got := ids(h.lsJSON().Items); got[0] != newer {
		t.Errorf("order after unpinning = %v, want the newer item first again", got)
	}
}

// TestAddPinCreatesAPinnedItemInOneCommit: --pin is part of the add, not a pin
// run after it.
func TestAddPinCreatesAPinnedItemInOneCommit(t *testing.T) {
	h := newHarness(t)
	res := h.mutation("add", "Keep in view", "--pin")
	if !res.Items[0].Pinned {
		t.Error("td add --pin reported the item unpinned")
	}
	if got := h.commits(); got != 1 {
		t.Errorf("commit count = %d, want exactly 1", got)
	}
}

// TestLsTableAlignsPinnedTitles: the glyph is one rune two columns wide, and
// tabwriter pads by rune, so a table that got this wrong would stagger every
// pinned row's title by a column.
func TestLsTableAlignsPinnedTitles(t *testing.T) {
	h := newHarness(t)
	h.mutation("add", "Plain item", "-t", "cli")
	h.mutation("add", "Pinned item", "--pin")

	out := h.mustRun("ls")
	column := map[string]int{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		for _, title := range []string{"Plain item", "Pinned item"} {
			if at := strings.Index(line, title); at >= 0 {
				column[title] = ansi.StringWidth(line[:at])
				if title == "Pinned item" && !strings.Contains(line, pinGlyph) {
					t.Errorf("the pinned row carries no %s:\n%s", pinGlyph, out)
				}
			}
		}
	}
	if len(column) != 2 {
		t.Fatalf("td ls did not list both titles:\n%s", out)
	}
	if column["Plain item"] != column["Pinned item"] {
		t.Errorf("titles start at columns %v, want one column:\n%s", column, out)
	}
	if strings.Index(out, "Pinned item") > strings.Index(out, "Plain item") {
		t.Errorf("the pinned item is not listed first:\n%s", out)
	}
}
