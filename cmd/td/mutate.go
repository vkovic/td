package main

import (
	"fmt"
	"io"

	"github.com/vkovic/td/internal/epilogue"
	"github.com/vkovic/td/internal/task"
)

// finish runs the full epilogue after a change and reports what happened. Every
// mutating command ends here, so all of them commit and push the same way.
func (a *app) finish(res task.Result) error {
	ep, err := a.runEpilogue(epilogue.AllSteps, res.Message)
	if err != nil {
		return err
	}
	out := mutationResult{
		Action: res.Action,
		// Bodies are withheld from a removal, and the test for that is the
		// action string rather than which command ran: td rm reports "remove".
		// Both the word and this branch are the /td plugin's documented
		// contract, so neither is an internal detail to tidy up.
		Items:    newItemViews(res.Entries, res.Action != "remove"),
		Epilogue: newEpilogueView(ep),
	}
	return a.out.Emit(out, func(w io.Writer) error {
		for _, e := range res.Entries {
			if _, err := fmt.Fprintf(w, "%s  %s  %s\n", e.Item.ID, res.Action, e.Item.Title); err != nil {
				return err
			}
		}
		return nil
	})
}
