package main

import (
	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/task"
)

// newDoneCmd builds td done.
func newDoneCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "done <id>...",
		Short: "Mark items done",
		Long: "done stamps done_at on each item, which is what makes it done. The item\n" +
			"stays in the list until the archive sweep moves it out, done_ttl_days\n" +
			"after it was completed.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.setDone(args, true)
		},
	}
}

// newUndoCmd builds td undo.
func newUndoCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "undo <id>...",
		Short: "Reopen done items",
		Long:  "undo clears done_at, putting each item back in the open list.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.setDone(args, false)
		},
	}
}

// setDone hands td done and td undo to the service, which owns what completing
// an item means — including that reopening looks in the archive too, so an item
// already swept out comes back into the list.
func (a *app) setDone(prefixes []string, done bool) error {
	res, err := a.tasks.SetDone(task.StatusRequest{
		Scope: a.scope.Scope,
		IDs:   prefixes,
		Done:  done,
	})
	if err != nil {
		return err
	}
	return a.finish(res)
}
