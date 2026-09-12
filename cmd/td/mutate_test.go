package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vkovic/td/internal/gitx"
	"github.com/vkovic/td/internal/store"
)

// only returns the single entry in a listing, failing otherwise.
func only(t *testing.T, entries []store.Entry, what string) store.Entry {
	t.Helper()
	if len(entries) != 1 {
		t.Fatalf("%s holds %d items, want 1", what, len(entries))
	}
	return entries[0]
}

// list reads one area of the global scope.
func (h *harness) list(area store.Area) []store.Entry {
	h.t.Helper()
	entries, err := h.openStore().List(store.Global, area)
	if err != nil {
		h.t.Fatalf("List: %v", err)
	}
	return entries
}

func TestAddCreatesAnItem(t *testing.T) {
	h := newHarness(t)
	res := h.mutation("add", "Wire", "the", "epilogue", "-t", "cli", "-t", "epilogue", "--due", "2026-09-30", "-b", "Some detail.")

	if res.Action != "add" {
		t.Errorf("action = %q, want add", res.Action)
	}
	item := res.Items[0]
	if item.Title != "Wire the epilogue" {
		t.Errorf("title = %q, want the words after add joined", item.Title)
	}
	if strings.Join(item.Tags, ",") != "cli,epilogue" {
		t.Errorf("tags = %v", item.Tags)
	}
	if item.Due != "2026-09-30" {
		t.Errorf("due = %q", item.Due)
	}
	if item.Done || item.DoneAt != nil {
		t.Errorf("a new item is done: %+v", item)
	}
	if item.Body != "Some detail.\n" {
		t.Errorf("body = %q", item.Body)
	}
	if item.Scope != "global" || item.Area != "active" {
		t.Errorf("scope/area = %s/%s, want global/active", item.Scope, item.Area)
	}

	e := only(t, h.list(store.Active), "the live list")
	if e.Item.ID != item.ID {
		t.Errorf("the file holds %q, want %q", e.Item.ID, item.ID)
	}
	if h.commits() != 1 {
		t.Errorf("commit count = %d, want exactly 1 for one add", h.commits())
	}
	if !res.Epilogue.Committed {
		t.Error("the epilogue reported no commit")
	}
}

func TestAddRequiresATitle(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.run("add", "   "); err == nil {
		t.Error("td add with a blank title = nil, want an error")
	}
}

func TestAddBodyFromStdin(t *testing.T) {
	h := newHarness(t)
	if _, stderr, err := h.runStdin("From a pipe.\n", "add", "Piped", "--body-file", "-"); err != nil {
		t.Fatalf("td add --body-file -: %v\n%s", err, stderr)
	}
	e := only(t, h.list(store.Active), "the live list")
	if e.Item.Body != "From a pipe.\n" {
		t.Errorf("body = %q, want the piped text", e.Item.Body)
	}
}

func TestAddBodyFromFile(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("\n\nFrom a file.\n\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun("add", "From file", "--body-file", path)

	e := only(t, h.list(store.Active), "the live list")
	if e.Item.Body != "From a file.\n" {
		t.Errorf("body = %q, want the file's text with its blank lines trimmed", e.Item.Body)
	}
}

func TestAddRejectsBothBodyFlags(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.run("add", "Both", "-b", "x", "--body-file", "-"); err == nil {
		t.Error("td add -b --body-file = nil, want an error")
	}
}

func TestAddRejectsABadDueDate(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.run("add", "Bad due", "--due", "next tuesday"); err == nil {
		t.Error("td add --due 'next tuesday' = nil, want an error")
	}
	if len(h.list(store.Active)) != 0 {
		t.Error("a rejected add still wrote an item")
	}
}

func TestAddSplitsCommaSeparatedTags(t *testing.T) {
	h := newHarness(t)
	res := h.mutation("add", "Tagged", "-t", "cli,epilogue", "-t", "cli")
	if strings.Join(res.Items[0].Tags, ",") != "cli,epilogue" {
		t.Errorf("tags = %v, want them split and de-duplicated", res.Items[0].Tags)
	}
}

func TestAddRecordsProvenance(t *testing.T) {
	h := newHarness(t)
	res := h.mutation("add", "From Claude",
		"--source", "claude", "--session-name", "milestone-1", "--session-id", "3a28d128")

	item := res.Items[0]
	if item.Source != "claude" || item.ClaudeSessionName != "milestone-1" || item.ClaudeSessionID != "3a28d128" {
		t.Errorf("provenance not recorded: %+v", item)
	}
}

// TestAddRecordsTheWorkingDirectory: context holds the directory an item was
// raised from, which is the whole reason an item raised mid-session can be
// traced back to what the session was doing.
func TestAddRecordsTheWorkingDirectory(t *testing.T) {
	h := newHarness(t)
	// After the harness, which runs each test from a directory of its own —
	// the same thing that makes this field worth recording.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	res := h.mutation("add", "Raised from here")

	if got := res.Items[0].Context; got != wd {
		t.Errorf("context = %q, want the working directory %q", got, wd)
	}
	e := only(t, h.list(store.Active), "the live list")
	if e.Item.Context != wd {
		t.Errorf("the file records context %q, want %q", e.Item.Context, wd)
	}
}

func TestAddIntoAProjectScope(t *testing.T) {
	h := newHarness(t)
	h.mustRun("link", "acme")
	h.mustRun("add", "Project item")

	entries, err := h.openStore().List(store.Scope("acme"), store.Active)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the acme list holds %d items, want 1", len(entries))
	}
	if len(h.list(store.Active)) != 0 {
		t.Error("the item landed in the global list as well")
	}

	// -g reaches past the marker.
	h.mustRun("add", "Global item", "-g")
	if len(h.list(store.Active)) != 1 {
		t.Error("td add -g did not reach the global list")
	}
}

func TestEditChangesOnlyWhatIsNamed(t *testing.T) {
	h := newHarness(t)
	added := h.mutation("add", "Original", "-t", "one", "--due", "2026-09-30", "-b", "Original body.")
	id := added.Items[0].ID

	h.mustRun("edit", id[:4], "--title", "Renamed")

	e := only(t, h.list(store.Active), "the live list")
	if e.Item.Title != "Renamed" {
		t.Errorf("title = %q, want Renamed", e.Item.Title)
	}
	if strings.Join(e.Item.Tags, ",") != "one" {
		t.Errorf("tags = %v, want them untouched", e.Item.Tags)
	}
	if e.Item.Due == nil || e.Item.Due.String() != "2026-09-30" {
		t.Errorf("due = %v, want it untouched", e.Item.Due)
	}
	if e.Item.Body != "Original body.\n" {
		t.Errorf("body = %q, want it untouched", e.Item.Body)
	}
	if filepath.Base(e.Ref.Path) != id+"-renamed.md" {
		t.Errorf("file is %q, want it renamed to the new slug", filepath.Base(e.Ref.Path))
	}
}

func TestEditAppendsToTheBody(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Notes", "-b", "First.").Items[0].ID
	h.mustRun("edit", id, "--append", "Second.")

	e := only(t, h.list(store.Active), "the live list")
	if e.Item.Body != "First.\n\nSecond.\n" {
		t.Errorf("body = %q, want the paragraphs separated by a blank line", e.Item.Body)
	}
}

func TestEditClearsTheDueDate(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Due soon", "--due", "2026-09-30").Items[0].ID
	h.mustRun("edit", id, "--due", "")

	e := only(t, h.list(store.Active), "the live list")
	if e.Item.Due != nil {
		t.Errorf("due = %v, want it cleared", e.Item.Due)
	}
}

func TestEditReplacesTags(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Tagged", "-t", "old").Items[0].ID
	h.mustRun("edit", id, "-t", "new")

	e := only(t, h.list(store.Active), "the live list")
	if strings.Join(e.Item.Tags, ",") != "new" {
		t.Errorf("tags = %v, want them replaced", e.Item.Tags)
	}
}

func TestEditNeedsSomethingToChange(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Item").Items[0].ID
	if _, _, err := h.run("edit", id); err == nil {
		t.Error("td edit with no flags = nil, want an error")
	}
}

func TestEditRejectsBodyAndAppendTogether(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Item").Items[0].ID
	if _, _, err := h.run("edit", id, "-b", "new", "--append", "more"); err == nil {
		t.Error("td edit -b --append = nil, want an error")
	}
}

func TestDoneAndUndo(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Finish it").Items[0].ID

	res := h.mutation("done", id[:4])
	if !res.Items[0].Done || res.Items[0].DoneAt == nil {
		t.Errorf("done did not stamp done_at: %+v", res.Items[0])
	}
	e := only(t, h.list(store.Active), "the live list")
	if !e.Item.Done() {
		t.Error("the file does not record the item as done")
	}

	res = h.mutation("undo", id)
	if res.Items[0].Done || res.Items[0].DoneAt != nil {
		t.Errorf("undo did not clear done_at: %+v", res.Items[0])
	}
	e = only(t, h.list(store.Active), "the live list")
	if e.Item.Done() {
		t.Error("the file still records the item as done")
	}
}

func TestUndoReachesIntoTheArchive(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Long done").Items[0].ID

	// Put the item in the archive by hand, as the sweep would.
	s := h.openStore()
	e := only(t, h.list(store.Active), "the live list")
	if _, err := s.Move(e.Ref, e.Item, store.Global, store.Archived); err != nil {
		t.Fatal(err)
	}

	h.mustRun("undo", id)
	if len(h.list(store.Archived)) != 0 {
		t.Error("the item is still in the archive")
	}
	back := only(t, h.list(store.Active), "the live list")
	if back.Item.Done() {
		t.Error("the reopened item is still done")
	}
}

func TestRemoveAndRestore(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Throw away").Items[0].ID

	res := h.mutation("rm", id[:4])
	if res.Items[0].Area != "deleted" {
		t.Errorf("area = %q, want deleted", res.Items[0].Area)
	}
	if len(h.list(store.Active)) != 0 {
		t.Error("the item is still in the live list")
	}
	trashed := only(t, h.list(store.Deleted), "the trash")
	if !strings.Contains(trashed.Ref.Path, string(store.Deleted)) {
		t.Errorf("the file is at %q, want it under deleted/", trashed.Ref.Path)
	}

	res = h.mutation("restore", id)
	if res.Items[0].Area != "active" {
		t.Errorf("area = %q, want active", res.Items[0].Area)
	}
	if len(h.list(store.Deleted)) != 0 {
		t.Error("the item is still in the trash")
	}
	if len(h.list(store.Active)) != 1 {
		t.Error("the item did not come back to the live list")
	}
}

func TestRemoveDoesNotEraseTheFile(t *testing.T) {
	h := newHarness(t)
	h.mutation("add", "Throw away", "-b", "Worth keeping.")
	h.mustRun("rm", h.list(store.Active)[0].Item.ID)

	trashed := only(t, h.list(store.Deleted), "the trash")
	if trashed.Item.Body != "Worth keeping.\n" {
		t.Errorf("the trashed item lost its body: %q", trashed.Item.Body)
	}
}

func TestTrashIsNotCommitted(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Throw away").Items[0].ID
	h.mustRun("rm", id)

	out, err := h.openStore().Repo().Run("ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, string(store.Deleted)+"/") {
		t.Errorf("the trash was committed:\n%s", out)
	}
}

func TestEachMutationLeavesExactlyOneCommit(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Round trip").Items[0].ID
	if got := h.commits(); got != 1 {
		t.Fatalf("commit count after add = %d, want 1", got)
	}
	for i, args := range [][]string{
		{"edit", id, "--title", "Round trip, renamed"},
		{"done", id},
		{"undo", id},
		{"rm", id},
		{"restore", id},
	} {
		h.mustRun(args...)
		if got, want := h.commits(), i+2; got != want {
			t.Errorf("commit count after td %s = %d, want %d", strings.Join(args, " "), got, want)
		}
	}
}

func TestMultipleIdsInOneCommand(t *testing.T) {
	h := newHarness(t)
	a := h.mutation("add", "First").Items[0].ID
	b := h.mutation("add", "Second").Items[0].ID

	res := h.mutation("done", a, b)
	if len(res.Items) != 2 {
		t.Fatalf("done reported %d items, want 2", len(res.Items))
	}
	for _, e := range h.list(store.Active) {
		if !e.Item.Done() {
			t.Errorf("%s is not done", e.Item.ID)
		}
	}
	if got := h.commits(); got != 3 {
		t.Errorf("commit count = %d, want 3: two adds and one done", got)
	}
}

func TestABadIdChangesNothing(t *testing.T) {
	h := newHarness(t)
	good := h.mutation("add", "Good").Items[0].ID
	before := h.commits()

	if _, _, err := h.run("done", good, "nosuchid"); err == nil {
		t.Fatal("td done with an unknown id = nil, want an error")
	}
	e := only(t, h.list(store.Active), "the live list")
	if e.Item.Done() {
		t.Error("the valid id was applied even though another failed")
	}
	if got := h.commits(); got != before {
		t.Errorf("commit count = %d, want it unchanged at %d", got, before)
	}
}

func TestAmbiguousPrefixIsRejected(t *testing.T) {
	h := newHarness(t)
	a := h.mutation("add", "First").Items[0].ID
	h.mutation("add", "Second")

	// Ids are sequential, so a short prefix matches both.
	_, _, err := h.run("done", a[:2])
	if err == nil {
		t.Fatal("td done with an ambiguous prefix = nil, want an error")
	}
	if !strings.Contains(err.Error(), "more than one") {
		t.Errorf("error = %v, want it to say the prefix is ambiguous", err)
	}
}

// TestCommitTakesAMessage: td commit takes -m, and a hand tidy deserves a
// subject that says what it was rather than one describing a run that recorded
// nothing.
func TestCommitTakesAMessage(t *testing.T) {
	h := newHarness(t)
	h.mustRun("add", "Something")

	appendLine(h.t, h)
	h.mustRun("commit", "-m", "hand tidy")

	if got := subject(t, h); got != "hand tidy" {
		t.Errorf("the commit subject is %q, want the message td commit -m was given", got)
	}

	// --json reports the message it used, so a caller can see what it wrote.
	appendLine(h.t, h)
	out := h.mustRun("commit", "--json", "-m", "x")
	var res maintenanceResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Epilogue.Message != "x" {
		t.Errorf("--json reports message %q, want x", res.Epilogue.Message)
	}

	// With no -m, the default message describing the run still applies.
	appendLine(h.t, h)
	h.mustRun("commit")
	if got := subject(t, h); got == "hand tidy" || got == "x" {
		t.Errorf("the commit subject is %q, want the default message", got)
	}
}

// appendLine dirties the store the way a hand edit does, so there is something
// for the next commit to record.
func appendLine(t *testing.T, h *harness) {
	t.Helper()
	e := only(t, h.list(store.Active), "the live list")
	f, err := os.OpenFile(e.Ref.Path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\na line\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// subject is the subject line of the store's most recent commit.
func subject(t *testing.T, h *harness) string {
	t.Helper()
	out, err := gitx.New(h.root).Run("log", "-1", "--format=%s")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func TestNoEpilogueSkipsTheCommit(t *testing.T) {
	h := newHarness(t)
	h.mustRun("add", "Uncommitted", "--no-epilogue")
	if got := h.commits(); got != 0 {
		t.Errorf("commit count = %d with --no-epilogue, want 0", got)
	}
	if len(h.list(store.Active)) != 1 {
		t.Error("--no-epilogue also skipped writing the item")
	}
}

// TestTheClockIsInjectable pins what the seam is for. One clock decides both
// the instant a command stamps an item with and the instant the archive sweep
// measures a done TTL against, and a test can choose it.
//
// Without it the CLI could fix neither, which is how TestLsOrdering came to
// pass for a week and fail ever after: its fixture named a calendar day, the
// sweep compared against the real one, and the gap between them grew.
func TestTheClockIsInjectable(t *testing.T) {
	h := newHarness(t)
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	h.freeze(at)

	item := h.mutation("add", "Pinned").Items[0]
	if !item.Created.Equal(at) || !item.Updated.Equal(at) {
		t.Errorf("created = %s and updated = %s, want both %s", item.Created, item.Updated, at)
	}

	// Completed at the frozen instant, the item is no days old however long ago
	// that date really was, so the sweep leaves it in the list.
	h.mustRun("done", item.ID)
	if got := ids(h.lsJSON("--done").Items); len(got) != 1 {
		t.Errorf("td ls --done listed %v, want the item still in the list", got)
	}

	// Eight days on — a day past the default TTL — the same item is swept out.
	h.freeze(at.AddDate(0, 0, 8))
	if got := ids(h.lsJSON("--done").Items); len(got) != 0 {
		t.Errorf("td ls --done listed %v eight days on, want it archived", got)
	}
}
