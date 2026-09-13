package main

import (
	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/task"
)

// newPinCmd builds td pin.
func newPinCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "pin <id>...",
		Short: "Pin items to the top of their section",
		Long: "pin puts each item at the head of its section: first among the open\n" +
			"items, and first among the done ones once it is finished. It does not\n" +
			"change updated, and pinning an item already pinned changes nothing.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.setPinned(args, true)
		},
	}
}

// newUnpinCmd builds td unpin.
func newUnpinCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <id>...",
		Short: "Unpin items",
		Long:  "unpin returns each item to its place by timestamp. Unpinning an item\nthat is not pinned changes nothing.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.setPinned(args, false)
		},
	}
}

// setPinned hands td pin and td unpin to the service, which owns what a pin
// means — including that it leaves updated alone.
func (a *app) setPinned(prefixes []string, pinned bool) error {
	res, err := a.tasks.SetPinned(task.PinRequest{
		Scope:  a.scope.Scope,
		IDs:    prefixes,
		Pinned: pinned,
	})
	if err != nil {
		return err
	}
	return a.finish(res)
}
