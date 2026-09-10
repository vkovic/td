package main

import (
	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/store"
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

// setDone stamps or clears done_at on each item. Reopening looks in the archive
// too, so an item swept out can be brought back into the list by undoing it.
func (a *app) setDone(prefixes []string, done bool) error {
	areas := []store.Area{store.Active}
	action := "done"
	if !done {
		areas = append(areas, store.Archived)
		action = "undo"
	}

	entries, err := a.resolveAll(prefixes, areas...)
	if err != nil {
		return err
	}

	stamp := now()
	for i, e := range entries {
		it := e.Item
		if done {
			at := stamp
			it.DoneAt = &at
		} else {
			it.DoneAt = nil
		}
		it.Updated = stamp

		// An item reopened out of the archive belongs back in the list.
		if !done && e.Ref.Area == store.Archived {
			ref, err := a.store.Move(e.Ref, it, e.Ref.Scope, store.Active)
			if err != nil {
				return err
			}
			entries[i].Ref = ref
			continue
		}
		if _, err := a.store.Save(e.Ref.Scope, e.Ref.Area, it); err != nil {
			return err
		}
	}
	return a.finish(action, commitMessage(action, entries), entries)
}
