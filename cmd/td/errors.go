package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/store"
)

// Exit codes. A caller — a shell script, or the /td plugin — can tell why a
// command failed without reading its message.
const (
	// exitOK means the command succeeded.
	exitOK = 0
	// exitFailure means the store or git could not do what was asked: a file
	// that will not parse, a directory that cannot be written, a failed commit.
	exitFailure = 1
	// exitUsage means the command line was wrong: an unknown flag, a missing
	// argument, contradictory flags.
	exitUsage = 2
	// exitNotFound means no item matched the id given.
	exitNotFound = 3
	// exitAmbiguous means an id prefix matched more than one item.
	exitAmbiguous = 4
)

// usageError marks a failure as the caller's mistake rather than the store's,
// so it exits with exitUsage and prints the command's usage.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// usagef builds a usage error.
func usagef(format string, a ...any) error {
	return &usageError{err: fmt.Errorf(format, a...)}
}

// exitCode classifies an error into the code td exits with.
func exitCode(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, store.ErrAmbiguous):
		return exitAmbiguous
	case errors.Is(err, store.ErrNotFound):
		return exitNotFound
	case isUsageError(err):
		return exitUsage
	default:
		return exitFailure
	}
}

// isUsageError reports whether err came from the command line rather than the
// store.
func isUsageError(err error) bool {
	var u *usageError
	if errors.As(err, &u) {
		return true
	}
	// Choosing two scopes at once is a contradiction in the flags, not a
	// failure of the store.
	return errors.Is(err, store.ErrScopeConflict)
}

// markUsageErrors makes cobra's own complaints — an unknown flag, the wrong
// number of arguments — carry the usage classification, so they exit with
// exitUsage like td's own argument checks do.
func markUsageErrors(cmd *cobra.Command) {
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err: err}
	})
	if args := cmd.Args; args != nil {
		cmd.Args = func(c *cobra.Command, a []string) error {
			if err := args(c, a); err != nil {
				return &usageError{err: err}
			}
			return nil
		}
	}
	for _, child := range cmd.Commands() {
		markUsageErrors(child)
	}
}
