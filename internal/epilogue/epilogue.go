// Package epilogue is the tail every td command runs: catch up on hand edits,
// sweep out items whose done TTL has elapsed, commit, and push. It holds an
// exclusive lock for the whole run, so a CLI command and the TUI can never race
// on the same files or on .git/index.
package epilogue

import (
	"errors"
	"fmt"
	"time"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/store"
)

// Step names one part of the epilogue. The maintenance commands — td bump, td
// archive, td commit, td push — each run just their own.
type Step uint8

const (
	// StepBump records edits made to item files outside td.
	StepBump Step = 1 << iota
	// StepArchive moves done items out of the list once their TTL has elapsed.
	StepArchive
	// StepCommit commits the store.
	StepCommit
	// StepPush pushes the store to its remote.
	StepPush
)

// AllSteps is the full epilogue, in the order Run performs it.
const AllSteps = StepBump | StepArchive | StepCommit | StepPush

// Has reports whether s includes step.
func (s Step) Has(step Step) bool { return s&step != 0 }

// Options configures one epilogue run.
type Options struct {
	// Store is the store to act on. Required.
	Store *store.Store
	// Config supplies done_ttl_days, auto_commit and auto_push.
	Config config.Config
	// Steps selects which parts to run. Zero means AllSteps.
	Steps Step
	// Message is the commit message. Empty means one describing what the run
	// did, which is what the maintenance commands want.
	Message string
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// Result reports what a run did, so a command can print it and --json can
// render it.
type Result struct {
	// Bumped holds the ids whose updated timestamp was caught up.
	Bumped []string
	// Archived holds the ids moved out of the list.
	Archived []string
	// Committed reports whether a commit was made.
	Committed bool
	// Message is the commit message used, when one was.
	Message string
	// Pushed reports whether a push reached a remote. A store with no remote
	// pushes nothing and reports false.
	Pushed bool
	// Warnings are failures that must not fail the command: a push that could
	// not reach its remote, an item file that will not parse, a sweep skipped
	// because the store was not in a recoverable state.
	Warnings []error
}

// Run performs the epilogue under the store's lock.
//
// The order is fixed: bump, then archive, then commit, then push. Bumping first
// means an item hand-edited into the done state is seen by the sweep in the
// same run, and committing after both means one command leaves one commit.
func Run(opts Options) (Result, error) {
	var res Result
	if opts.Store == nil {
		return res, errors.New("epilogue: no store given")
	}
	steps := opts.Steps
	if steps == 0 {
		steps = AllSteps
	}
	now := opts.Now
	if now == nil {
		// store.Now, not time.Now: this clock stamps as well as compares, and
		// an untruncated timestamp written into an item makes the file's mtime
		// outrun its own updated field — which is what HandEdited reads as an
		// edit made outside td. bump truncates again before stamping, so this
		// was safe rather than correct; the two callers already pass
		// second-truncated clocks and the default should not be the odd one.
		now = store.Now
	}

	lk, err := acquireLock(opts.Store.LockPath())
	if err != nil {
		return res, err
	}
	// A failed release is not worth failing the run over, and not worth
	// warning about either: the lock lives on the open descriptor, so the
	// process exiting drops it whatever this returns.
	defer func() { _ = lk.release() }()

	repo := opts.Store.Repo()

	// Whether the store was already dirty decides if the sweep may move files:
	// see sweep's comment.
	dirtyBefore, err := repo.IsDirty()
	if err != nil {
		return res, err
	}

	if steps.Has(StepBump) {
		if err := bump(opts.Store, now(), &res); err != nil {
			return res, err
		}
	}
	if steps.Has(StepArchive) {
		if err := sweep(opts.Store, opts.Config, now(), dirtyBefore, &res); err != nil {
			return res, err
		}
	}
	if steps.Has(StepCommit) && opts.Config.AutoCommit {
		message := opts.Message
		if message == "" {
			message = defaultMessage(res)
		}
		committed, err := repo.CommitAll(message)
		if err != nil {
			return res, err
		}
		res.Committed = committed
		if committed {
			res.Message = message
		}
	}
	if steps.Has(StepPush) && opts.Config.AutoPush {
		pushed, err := repo.Push()
		if err != nil {
			// A store that could not reach its remote is still a correct store.
			res.Warnings = append(res.Warnings, fmt.Errorf("push failed: %w", err))
		} else {
			res.Pushed = pushed
		}
	}
	return res, nil
}

// bump catches up the updated timestamp of every item whose file has been
// edited outside td, across every scope.
//
// Only live items are bumped. Archived and deleted items are out of the list,
// so re-dating them would rewrite files for no visible effect — and the fewer
// files this touches, the smaller the blast radius if it is ever wrong.
//
// Rewriting an item pins its file's mtime back to the new updated value, so the
// next run sees the two equal and does not bump it again.
func bump(s *store.Store, now time.Time, res *Result) error {
	entries, err := s.ListAll(store.Active)
	if err != nil {
		if len(entries) == 0 {
			return err
		}
		res.Warnings = append(res.Warnings, err)
	}
	for _, e := range entries {
		edited, err := store.HandEdited(e)
		if err != nil {
			return err
		}
		if !edited {
			continue
		}
		e.Item.Updated = now.UTC().Truncate(time.Second)
		if _, err := s.Save(e.Ref.Scope, e.Ref.Area, e.Item); err != nil {
			return fmt.Errorf("recording a hand edit to %s: %w", e.Ref.Path, err)
		}
		res.Bumped = append(res.Bumped, e.Item.ID)
	}
	return nil
}

// sweep moves done items out of the list once done_ttl_days has elapsed since
// they were completed.
//
// It runs only when the store's current state is recoverable: either
// auto_commit is on, so this run commits the moves and git can revert them, or
// the tree was clean before the run, so git restores it. A store that was
// already dirty with no commit coming is left alone and the skip is reported —
// this is the one part of the epilogue that moves files, and it must not do so
// on top of uncommitted work.
func sweep(s *store.Store, cfg config.Config, now time.Time, dirtyBefore bool, res *Result) error {
	if dirtyBefore && !cfg.AutoCommit {
		res.Warnings = append(res.Warnings, errors.New(
			"archive sweep skipped: the store has uncommitted changes and auto_commit is off"))
		return nil
	}

	entries, err := s.ListAll(store.Active)
	if err != nil {
		if len(entries) == 0 {
			return err
		}
		res.Warnings = append(res.Warnings, err)
	}
	ttl := time.Duration(cfg.DoneTTLDays) * 24 * time.Hour
	for _, e := range entries {
		if e.Item.DoneAt == nil {
			continue
		}
		if now.Sub(*e.Item.DoneAt) <= ttl {
			continue
		}
		if _, err := s.Move(e.Ref, e.Item, e.Ref.Scope, store.Archived); err != nil {
			return fmt.Errorf("archiving %s: %w", e.Ref.Path, err)
		}
		res.Archived = append(res.Archived, e.Item.ID)
	}
	return nil
}

// defaultMessage describes what a run did, for the commands that do not supply
// a message of their own.
func defaultMessage(res Result) string {
	bumped, archived := len(res.Bumped), len(res.Archived)
	switch {
	case bumped > 0 && archived > 0:
		return fmt.Sprintf("td: record %s, archive %s",
			plural(bumped, "hand edit", "hand edits"), plural(archived, "item", "items"))
	case bumped > 0:
		return "td: record " + plural(bumped, "hand edit", "hand edits")
	case archived > 0:
		return "td: archive " + plural(archived, "item", "items")
	default:
		return "td: sync"
	}
}

// plural renders a count with the right noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
