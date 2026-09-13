package task

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/vkovic/td/internal/store"
	"github.com/vkovic/td/internal/tdtest"
)

// TestSetPinnedLeavesUpdatedAlone: a pin says where an item sits, not that it
// was worked on, so updated and the file's mtime both stay where they were and
// the next bump finds nothing to record.
func TestSetPinnedLeavesUpdatedAlone(t *testing.T) {
	created := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	now := created
	svc, st := clockedService(t, &now)
	e := add(t, svc, "Buy milk")

	now = created.Add(3 * time.Hour)
	res, err := svc.SetPinned(PinRequest{Scope: store.Global, IDs: []string{e.Item.ID}, Pinned: true})
	if err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	if res.Action != "pin" {
		t.Errorf("Action = %q, want \"pin\"", res.Action)
	}
	if want := "td: pin " + e.Item.ID + " Buy milk"; res.Message != want {
		t.Errorf("Message = %q, want %q", res.Message, want)
	}

	back := only(t, st)
	if !back.Item.Pinned {
		t.Error("the item is not pinned on disk")
	}
	if !back.Item.Updated.Equal(created) {
		t.Errorf("updated = %s, want it left at %s", back.Item.Updated, created)
	}
	if edited, err := store.HandEdited(back); err != nil || edited {
		t.Errorf("HandEdited = %v, %v: the pin's own write reads as a hand edit", edited, err)
	}

	res, err = svc.SetPinned(PinRequest{Scope: store.Global, IDs: []string{e.Item.ID}, Pinned: false})
	if err != nil {
		t.Fatalf("SetPinned unpinning: %v", err)
	}
	if res.Action != "unpin" {
		t.Errorf("Action = %q, want \"unpin\"", res.Action)
	}
	if only(t, st).Item.Pinned {
		t.Error("the item is still pinned after unpinning")
	}
}

// TestPinningAPinnedItemWritesNothing: td pin is idempotent, and the way it
// stays so without an empty commit is by leaving the file alone — its bytes and
// its mtime both.
func TestPinningAPinnedItemWritesNothing(t *testing.T) {
	now := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	svc, st := clockedService(t, &now)
	e := add(t, svc, "Buy milk")
	pin := PinRequest{Scope: store.Global, IDs: []string{e.Item.ID}, Pinned: true}
	if _, err := svc.SetPinned(pin); err != nil {
		t.Fatal(err)
	}

	path := only(t, st).Ref.Path
	before, info := readWithStat(t, path)

	res, err := svc.SetPinned(pin)
	if err != nil {
		t.Fatalf("SetPinned on a pinned item: %v", err)
	}
	if len(res.Entries) != 1 || !res.Entries[0].Item.Pinned {
		t.Errorf("the result does not report the pinned item: %+v", res.Entries)
	}
	after, infoAfter := readWithStat(t, path)
	if !bytes.Equal(before, after) || !info.ModTime().Equal(infoAfter.ModTime()) {
		t.Error("pinning an already pinned item rewrote its file")
	}
}

// TestPinRecordsAnUnbumpedHandEdit: Save pulls a file's mtime back to its
// updated, and a hand edit nobody has bumped yet is known only by its mtime
// being later. A pin that did not record the edit would erase it.
func TestPinRecordsAnUnbumpedHandEdit(t *testing.T) {
	created := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	now := created
	svc, st := clockedService(t, &now)
	e := add(t, svc, "Buy milk")

	path := only(t, st).Ref.Path
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, []byte("\nAdded by hand.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Time{}, created.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	now = created.Add(2 * time.Hour)
	if _, err := svc.SetPinned(PinRequest{Scope: store.Global, IDs: []string{e.Item.ID}, Pinned: true}); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	back := only(t, st)
	if !back.Item.Updated.Equal(now) {
		t.Errorf("updated = %s, want the hand edit recorded at %s", back.Item.Updated, now)
	}
	if back.Item.Body != "Added by hand.\n" {
		t.Errorf("body = %q, want the hand edit kept", back.Item.Body)
	}
}

// clockedService builds a Service whose clock reads *now, so a test can move
// time between two operations on the same store.
func clockedService(t *testing.T, now *time.Time) (*Service, *store.Store) {
	t.Helper()
	tdtest.IsolateGit(t)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	clock := func() time.Time { return *now }
	return New(st, clock, WithIDGen(store.NewIDGen(clock))), st
}

// only reads back the single live item in the global list.
func only(t *testing.T, st *store.Store) store.Entry {
	t.Helper()
	entries, err := st.List(store.Global, store.Active)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the list holds %d items, want 1", len(entries))
	}
	return entries[0]
}

// readWithStat returns a file's bytes and its stat.
func readWithStat(t *testing.T, path string) ([]byte, os.FileInfo) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return b, info
}
