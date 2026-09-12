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

// lsJSON runs td ls with --json and decodes the result.
func (h *harness) lsJSON(args ...string) lsResult {
	h.t.Helper()
	stdout := h.mustRun(append([]string{"ls", "--json"}, args...)...)
	var res lsResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		h.t.Fatalf("td ls: output is not JSON: %v\n%s", err, stdout)
	}
	return res
}

// ids is the id column of a listing.
func ids(items []itemView) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}

// place writes an item straight into the store with exact timestamps, so an
// ordering test does not depend on how fast the CLI runs.
func (h *harness) place(scope store.Scope, id, title string, created, updated time.Time, doneAt *time.Time) {
	h.t.Helper()
	it := &store.Item{ID: id, Title: title, Created: created, Updated: updated, DoneAt: doneAt}
	if _, err := h.openStore().Save(scope, store.Active, it); err != nil {
		h.t.Fatalf("Save: %v", err)
	}
}

func TestLsShowsOpenItemsOnly(t *testing.T) {
	h := newHarness(t)
	open := h.mutation("add", "Still open").Items[0].ID
	finished := h.mutation("add", "Finished").Items[0].ID
	h.mustRun("done", finished)

	res := h.lsJSON()
	if got := ids(res.Items); len(got) != 1 || got[0] != open {
		t.Errorf("td ls listed %v, want just the open item %s", got, open)
	}

	res = h.lsJSON("--done")
	if got := ids(res.Items); len(got) != 2 {
		t.Errorf("td ls --done listed %v, want both items", got)
	} else if got[1] != finished {
		t.Errorf("td ls --done listed %v, want the done item last", got)
	}
}

func TestLsOrdering(t *testing.T) {
	h := newHarness(t)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return base.AddDate(0, 0, n) }
	// td ls runs the epilogue, whose archive sweep decides whether a done item
	// is still in the list. Against the real clock this fixture passed for a
	// week and then failed forever, the sweep quietly taking a row the order
	// expects. Pinning the clock to day 9 makes the later done_at six days old,
	// inside the 7-day default TTL, whenever the test runs.
	h.freeze(day(9))

	doneEarly, doneLate := day(3), day(5)
	// Deliberately filed out of order.
	h.place(store.Global, "aaaaaaa3", "Open, oldest update", day(1), day(1), nil)
	h.place(store.Global, "aaaaaaa1", "Open, newest update", day(1), day(9), nil)
	h.place(store.Global, "aaaaaaa2", "Open, middle update", day(1), day(4), nil)
	h.place(store.Global, "bbbbbbb1", "Done, most recently", day(1), day(2), &doneLate)
	h.place(store.Global, "bbbbbbb2", "Done, longer ago", day(1), day(8), &doneEarly)

	got := ids(h.lsJSON("--done").Items)
	want := []string{"aaaaaaa1", "aaaaaaa2", "aaaaaaa3", "bbbbbbb1", "bbbbbbb2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v\nwant     %v\n(open by updated desc, then done by done_at desc)", got, want)
	}
}

func TestLsOrderingBreaksTiesOnCreated(t *testing.T) {
	h := newHarness(t)
	updated := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	h.place(store.Global, "aaaaaaa1", "Created first", updated.AddDate(0, 0, -5), updated, nil)
	h.place(store.Global, "aaaaaaa2", "Created later", updated.AddDate(0, 0, -1), updated, nil)

	got := ids(h.lsJSON().Items)
	if len(got) != 2 || got[0] != "aaaaaaa2" {
		t.Errorf("order = %v, want the more recently created first when updated ties", got)
	}
}

func TestLsFiltersByTag(t *testing.T) {
	h := newHarness(t)
	both := h.mutation("add", "Both tags", "-t", "cli", "-t", "urgent").Items[0].ID
	h.mutation("add", "One tag", "-t", "cli")
	h.mutation("add", "No tags")

	if got := ids(h.lsJSON("-t", "cli").Items); len(got) != 2 {
		t.Errorf("td ls -t cli listed %v, want both cli items", got)
	}
	got := ids(h.lsJSON("-t", "cli", "-t", "urgent").Items)
	if len(got) != 1 || got[0] != both {
		t.Errorf("td ls -t cli -t urgent listed %v, want only the item carrying both", got)
	}
	if got := ids(h.lsJSON("-t", "nosuchtag").Items); len(got) != 0 {
		t.Errorf("td ls -t nosuchtag listed %v, want nothing", got)
	}
}

func TestLsAllMergesScopes(t *testing.T) {
	h := newHarness(t)
	h.mustRun("add", "Global item", "-g")
	h.mustRun("add", "Acme item", "-p", "acme")

	if got := ids(h.lsJSON().Items); len(got) != 1 {
		t.Errorf("td ls listed %v, want only the global item", got)
	}

	res := h.lsJSON("--all")
	if len(res.Items) != 2 {
		t.Fatalf("td ls --all listed %d items, want 2", len(res.Items))
	}
	if res.Scope != "all" {
		t.Errorf("scope = %q, want all", res.Scope)
	}
	scopes := map[string]bool{}
	for _, item := range res.Items {
		scopes[item.Scope] = true
	}
	if !scopes["global"] || !scopes["acme"] {
		t.Errorf("scopes = %v, want both global and acme reported", scopes)
	}

	// The human table gains a scope column only under --all.
	if out := h.mustRun("ls", "--all"); !strings.Contains(out, "SCOPE") {
		t.Errorf("td ls --all table has no scope column:\n%s", out)
	}
	if out := h.mustRun("ls"); strings.Contains(out, "SCOPE") {
		t.Errorf("td ls table has a scope column:\n%s", out)
	}
}

func TestLsEmptyListPrintsNothing(t *testing.T) {
	h := newHarness(t)
	if out := h.mustRun("ls"); out != "" {
		t.Errorf("td ls on an empty store printed %q, want nothing", out)
	}
	res := h.lsJSON()
	if len(res.Items) != 0 {
		t.Errorf("items = %v, want none", res.Items)
	}
	// An empty listing must still be a JSON array, not null.
	stdout := h.mustRun("ls", "--json")
	if !strings.Contains(stdout, `"items": []`) {
		t.Errorf("td ls --json on an empty store = %s, want an empty array", stdout)
	}
}

func TestLsTableOmitsEmptyColumns(t *testing.T) {
	h := newHarness(t)
	h.mustRun("add", "No due date, no tags")

	out := h.mustRun("ls")
	for _, col := range []string{"DUE", "TAGS", "STATUS"} {
		if strings.Contains(out, col) {
			t.Errorf("table carries an all-empty %s column:\n%s", col, out)
		}
	}
	if !strings.Contains(out, "TITLE") {
		t.Errorf("table has no TITLE column:\n%s", out)
	}
}

func TestShowPrintsTheWholeItem(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Wire it", "-t", "cli", "--due", "2026-09-30", "-b", "The body.").Items[0].ID

	out := h.mustRun("show", id[:4])
	for _, want := range []string{id, "Wire it", "cli", "2026-09-30", "The body."} {
		if !strings.Contains(out, want) {
			t.Errorf("td show output is missing %q:\n%s", want, out)
		}
	}

	stdout := h.mustRun("show", id, "--json")
	var res showResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout)
	}
	if res.Item.Body != "The body.\n" {
		t.Errorf("body = %q, want the item's body included", res.Item.Body)
	}
}

func TestShowFindsTrashedAndArchivedItems(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Thrown away").Items[0].ID
	h.mustRun("rm", id)

	out := h.mustRun("show", id)
	if !strings.Contains(out, "deleted") {
		t.Errorf("td show does not report the item as trashed:\n%s", out)
	}
}

// TestCommandsTakeAnIDSuffix: the three characters the TUI's i key prints are
// the tail of an id, and every command that takes an id has to accept that form
// — otherwise the tag on screen is something a reader can see and not use.
func TestCommandsTakeAnIDSuffix(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Wire the epilogue").Items[0].ID
	tag := store.ShortID(id)

	out := h.mustRun("show", tag)
	if !strings.Contains(out, id) {
		t.Errorf("td show %s did not find %s:\n%s", tag, id, out)
	}
	if got := h.mutation("done", tag).Items[0].ID; got != id {
		t.Errorf("td done %s acted on %s, want %s", tag, got, id)
	}
}

// TestAmbiguousIDNamesEveryCandidateWithItsTitle: two items sharing the tail the
// pane shows is the collision the reader has to resolve, and they resolve it by
// title — a list of bare ids only tells them to guess again.
func TestAmbiguousIDNamesEveryCandidateWithItsTitle(t *testing.T) {
	h := newHarness(t)
	now := time.Now().UTC().Truncate(time.Second)
	h.place(store.Global, "64qmbaaa", "Fix the backend", now, now, nil)
	h.place(store.Global, "64s3caaa", "Ship the release", now, now, nil)

	_, _, err := h.run("done", "aaa")
	if err == nil {
		t.Fatal("td done aaa = nil, want an ambiguous-id error")
	}
	for _, want := range []string{"64qmbaaa", "Fix the backend", "64s3caaa", "Ship the release"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q:\n%v", want, err)
		}
	}
}

func TestShowUnknownIDFails(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.run("show", "nosuchid"); err == nil {
		t.Error("td show nosuchid = nil, want an error")
	}
}

func TestReadCommandsRecordHandEditsWithoutPushing(t *testing.T) {
	// The plan's verify for this step: a hand-edited file shows a bumped
	// updated and a commit, with no push attempted.
	h := newHarness(t)
	// File the item with an old updated timestamp, so a bump to now is visibly
	// later rather than landing in the same second the test runs in.
	old := time.Now().UTC().AddDate(0, 0, -3).Truncate(time.Second)
	const id = "aaaaaaa1"
	h.place(store.Global, id, "Hand edit me", old, old, nil)

	bare := filepath.Join(t.TempDir(), "remote.git")
	if _, err := gitx.New(t.TempDir()).Run("init", "--bare", "--quiet", bare); err != nil {
		t.Fatal(err)
	}
	repo := gitx.New(h.root)
	if _, err := repo.Run("remote", "add", "origin", bare); err != nil {
		t.Fatal(err)
	}

	commitsBefore := h.commits()

	// Edit the file the way a person would. td pinned its mtime to the old
	// updated value, so writing to it now moves mtime past that on its own.
	path := h.list(store.Active)[0].Ref.Path
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, []byte("\nAdded by hand.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	res := h.lsJSON()
	if len(res.Epilogue.Bumped) != 1 || res.Epilogue.Bumped[0] != id {
		t.Errorf("bumped = %v, want the hand-edited item", res.Epilogue.Bumped)
	}
	if !res.Items[0].Updated.After(old) {
		t.Errorf("updated = %v, want it bumped past the old %v", res.Items[0].Updated, old)
	}
	if !res.Epilogue.Committed {
		t.Error("the hand edit was not committed")
	}
	if got := h.commits(); got != commitsBefore+1 {
		t.Errorf("commit count = %d, want one more than %d", got, commitsBefore)
	}

	if res.Epilogue.Pushed {
		t.Error("td ls pushed; reads must never push")
	}
	remote := gitx.New(bare)
	has, err := remote.HasCommits()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("the remote's ref moved; td ls must not push")
	}
}

func TestMaintenanceCommands(t *testing.T) {
	h := newHarness(t)
	id := h.mutation("add", "Item").Items[0].ID

	// bump: record a hand edit, without committing it.
	path := h.list(store.Active)[0].Ref.Path
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, []byte("\nBy hand.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Time{}, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	commitsBefore := h.commits()
	stdout := h.mustRun("bump", "--json")
	var res maintenanceResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Epilogue.Bumped) != 1 {
		t.Errorf("td bump reported %v, want the hand edit", res.Epilogue.Bumped)
	}
	if res.Epilogue.Committed || h.commits() != commitsBefore {
		t.Error("td bump committed; it must run only its own step")
	}

	// commit: record what bump left behind.
	stdout = h.mustRun("commit", "--json")
	res = maintenanceResult{}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Epilogue.Committed {
		t.Error("td commit did not commit")
	}
	if got := h.commits(); got != commitsBefore+1 {
		t.Errorf("commit count = %d, want one more than %d", got, commitsBefore)
	}

	// archive: sweep a long-done item.
	h.mustRun("done", id)
	e := h.list(store.Active)[0]
	longAgo := time.Now().UTC().AddDate(0, 0, -30).Truncate(time.Second)
	e.Item.DoneAt = &longAgo
	if _, err := h.openStore().Save(store.Global, store.Active, e.Item); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun("archive", "--json")
	res = maintenanceResult{}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Epilogue.Archived) != 1 {
		t.Errorf("td archive reported %v, want the long-done item", res.Epilogue.Archived)
	}
	if len(h.list(store.Archived)) != 1 {
		t.Error("the item is not in archived/")
	}

	// push: a no-op without a remote, and not an error.
	if _, _, err := h.run("push"); err != nil {
		t.Errorf("td push with no remote = %v, want nil", err)
	}
}

func TestCommitAndPushOverrideTheirAutoSettings(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(h.root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(h.root, "config.toml")
	if err := os.WriteFile(cfg, []byte("auto_commit = false\nauto_push = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.mustRun("add", "Not committed")
	if got := h.commits(); got != 0 {
		t.Fatalf("commit count = %d with auto_commit off, want 0", got)
	}

	h.mustRun("commit")
	if got := h.commits(); got != 1 {
		t.Errorf("commit count = %d, want td commit to commit despite auto_commit being off", got)
	}
}
