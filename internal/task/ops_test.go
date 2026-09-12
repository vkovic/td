package task

import (
	"testing"
	"time"

	"github.com/vkovic/td/internal/store"
)

// add puts one item in the store and returns it, for a test that needs
// something to act on rather than something to assert about.
func add(t *testing.T, svc *Service, title string) store.Entry {
	t.Helper()
	res, err := svc.Add(AddRequest{Scope: store.Global, Title: title})
	if err != nil {
		t.Fatalf("Add %q: %v", title, err)
	}
	return res.Entries[0]
}

// TestRemoveReportsRemoveAndNotRm: the command is td rm and the action it
// reports is "remove". The /td plugin documents both, and cmd/td decides
// whether item bodies appear in --json by testing for this exact word, so the
// mismatch is contract rather than oversight.
func TestRemoveReportsRemoveAndNotRm(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	e := add(t, svc, "Buy milk")

	res, err := svc.Remove(MoveRequest{Scope: store.Global, IDs: []string{e.Item.ID}})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if res.Action != "remove" {
		t.Errorf("Action = %q, want \"remove\"", res.Action)
	}
	if want := "td: remove " + e.Item.ID + " Buy milk"; res.Message != want {
		t.Errorf("Message = %q, want %q", res.Message, want)
	}
	if got := res.Entries[0].Ref.Area; got != store.Deleted {
		t.Errorf("the item is filed under %q, want the trash", got)
	}
}

// TestSetDoneNamesTheDirectionItWent: one call does both, so the action it
// reports is the only thing telling the caller which way it went.
func TestSetDoneNamesTheDirectionItWent(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	e := add(t, svc, "Buy milk")

	done, err := svc.SetDone(StatusRequest{Scope: store.Global, IDs: []string{e.Item.ID}, Done: true})
	if err != nil {
		t.Fatalf("SetDone: %v", err)
	}
	if done.Action != "done" {
		t.Errorf("Action = %q, want \"done\"", done.Action)
	}
	if !done.Entries[0].Item.Done() {
		t.Error("the item is not done after being marked done")
	}

	undone, err := svc.SetDone(StatusRequest{Scope: store.Global, IDs: []string{e.Item.ID}, Done: false})
	if err != nil {
		t.Fatalf("SetDone reopening: %v", err)
	}
	if undone.Action != "undo" {
		t.Errorf("Action = %q, want \"undo\"", undone.Action)
	}
	if undone.Entries[0].Item.Done() {
		t.Error("the item is still done after being reopened")
	}
}

// TestUndoBringsAnItemBackFromTheArchive: the sweep moves a done item out of
// the list, and reopening it has to put it back — otherwise undo leaves an open
// item filed where nothing open is ever looked for.
func TestUndoBringsAnItemBackFromTheArchive(t *testing.T) {
	at := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	svc := newService(t, at)
	e := add(t, svc, "Buy milk")

	done, err := svc.SetDone(StatusRequest{Scope: store.Global, IDs: []string{e.Item.ID}, Done: true})
	if err != nil {
		t.Fatalf("SetDone: %v", err)
	}
	archived := done.Entries[0]
	if _, err := svc.store.Move(archived.Ref, archived.Item, store.Global, store.Archived); err != nil {
		t.Fatalf("sweeping the item into the archive: %v", err)
	}

	res, err := svc.SetDone(StatusRequest{Scope: store.Global, IDs: []string{e.Item.ID}, Done: false})
	if err != nil {
		t.Fatalf("SetDone reopening from the archive: %v", err)
	}
	if got := res.Entries[0].Ref.Area; got != store.Active {
		t.Errorf("a reopened item is filed under %q, want the live list", got)
	}
}

// TestABadIDInTheBatchWritesNothing: every id is resolved before the first item
// is touched, so a command naming four items and one typo changes none of them.
// Half-applying would leave the person to work out which half.
func TestABadIDInTheBatchWritesNothing(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	first := add(t, svc, "Buy milk")
	second := add(t, svc, "Call the vet")

	_, err := svc.SetDone(StatusRequest{
		Scope: store.Global,
		IDs:   []string{first.Item.ID, second.Item.ID, "nosuchid"},
		Done:  true,
	})
	if err == nil {
		t.Fatal("SetDone accepted an id that does not resolve")
	}

	for _, want := range []store.Entry{first, second} {
		got, err := svc.store.Resolve(want.Item.ID, store.Global, store.Active)
		if err != nil {
			t.Fatalf("resolving %s after the failed batch: %v", want.Item.ID, err)
		}
		if got.Item.Done() {
			t.Errorf("%s was marked done by a batch that failed", want.Item.ID)
		}
	}
}

// TestTheSameItemNamedTwiceIsActedOnOnce: naming an id twice is a duplicate
// that changes nothing, so it is taken once rather than refused.
func TestTheSameItemNamedTwiceIsActedOnOnce(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	e := add(t, svc, "Buy milk")

	res, err := svc.SetDone(StatusRequest{
		Scope: store.Global,
		IDs:   []string{e.Item.ID, e.Item.ID},
		Done:  true,
	})
	if err != nil {
		t.Fatalf("SetDone: %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("the same id twice produced %d entries, want 1", len(res.Entries))
	}
	if want := "td: done " + e.Item.ID + " Buy milk"; res.Message != want {
		t.Errorf("Message = %q, want the single-item form %q", res.Message, want)
	}
}

// TestRestoreBringsAnItemBackFromTheTrash: rm is reversible, which is the whole
// reason it moves a file rather than deleting one.
func TestRestoreBringsAnItemBackFromTheTrash(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	e := add(t, svc, "Buy milk")

	if _, err := svc.Remove(MoveRequest{Scope: store.Global, IDs: []string{e.Item.ID}}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	res, err := svc.Restore(MoveRequest{Scope: store.Global, IDs: []string{e.Item.ID}})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.Action != "restore" {
		t.Errorf("Action = %q, want \"restore\"", res.Action)
	}
	if got := res.Entries[0].Ref.Area; got != store.Active {
		t.Errorf("a restored item is filed under %q, want the live list", got)
	}
}
