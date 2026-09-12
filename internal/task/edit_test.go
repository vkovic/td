package task

import (
	"errors"
	"testing"
	"time"

	"github.com/vkovic/td/internal/store"
)

// edited applies a change to one fresh item and hands back what was saved.
func edited(t *testing.T, svc *Service, seed AddRequest, ch Change) *store.Item {
	t.Helper()
	seed.Scope = store.Global
	res, err := svc.Add(seed)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	out, err := svc.Edit(res.Entries, ch)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	back, err := svc.store.Resolve(out.Entries[0].Item.ID, store.Global, store.Active)
	if err != nil {
		t.Fatalf("reading the item back: %v", err)
	}
	return back.Item
}

// TestEditLeavesEverythingItWasNotAskedAbout: the rule the whole Change struct
// exists to hold. A change naming one field must not blank the others, which is
// what a plain struct of values would have done the first time somebody passed
// one without filling it in.
func TestEditLeavesEverythingItWasNotAskedAbout(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	due := mustDate(t, "2026-09-30")
	title := "Buy oat milk"

	it := edited(t, svc, AddRequest{
		Title: "Buy milk",
		Tags:  []string{"errand"},
		Due:   &due,
		Body:  "from the corner shop\n",
	}, Change{Title: &title})

	if it.Title != "Buy oat milk" {
		t.Errorf("Title = %q, want the replacement", it.Title)
	}
	if len(it.Tags) != 1 || it.Tags[0] != "errand" {
		t.Errorf("Tags = %v, want the originals kept", it.Tags)
	}
	if it.Due == nil || it.Due.String() != "2026-09-30" {
		t.Errorf("Due = %v, want the original kept", it.Due)
	}
	if it.Body != "from the corner shop\n" {
		t.Errorf("Body = %q, want the original kept", it.Body)
	}
}

// TestEditReplacesTagsRatherThanAddingToThem: --tag states the whole set. A
// change that appended would make removing a tag impossible.
func TestEditReplacesTagsRatherThanAddingToThem(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	tags := []string{"shopping"}

	it := edited(t, svc, AddRequest{Title: "Buy milk", Tags: []string{"errand", "urgent"}}, Change{Tags: &tags})

	if len(it.Tags) != 1 || it.Tags[0] != "shopping" {
		t.Errorf("Tags = %v, want only the replacement", it.Tags)
	}
}

// TestClearDueEmptiesTheFieldAndNilLeavesIt: the two states a pointer alone
// cannot tell apart, which is why Due is its own type.
func TestClearDueEmptiesTheFieldAndNilLeavesIt(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	due := mustDate(t, "2026-09-30")
	title := "Buy oat milk"

	cleared := edited(t, svc, AddRequest{Title: "Buy milk", Due: &due}, Change{Due: ClearDue()})
	if cleared.Due != nil {
		t.Errorf("Due = %v after ClearDue, want it gone", cleared.Due)
	}

	kept := edited(t, svc, AddRequest{Title: "Buy milk", Due: &due}, Change{Title: &title})
	if kept.Due == nil {
		t.Error("a change that did not mention the due date cleared it")
	}

	later := mustDate(t, "2026-10-15")
	moved := edited(t, svc, AddRequest{Title: "Buy milk", Due: &due}, Change{Due: SetDue(later)})
	if moved.Due == nil || moved.Due.String() != "2026-10-15" {
		t.Errorf("Due = %v, want the replacement", moved.Due)
	}
}

// TestEditAppendsAParagraphRatherThanOverwriting: --append adds to the body,
// separated by a blank line so the result is still markdown.
func TestEditAppendsAParagraphRatherThanOverwriting(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	add := "and oat milk"

	it := edited(t, svc, AddRequest{Title: "Buy milk", Body: "from the corner shop\n"}, Change{Append: &add})

	if want := "from the corner shop\n\nand oat milk\n"; it.Body != want {
		t.Errorf("Body = %q, want %q", it.Body, want)
	}
}

// TestEditRefusesToBlankATitle: an item with no title has no handle anyone
// could find it by, so replacing one with nothing is refused rather than done.
func TestEditRefusesToBlankATitle(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	e := add(t, svc, "Buy milk")
	blank := ""

	if _, err := svc.Edit([]store.Entry{e}, Change{Title: &blank}); !errors.Is(err, ErrEmptyTitle) {
		t.Fatalf("Edit error = %v, want ErrEmptyTitle", err)
	}
	back, err := svc.store.Resolve(e.Item.ID, store.Global, store.Active)
	if err != nil {
		t.Fatalf("reading the item back: %v", err)
	}
	if back.Item.Title != "Buy milk" {
		t.Errorf("Title = %q, want the refused edit to have written nothing", back.Item.Title)
	}
}

// TestEditRefusesAChangeThatSetsNothing: a change with every field nil would
// stamp updated on every item it named and commit, for no reason.
func TestEditRefusesAChangeThatSetsNothing(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))
	e := add(t, svc, "Buy milk")

	if _, err := svc.Edit([]store.Entry{e}, Change{}); !errors.Is(err, ErrNothingToChange) {
		t.Errorf("Edit error = %v, want ErrNothingToChange", err)
	}
}

// mustDate is store.ParseDate with the error turned into a failure.
func mustDate(t *testing.T, s string) store.Date {
	t.Helper()
	d, err := store.ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate %q: %v", s, err)
	}
	return d
}
