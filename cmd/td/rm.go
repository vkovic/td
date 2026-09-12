package main

import (
	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/task"
)

// newRemoveCmd builds td rm.
func newRemoveCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id>...",
		Aliases: []string{"remove"},
		Short:   "Move items to the trash",
		Long: "rm moves each item into deleted/, which is gitignored and never purged.\n" +
			"Nothing is erased, and td restore brings an item back.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.remove(args)
		},
	}
}

// remove moves items to the trash. The service reports the action as "remove"
// rather than "rm", which is what --json carries and what the /td plugin reads.
func (a *app) remove(prefixes []string) error {
	res, err := a.tasks.Remove(task.MoveRequest{Scope: a.scope.Scope, IDs: prefixes})
	if err != nil {
		return err
	}
	return a.finish(res)
}

// newRestoreCmd builds td restore.
func newRestoreCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <id>...",
		Short: "Bring items back from the trash or the archive",
		Long:  "restore returns each item from deleted/ or archived/ to the live list.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.restore(args)
		},
	}
}

// restore returns items to the live list, from the trash or from the archive.
func (a *app) restore(prefixes []string) error {
	res, err := a.tasks.Restore(task.MoveRequest{Scope: a.scope.Scope, IDs: prefixes})
	if err != nil {
		return err
	}
	return a.finish(res)
}
