package main

import (
	"fmt"
	"io"

	"github.com/vkovic/td/internal/epilogue"
	"github.com/vkovic/td/internal/store"
)

// resolveAll turns id prefixes into entries, failing before anything is written
// if any of them does not resolve. A command that touches several items must
// not half-apply itself because the last id was a typo.
func (a *app) resolveAll(prefixes []string, areas ...store.Area) ([]store.Entry, error) {
	entries := make([]store.Entry, 0, len(prefixes))
	seen := make(map[string]bool, len(prefixes))
	for _, prefix := range prefixes {
		e, err := a.store.Resolve(prefix, a.scope.Scope, areas...)
		if err != nil {
			return nil, err
		}
		if seen[e.Item.ID] {
			continue // the same item named twice is not an error, just once
		}
		seen[e.Item.ID] = true
		entries = append(entries, e)
	}
	return entries, nil
}

// finish runs the full epilogue after a change and reports the result. Every
// mutating command ends here, so all of them commit and push the same way.
func (a *app) finish(action, message string, entries []store.Entry) error {
	res, err := a.runEpilogue(epilogue.AllSteps, message)
	if err != nil {
		return err
	}
	out := mutationResult{
		Action:   action,
		Items:    newItemViews(entries, action != "remove"),
		Epilogue: newEpilogueView(res),
	}
	return a.out.Emit(out, func(w io.Writer) error {
		for _, e := range entries {
			if _, err := fmt.Fprintf(w, "%s  %s  %s\n", e.Item.ID, action, e.Item.Title); err != nil {
				return err
			}
		}
		return nil
	})
}

// commitMessage describes a change for git: the action, and the item it acted
// on when there was exactly one.
func commitMessage(action string, entries []store.Entry) string {
	if len(entries) == 1 {
		return fmt.Sprintf("td: %s %s %s", action, entries[0].Item.ID, entries[0].Item.Title)
	}
	return fmt.Sprintf("td: %s %d items", action, len(entries))
}
