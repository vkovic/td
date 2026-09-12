package main

import (
	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/store"
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

// remove moves items to the trash.
func (a *app) remove(prefixes []string) error {
	entries, err := a.resolveAll(prefixes, store.Active, store.Archived)
	if err != nil {
		return err
	}

	stamp := a.now()
	for i, e := range entries {
		e.Item.Updated = stamp
		ref, err := a.store.Move(e.Ref, e.Item, e.Ref.Scope, store.Deleted)
		if err != nil {
			return err
		}
		entries[i].Ref = ref
	}
	return a.finish("remove", commitMessage("remove", entries), entries)
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

// restore returns items to the live list.
func (a *app) restore(prefixes []string) error {
	entries, err := a.resolveAll(prefixes, store.Deleted, store.Archived)
	if err != nil {
		return err
	}

	stamp := a.now()
	for i, e := range entries {
		e.Item.Updated = stamp
		ref, err := a.store.Move(e.Ref, e.Item, e.Ref.Scope, store.Active)
		if err != nil {
			return err
		}
		entries[i].Ref = ref
	}
	return a.finish("restore", commitMessage("restore", entries), entries)
}
