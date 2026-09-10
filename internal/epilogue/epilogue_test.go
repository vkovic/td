package epilogue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/gitx"
	"github.com/vkovic/td/internal/store"
)

// fixedNow is the clock every test runs against, so a TTL boundary is exact.
var fixedNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func at(t time.Time) func() time.Time { return func() time.Time { return t } }

// isolate keeps git from reading the developer's own configuration.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-system"))
	t.Setenv("GIT_AUTHOR_NAME", "td test")
	t.Setenv("GIT_AUTHOR_EMAIL", "td@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "td test")
	t.Setenv("GIT_COMMITTER_EMAIL", "td@example.invalid")
}

// newStore opens a store in a temp directory with git isolated.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	isolate(t)
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

// add files an item, written as td would write it.
func add(t *testing.T, s *store.Store, scope store.Scope, id, title string, updated time.Time, doneAt *time.Time) store.Ref {
	t.Helper()
	it := &store.Item{
		ID:      id,
		Title:   title,
		Created: updated,
		Updated: updated,
		DoneAt:  doneAt,
	}
	ref, err := s.Save(scope, store.Active, it)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	return ref
}

// handEdit appends to an item file the way a person editing it would, moving
// its modification time past the updated timestamp inside it.
func handEdit(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, []byte("\nA note added by hand.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Time{}, fixedNow.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
}

// commitCount is how many commits a repository holds.
func commitCount(t *testing.T, repo *gitx.Repo) int {
	t.Helper()
	has, err := repo.HasCommits()
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		return 0
	}
	out, err := repo.Run("rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range strings.TrimSpace(out) {
		n = n*10 + int(c-'0')
	}
	return n
}

// run executes a git command in dir, for assertions the gitx API does not cover.
func run(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	out, err := gitx.New(dir).Run(args...)
	return out, err
}

func defaultConfig() config.Config {
	c := config.Default()
	c.AutoPush = false // no remote in tests
	return c
}

func TestBumpRecordsAHandEditExactlyOnce(t *testing.T) {
	s := newStore(t)
	old := fixedNow.Add(-48 * time.Hour)
	ref := add(t, s, store.Global, "aaaaaaa1", "Hand edited", old, nil)
	add(t, s, store.Global, "aaaaaaa2", "Untouched", old, nil)

	handEdit(t, ref.Path)

	res, err := Run(Options{Store: s, Config: defaultConfig(), Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Bumped) != 1 || res.Bumped[0] != "aaaaaaa1" {
		t.Fatalf("Bumped = %v, want just the hand-edited item", res.Bumped)
	}

	it, err := s.Load(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Updated.Equal(fixedNow.Truncate(time.Second)) {
		t.Errorf("updated = %v, want the run's clock %v", it.Updated, fixedNow)
	}

	// A second run must find nothing: td's own write pinned mtime to updated.
	res, err = Run(Options{Store: s, Config: defaultConfig(), Now: at(fixedNow.Add(time.Hour))})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(res.Bumped) != 0 {
		t.Errorf("second run bumped %v, want nothing re-bumped", res.Bumped)
	}
}

func TestBumpLeavesTdsOwnWritesAlone(t *testing.T) {
	s := newStore(t)
	add(t, s, store.Global, "aaaaaaa1", "Just written", fixedNow, nil)

	res, err := Run(Options{Store: s, Config: defaultConfig(), Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Bumped) != 0 {
		t.Errorf("Bumped = %v, want nothing: td wrote the file itself", res.Bumped)
	}
}

func TestBumpAcrossScopes(t *testing.T) {
	s := newStore(t)
	old := fixedNow.Add(-48 * time.Hour)
	a := add(t, s, store.Global, "aaaaaaa1", "Global", old, nil)
	b := add(t, s, store.Scope("td"), "bbbbbbb1", "Project", old, nil)
	handEdit(t, a.Path)
	handEdit(t, b.Path)

	res, err := Run(Options{Store: s, Config: defaultConfig(), Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Bumped) != 2 {
		t.Errorf("Bumped = %v, want both scopes caught up", res.Bumped)
	}
}

func TestArchiveSweepTTLBoundary(t *testing.T) {
	cfg := defaultConfig()
	cfg.DoneTTLDays = 7

	tests := []struct {
		name        string
		doneDaysAgo int
		wantArchive bool
	}{
		{"done 8 days ago", 8, true},
		{"done 6 days ago", 6, false},
		{"done exactly 7 days ago", 7, false},
		{"done just now", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			doneAt := fixedNow.Add(-time.Duration(tc.doneDaysAgo) * 24 * time.Hour)
			add(t, s, store.Global, "aaaaaaa1", "Done item", fixedNow.Add(-240*time.Hour), &doneAt)

			res, err := Run(Options{Store: s, Config: cfg, Now: at(fixedNow)})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			archived := len(res.Archived) == 1
			if archived != tc.wantArchive {
				t.Fatalf("Archived = %v, want archived=%v", res.Archived, tc.wantArchive)
			}

			inArchive, err := s.List(store.Global, store.Archived)
			if err != nil {
				t.Fatal(err)
			}
			inList, err := s.List(store.Global)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantArchive {
				if len(inArchive) != 1 || len(inList) != 0 {
					t.Errorf("item is in %d archived / %d live, want 1/0", len(inArchive), len(inList))
				}
			} else if len(inArchive) != 0 || len(inList) != 1 {
				t.Errorf("item is in %d archived / %d live, want 0/1", len(inArchive), len(inList))
			}
		})
	}
}

func TestArchiveSweepLeavesOpenItemsAlone(t *testing.T) {
	s := newStore(t)
	add(t, s, store.Global, "aaaaaaa1", "Still open", fixedNow.Add(-240*time.Hour), nil)

	res, err := Run(Options{Store: s, Config: defaultConfig(), Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Archived) != 0 {
		t.Errorf("Archived = %v, want an open item left in the list however old", res.Archived)
	}
}

func TestArchiveSweepSkippedWhenNotRecoverable(t *testing.T) {
	s := newStore(t)
	cfg := defaultConfig()
	cfg.AutoCommit = false

	doneAt := fixedNow.Add(-30 * 24 * time.Hour)
	add(t, s, store.Global, "aaaaaaa1", "Long done", fixedNow.Add(-240*time.Hour), &doneAt)

	// The store is dirty and nothing will commit: the sweep must stand down.
	res, err := Run(Options{Store: s, Config: cfg, Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Archived) != 0 {
		t.Errorf("Archived = %v, want the sweep skipped", res.Archived)
	}
	if len(res.Warnings) == 0 {
		t.Error("the skipped sweep was not reported as a warning")
	}

	// With auto_commit back on, the same store sweeps and commits.
	res, err = Run(Options{Store: s, Config: defaultConfig(), Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Archived) != 1 {
		t.Errorf("Archived = %v, want the item swept once committing is on", res.Archived)
	}
}

func TestBumpFeedsTheSweepInOneRun(t *testing.T) {
	// An item hand-edited into the done state long ago must be bumped and then
	// seen by the sweep in the same run.
	s := newStore(t)
	ref := add(t, s, store.Global, "aaaaaaa1", "Marked done by hand", fixedNow.Add(-240*time.Hour), nil)

	b, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(b), "done_at:", "done_at: 2026-08-01T00:00:00Z", 1)
	if err := os.WriteFile(ref.Path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(ref.Path, time.Time{}, fixedNow.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	res, err := Run(Options{Store: s, Config: defaultConfig(), Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Bumped) != 1 {
		t.Errorf("Bumped = %v, want the hand edit recorded", res.Bumped)
	}
	if len(res.Archived) != 1 {
		t.Errorf("Archived = %v, want the same run to sweep it", res.Archived)
	}
}

func TestCommitLeavesExactlyOneCommit(t *testing.T) {
	s := newStore(t)
	add(t, s, store.Global, "aaaaaaa1", "One", fixedNow, nil)

	res, err := Run(Options{Store: s, Config: defaultConfig(), Message: "td: add One", Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Committed {
		t.Fatal("Committed = false, want the new item committed")
	}
	if got := commitCount(t, s.Repo()); got != 1 {
		t.Errorf("commit count = %d, want exactly 1", got)
	}
	subject, err := run(t, s.Root(), "log", "-1", "--pretty=%s")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(subject) != "td: add One" {
		t.Errorf("commit subject = %q, want the message passed in", strings.TrimSpace(subject))
	}

	// A run that changes nothing must not add a commit.
	res, err = Run(Options{Store: s, Config: defaultConfig(), Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.Committed {
		t.Error("Committed = true on a clean store")
	}
	if got := commitCount(t, s.Repo()); got != 1 {
		t.Errorf("commit count = %d, want still 1", got)
	}
}

func TestCommitSkippedWhenAutoCommitOff(t *testing.T) {
	s := newStore(t)
	cfg := defaultConfig()
	cfg.AutoCommit = false
	add(t, s, store.Global, "aaaaaaa1", "One", fixedNow, nil)

	res, err := Run(Options{Store: s, Config: cfg, Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Committed {
		t.Error("Committed = true with auto_commit off")
	}
	if got := commitCount(t, s.Repo()); got != 0 {
		t.Errorf("commit count = %d, want 0", got)
	}
}

func TestDefaultCommitMessageDescribesTheRun(t *testing.T) {
	tests := []struct {
		name string
		res  Result
		want string
	}{
		{"nothing", Result{}, "td: sync"},
		{"one bump", Result{Bumped: []string{"a"}}, "td: record 1 hand edit"},
		{"two bumps", Result{Bumped: []string{"a", "b"}}, "td: record 2 hand edits"},
		{"one archive", Result{Archived: []string{"a"}}, "td: archive 1 item"},
		{"both", Result{Bumped: []string{"a"}, Archived: []string{"b", "c"}}, "td: record 1 hand edit, archive 2 items"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultMessage(tc.res); got != tc.want {
				t.Errorf("defaultMessage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStepsAreAddressable(t *testing.T) {
	s := newStore(t)
	old := fixedNow.Add(-240 * time.Hour)
	doneAt := fixedNow.Add(-30 * 24 * time.Hour)
	ref := add(t, s, store.Global, "aaaaaaa1", "Hand edited", old, nil)
	add(t, s, store.Global, "aaaaaaa2", "Long done", old, &doneAt)
	handEdit(t, ref.Path)

	// Bump only: no sweep, no commit.
	res, err := Run(Options{Store: s, Config: defaultConfig(), Steps: StepBump, Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run(StepBump): %v", err)
	}
	if len(res.Bumped) != 1 || len(res.Archived) != 0 || res.Committed {
		t.Errorf("StepBump did more than bump: %+v", res)
	}
	if got := commitCount(t, s.Repo()); got != 0 {
		t.Errorf("commit count = %d after StepBump, want 0", got)
	}

	// Archive only.
	res, err = Run(Options{Store: s, Config: defaultConfig(), Steps: StepArchive, Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run(StepArchive): %v", err)
	}
	if len(res.Archived) != 1 || len(res.Bumped) != 0 || res.Committed {
		t.Errorf("StepArchive did more than archive: %+v", res)
	}

	// Commit only.
	res, err = Run(Options{Store: s, Config: defaultConfig(), Steps: StepCommit, Message: "td: manual", Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run(StepCommit): %v", err)
	}
	if !res.Committed || len(res.Bumped) != 0 || len(res.Archived) != 0 {
		t.Errorf("StepCommit did more than commit: %+v", res)
	}
	if got := commitCount(t, s.Repo()); got != 1 {
		t.Errorf("commit count = %d, want 1", got)
	}
}

// TestPushWithNoRemoteReportsNoPush: auto_push on a store that has no remote
// is not a failure and not a push. Reporting it as a push is a claim about a
// remote that does not exist, which is what --json and the pane would print.
func TestPushWithNoRemoteReportsNoPush(t *testing.T) {
	s := newStore(t)
	cfg := defaultConfig()
	cfg.AutoPush = true
	add(t, s, store.Global, "aaaaaaa1", "One", fixedNow, nil)

	res, err := Run(Options{Store: s, Config: cfg, Message: "td: add One", Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Pushed {
		t.Error("Pushed = true for a store with no remote")
	}
	if !res.Committed {
		t.Error("Committed = false, want the commit to have happened")
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none: having no remote is not a failure", res.Warnings)
	}
}

// TestPushToARemoteReportsAPush is the other half: a reachable remote gives
// Pushed true, so the field distinguishes the two cases rather than being
// constantly true.
func TestPushToARemoteReportsAPush(t *testing.T) {
	s := newStore(t)
	cfg := defaultConfig()
	cfg.AutoPush = true

	bare := filepath.Join(t.TempDir(), "remote.git")
	if _, err := run(t, t.TempDir(), "init", "--bare", "--quiet", bare); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, s.Root(), "remote", "add", "origin", bare); err != nil {
		t.Fatal(err)
	}
	add(t, s, store.Global, "aaaaaaa1", "One", fixedNow, nil)

	res, err := Run(Options{Store: s, Config: cfg, Message: "td: add One", Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Pushed {
		t.Errorf("Pushed = false against a reachable remote, warnings %v", res.Warnings)
	}
	if got := commitCount(t, gitx.New(bare)); got != 1 {
		t.Errorf("the remote holds %d commits, want 1", got)
	}
}

func TestPushWarnsRatherThanFails(t *testing.T) {
	s := newStore(t)
	cfg := defaultConfig()
	cfg.AutoPush = true
	if _, err := run(t, s.Root(), "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git")); err != nil {
		t.Fatal(err)
	}
	add(t, s, store.Global, "aaaaaaa1", "One", fixedNow, nil)

	res, err := Run(Options{Store: s, Config: cfg, Message: "td: add One", Now: at(fixedNow)})
	if err != nil {
		t.Fatalf("Run returned an error for a failed push: %v", err)
	}
	if res.Pushed {
		t.Error("Pushed = true against a missing remote")
	}
	if !res.Committed {
		t.Error("a failed push must not undo the commit")
	}
	if len(res.Warnings) == 0 {
		t.Error("the failed push was not reported as a warning")
	}
}

func TestPushSkippedWhenStepNotSelected(t *testing.T) {
	s := newStore(t)
	cfg := defaultConfig()
	cfg.AutoPush = true

	bare := filepath.Join(t.TempDir(), "remote.git")
	if _, err := run(t, t.TempDir(), "init", "--bare", "--quiet", bare); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, s.Root(), "remote", "add", "origin", bare); err != nil {
		t.Fatal(err)
	}
	add(t, s, store.Global, "aaaaaaa1", "One", fixedNow, nil)

	// This is what td ls does: everything but the push.
	res, err := Run(Options{
		Store:   s,
		Config:  cfg,
		Steps:   StepBump | StepArchive | StepCommit,
		Message: "td: sync",
		Now:     at(fixedNow),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Pushed {
		t.Error("Pushed = true with StepPush deselected")
	}
	if got := commitCount(t, gitx.New(bare)); got != 0 {
		t.Errorf("the remote moved: %d commits, want 0", got)
	}
}

func TestRunBlocksOnTheLock(t *testing.T) {
	s := newStore(t)
	add(t, s, store.Global, "aaaaaaa1", "One", fixedNow, nil)

	held, err := acquireLock(s.LockPath())
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := Run(Options{Store: s, Config: defaultConfig(), Message: "td: add One", Now: at(fixedNow)})
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Run completed while the lock was held: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Blocked, as it must be.
	}

	if err := held.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after the lock was released: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not proceed after the lock was released")
	}
	if got := commitCount(t, s.Repo()); got != 1 {
		t.Errorf("commit count = %d, want 1", got)
	}
}

func TestLockFileIsNotCommitted(t *testing.T) {
	s := newStore(t)
	add(t, s, store.Global, "aaaaaaa1", "One", fixedNow, nil)
	if _, err := Run(Options{Store: s, Config: defaultConfig(), Message: "td: add One", Now: at(fixedNow)}); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, s.Root(), "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, store.LockName) {
		t.Errorf("the lock file was committed:\n%s", out)
	}
}

func TestRunRequiresAStore(t *testing.T) {
	if _, err := Run(Options{}); err == nil {
		t.Error("Run with no store = nil, want an error")
	}
}
