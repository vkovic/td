// Package gitx is td's only entry point to git. Every invocation runs against
// an explicit repository root with -C, so no command depends on the process's
// working directory.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrGitMissing is returned when git is not installed.
var ErrGitMissing = errors.New("git is not installed or not on $PATH")

// Repo is a git repository at a known root.
type Repo struct {
	root string
}

// New returns a handle on the repository rooted at dir. Nothing is run and the
// directory need not exist yet.
func New(dir string) *Repo {
	return &Repo{root: filepath.Clean(dir)}
}

// Root is the repository's directory.
func (r *Repo) Root() string { return r.root }

// Exists reports whether the root already holds a git repository.
func (r *Repo) Exists() (bool, error) {
	_, err := os.Stat(filepath.Join(r.root, ".git"))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("checking for a git repository in %s: %w", r.root, err)
}

// Init creates a repository at the root. It is a no-op when one is already
// there, so it is safe to call on every command.
func (r *Repo) Init() error {
	exists, err := r.Exists()
	if err != nil || exists {
		return err
	}
	_, err = r.run("init", "--quiet")
	return err
}

// IsDirty reports whether the working tree has changes git would commit,
// including untracked files.
func (r *Repo) IsDirty() (bool, error) {
	out, err := r.run("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// CommitAll stages everything in the repository and commits it, reporting
// whether a commit was actually made. A clean tree is not an error: it commits
// nothing and returns false, so a command that changed nothing stays quiet.
func (r *Repo) CommitAll(message string) (bool, error) {
	if strings.TrimSpace(message) == "" {
		return false, errors.New("cannot commit with an empty message")
	}
	dirty, err := r.IsDirty()
	if err != nil {
		return false, err
	}
	if !dirty {
		return false, nil
	}
	if _, err := r.run("add", "--all"); err != nil {
		return false, err
	}
	if _, err := r.run("commit", "--quiet", "--message", message); err != nil {
		return false, err
	}
	return true, nil
}

// HasRemote reports whether the repository has any remote configured. Without
// one there is nowhere to push and Push does nothing.
func (r *Repo) HasRemote() (bool, error) {
	out, err := r.run("remote")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Push sends the current branch to its remote, setting the upstream on the
// first push. A repository with no remote is a no-op and not an error.
//
// A push that fails is returned as an error, but callers must treat it as a
// warning: a store that could not reach its remote is still a correct store,
// and td must not fail a command over it.
func (r *Repo) Push() error {
	has, err := r.HasRemote()
	if err != nil || !has {
		return err
	}
	if upstream, err := r.hasUpstream(); err != nil {
		return err
	} else if upstream {
		_, err = r.run("push")
		return err
	}

	remote, err := r.defaultRemote()
	if err != nil {
		return err
	}
	branch, err := r.CurrentBranch()
	if err != nil {
		return err
	}
	_, err = r.run("push", "--set-upstream", remote, branch)
	return err
}

// CurrentBranch returns the checked out branch name.
func (r *Repo) CurrentBranch() (string, error) {
	out, err := r.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// HasCommits reports whether HEAD points at anything yet. A repository created
// but never committed to has no HEAD, which several git commands treat as an
// error rather than an empty result.
func (r *Repo) HasCommits() (bool, error) {
	if _, err := r.run("rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		var runErr *RunError
		if errors.As(err, &runErr) {
			return false, nil // no HEAD yet, which is a state and not a failure
		}
		return false, err
	}
	return true, nil
}

// hasUpstream reports whether the current branch tracks a remote branch.
func (r *Repo) hasUpstream() (bool, error) {
	if _, err := r.run("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err != nil {
		var runErr *RunError
		if errors.As(err, &runErr) {
			return false, nil // no upstream configured, which is not a failure
		}
		return false, err
	}
	return true, nil
}

// defaultRemote picks the remote to push to: origin when it exists, otherwise
// the first one configured.
func (r *Repo) defaultRemote() (string, error) {
	out, err := r.run("remote")
	if err != nil {
		return "", err
	}
	remotes := strings.Fields(out)
	if len(remotes) == 0 {
		return "", errors.New("the store has no git remote to push to")
	}
	for _, name := range remotes {
		if name == "origin" {
			return name, nil
		}
	}
	return remotes[0], nil
}

// RunError is a git command that ran and failed, carrying what git printed so
// the message a user sees is git's own.
type RunError struct {
	Args   []string
	Output string
	Err    error
}

func (e *RunError) Error() string {
	cmd := "git " + strings.Join(e.Args, " ")
	if e.Output == "" {
		return fmt.Sprintf("%s: %v", cmd, e.Err)
	}
	return fmt.Sprintf("%s: %v: %s", cmd, e.Err, e.Output)
}

func (e *RunError) Unwrap() error { return e.Err }

// run executes one git command against the repository root and returns its
// standard output.
func (r *Repo) run(args ...string) (string, error) {
	full := append([]string{"-C", r.root}, args...)
	cmd := exec.Command("git", full...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Never let git open an interactive prompt: a credential prompt inside a td
	// command would hang the terminal, and inside a Claude Code turn it would
	// hang with nothing to type into.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrGitMissing
		}
		return "", &RunError{
			Args:   args,
			Output: strings.TrimSpace(stderr.String() + stdout.String()),
			Err:    err,
		}
	}
	return stdout.String(), nil
}
