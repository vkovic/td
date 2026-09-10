package gitx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate keeps git from reading the developer's own configuration, so a test
// asserts on td's behavior and not on whatever is set in ~/.gitconfig.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-system"))
	t.Setenv("GIT_AUTHOR_NAME", "td test")
	t.Setenv("GIT_AUTHOR_EMAIL", "td@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "td test")
	t.Setenv("GIT_COMMITTER_EMAIL", "td@example.invalid")
}

// newRepo returns an initialized repository in a temp directory.
func newRepo(t *testing.T) *Repo {
	t.Helper()
	isolate(t)
	r := New(t.TempDir())
	if err := r.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return r
}

// write puts a file in the repository.
func write(t *testing.T, r *Repo, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Root(), name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitCount is how many commits the repository holds.
func commitCount(t *testing.T, r *Repo) int {
	t.Helper()
	has, err := r.HasCommits()
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		return 0
	}
	out, err := r.run("rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range strings.TrimSpace(out) {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestInitCreatesRepoAndIsIdempotent(t *testing.T) {
	isolate(t)
	r := New(t.TempDir())

	exists, err := r.Exists()
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("Exists() = true before Init")
	}
	if err := r.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.Root(), ".git")); err != nil {
		t.Errorf("Init did not create .git: %v", err)
	}

	// A second Init must leave the repository alone.
	write(t, r, "a.md", "x")
	if _, err := r.CommitAll("first"); err != nil {
		t.Fatal(err)
	}
	if err := r.Init(); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if got := commitCount(t, r); got != 1 {
		t.Errorf("commit count after a second Init = %d, want 1", got)
	}
}

func TestHasCommitsOnAFreshRepo(t *testing.T) {
	r := newRepo(t)
	has, err := r.HasCommits()
	if err != nil {
		t.Fatalf("HasCommits on a fresh repo: %v", err)
	}
	if has {
		t.Error("HasCommits() = true on a repository with no commits")
	}
}

func TestIsDirty(t *testing.T) {
	r := newRepo(t)
	dirty, err := r.IsDirty()
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("IsDirty() = true on an empty repository")
	}

	write(t, r, "a.md", "x")
	dirty, err = r.IsDirty()
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Error("IsDirty() = false with an untracked file present")
	}

	if _, err := r.CommitAll("add a"); err != nil {
		t.Fatal(err)
	}
	dirty, err = r.IsDirty()
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("IsDirty() = true right after a commit")
	}
}

func TestCommitAllWhenCleanIsANoOp(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.md", "x")
	if _, err := r.CommitAll("add a"); err != nil {
		t.Fatal(err)
	}

	committed, err := r.CommitAll("nothing changed")
	if err != nil {
		t.Fatalf("CommitAll on a clean tree: %v", err)
	}
	if committed {
		t.Error("CommitAll() = true on a clean tree, want no commit")
	}
	if got := commitCount(t, r); got != 1 {
		t.Errorf("commit count = %d, want 1", got)
	}
}

func TestCommitAllWhenDirtyMakesExactlyOneCommit(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.md", "x")
	write(t, r, "b.md", "y")

	committed, err := r.CommitAll("add two files")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	if !committed {
		t.Error("CommitAll() = false with a dirty tree")
	}
	if got := commitCount(t, r); got != 1 {
		t.Errorf("commit count = %d, want exactly 1", got)
	}

	out, err := r.run("log", "-1", "--pretty=%s")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "add two files" {
		t.Errorf("commit subject = %q, want the message passed in", strings.TrimSpace(out))
	}
}

func TestCommitAllRejectsAnEmptyMessage(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.md", "x")
	if _, err := r.CommitAll("   "); err == nil {
		t.Error("CommitAll with a blank message = nil, want an error")
	}
}

func TestHasRemote(t *testing.T) {
	r := newRepo(t)
	has, err := r.HasRemote()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("HasRemote() = true on a repository with no remote")
	}

	if _, err := r.run("remote", "add", "origin", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	has, err = r.HasRemote()
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("HasRemote() = false after adding origin")
	}
}

func TestPushWithNoRemoteIsANoOp(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.md", "x")
	if _, err := r.CommitAll("add a"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(); err != nil {
		t.Errorf("Push() with no remote = %v, want nil", err)
	}
}

func TestPushSetsUpstreamThenPushesAgain(t *testing.T) {
	r := newRepo(t)

	bare := filepath.Join(t.TempDir(), "remote.git")
	if _, err := New(filepath.Dir(bare)).run("init", "--bare", "--quiet", bare); err != nil {
		t.Fatal(err)
	}
	if _, err := r.run("remote", "add", "origin", bare); err != nil {
		t.Fatal(err)
	}

	write(t, r, "a.md", "x")
	if _, err := r.CommitAll("add a"); err != nil {
		t.Fatal(err)
	}
	// The first push has no upstream to follow and must set one.
	if err := r.Push(); err != nil {
		t.Fatalf("first Push: %v", err)
	}
	remote := New(bare)
	if got := commitCount(t, remote); got != 1 {
		t.Fatalf("the remote holds %d commits after the first push, want 1", got)
	}

	// The second push follows the upstream just configured.
	write(t, r, "b.md", "y")
	if _, err := r.CommitAll("add b"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(); err != nil {
		t.Fatalf("second Push: %v", err)
	}
	if got := commitCount(t, remote); got != 2 {
		t.Errorf("the remote holds %d commits after the second push, want 2", got)
	}
}

func TestPushFailureIsReported(t *testing.T) {
	r := newRepo(t)
	if _, err := r.run("remote", "add", "origin", filepath.Join(t.TempDir(), "does-not-exist.git")); err != nil {
		t.Fatal(err)
	}
	write(t, r, "a.md", "x")
	if _, err := r.CommitAll("add a"); err != nil {
		t.Fatal(err)
	}
	err := r.Push()
	if err == nil {
		t.Fatal("Push to a missing remote = nil, want an error for the caller to warn about")
	}
	var runErr *RunError
	if !errors.As(err, &runErr) {
		t.Errorf("Push error = %T, want a *RunError carrying git's output", err)
	}
}

func TestCurrentBranch(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.md", "x")
	if _, err := r.CommitAll("add a"); err != nil {
		t.Fatal(err)
	}
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch == "" || branch == "HEAD" {
		t.Errorf("CurrentBranch() = %q, want a branch name", branch)
	}
}

func TestRunErrorCarriesGitOutput(t *testing.T) {
	r := newRepo(t)
	_, err := r.run("checkout", "no-such-branch")
	if err == nil {
		t.Fatal("want an error")
	}
	var runErr *RunError
	if !errors.As(err, &runErr) {
		t.Fatalf("error = %T, want *RunError", err)
	}
	if !strings.Contains(runErr.Error(), "no-such-branch") {
		t.Errorf("error %q does not carry git's own message", runErr.Error())
	}
}
