package task

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vkovic/td/internal/store"
	"github.com/vkovic/td/internal/tdtest"
)

// newService opens a store in a temp directory with both the stamping clock and
// the id source pinned, so an added item is reproducible down to its bytes. The
// generator advances past the last id it handed out, so a frozen clock still
// yields a distinct id per call.
func newService(t *testing.T, at time.Time) *Service {
	t.Helper()
	tdtest.IsolateGit(t)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	clock := func() time.Time { return at }
	return New(st, clock, WithIDGen(store.NewIDGen(clock)))
}

// added returns the file one Add wrote, with the two things that are meant to
// differ between two adds replaced: the item's own id, and the source the
// surface named itself by.
func added(t *testing.T, res Result) string {
	t.Helper()
	b, err := os.ReadFile(res.Entries[0].Ref.Path)
	if err != nil {
		t.Fatalf("reading the item back: %v", err)
	}
	it := res.Entries[0].Item
	out := strings.ReplaceAll(string(b), it.ID, "<id>")
	return strings.ReplaceAll(out, "source: "+it.Source, "source: <source>")
}

// TestAddWritesTheSameItemWhicheverSurfaceAsked: td add and the pane's a key
// each built the item themselves, in two places, and the pair was kept in
// agreement by hand. Both call Add now, so the only difference a surface can
// introduce is what it puts in the request. Two requests differing only in
// source must produce files differing only in id and source.
func TestAddWritesTheSameItemWhicheverSurfaceAsked(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))

	cli, err := svc.Add(AddRequest{Scope: store.Global, Title: "Buy milk", Source: "cli"})
	if err != nil {
		t.Fatalf("Add as the CLI: %v", err)
	}
	tui, err := svc.Add(AddRequest{Scope: store.Global, Title: "Buy milk", Source: "tui"})
	if err != nil {
		t.Fatalf("Add as the TUI: %v", err)
	}

	if got, want := added(t, cli), added(t, tui); got != want {
		t.Errorf("the two surfaces no longer write the same item\n--- cli ---\n%s\n--- tui ---\n%s", got, want)
	}
	if cli.Entries[0].Item.ID == tui.Entries[0].Item.ID {
		t.Error("two adds share an id, so the file comparison above proves nothing")
	}
}

// TestAddCommitMessageIsTheSameFromEitherSurface: the two surfaces each had
// their own commitMessage, and each carried a comment telling the reader it had
// to agree with the other one. The TUI still composes its message at a
// different moment — after its editor exits, from the entry it holds — so this
// pins that the moment does not change the string.
func TestAddCommitMessageIsTheSameFromEitherSurface(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))

	res, err := svc.Add(AddRequest{Scope: store.Global, Title: "Buy milk", Source: "cli"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	it := res.Entries[0].Item
	if want := "td: add " + it.ID + " Buy milk"; res.Message != want {
		t.Errorf("Result.Message = %q, want %q", res.Message, want)
	}
	if got := CommitMessage("add", []store.Entry{{Item: it}}); got != res.Message {
		t.Errorf("the TUI's later message %q is not the service's %q", got, res.Message)
	}
}

// TestAddStampsCreatedAndUpdatedTogether: an item nobody has edited reads as
// created and updated at the same instant. The hand-edit check compares a
// file's mtime against updated, so two stamps a hair apart would make td report
// its own write as somebody else's edit.
func TestAddStampsCreatedAndUpdatedTogether(t *testing.T) {
	at := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	svc := newService(t, at)

	res, err := svc.Add(AddRequest{Scope: store.Global, Title: "Buy milk"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	it := res.Entries[0].Item
	if !it.Created.Equal(at) || !it.Updated.Equal(at) {
		t.Errorf("created = %v, updated = %v, want both %v", it.Created, it.Updated, at)
	}
}

// TestAddRefusesATitleOfNothing: a title of only spaces leaves an item with no
// handle anyone could find it by, so the request fails and nothing is written.
func TestAddRefusesATitleOfNothing(t *testing.T) {
	svc := newService(t, time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC))

	res, err := svc.Add(AddRequest{Scope: store.Global, Title: "   \t "})
	if !errors.Is(err, ErrEmptyTitle) {
		t.Fatalf("Add error = %v, want ErrEmptyTitle", err)
	}
	if len(res.Entries) != 0 {
		t.Errorf("a refused add still reported %d entries", len(res.Entries))
	}
	files, _ := filepath.Glob(filepath.Join(svc.store.Root(), "*.md"))
	if len(files) != 0 {
		t.Errorf("a refused add still wrote %v", files)
	}
}

// TestCommitMessageCountsWhatItCannotName: one item is named, several are
// counted, and none at all says nothing rather than "0 items" — which is what
// the TUI relies on when it has no entry to describe.
func TestCommitMessageCountsWhatItCannotName(t *testing.T) {
	one := []store.Entry{{Item: &store.Item{ID: "01hx2b9f", Title: "Buy milk"}}}
	two := append(append([]store.Entry(nil), one...), store.Entry{Item: &store.Item{ID: "01hx2b9g", Title: "Call the vet"}})

	for _, tc := range []struct {
		name    string
		entries []store.Entry
		want    string
	}{
		{"none", nil, ""},
		{"one", one, "td: add 01hx2b9f Buy milk"},
		{"several", two, "td: add 2 items"},
	} {
		if got := CommitMessage("add", tc.entries); got != tc.want {
			t.Errorf("%s: CommitMessage = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestTwoServicesInOneProcessDoNotIssueTheSameID: running the pane builds two
// Services — cmd/td makes one in setup, internal/tui makes another — and an id
// encodes the millisecond it was minted. A Service minting from a generator of
// its own would have its own counter, and two counters cannot keep two items
// created inside one millisecond apart. They mint from the process-wide
// generator, which is the whole reason that generator exists.
//
// The ids are drawn directly rather than through Add, which writes a file and
// so spaces its calls out over more than a millisecond each — enough to hide
// the collision this is here to catch.
func TestTwoServicesInOneProcessDoNotIssueTheSameID(t *testing.T) {
	cli, tui := New(nil, nil), New(nil, nil)

	seen := make(map[string]bool)
	last := ""
	for i := range 200 {
		svc := cli
		if i%2 == 1 {
			svc = tui
		}
		id := svc.newID()
		if seen[id] {
			t.Fatalf("the two services issued %q twice, at call %d", id, i)
		}
		if id <= last {
			t.Fatalf("id %q at call %d does not follow %q", id, i, last)
		}
		seen[id], last = true, id
	}
}
