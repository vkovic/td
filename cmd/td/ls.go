package main

import (
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/epilogue"
	"github.com/vkovic/td/internal/output"
	"github.com/vkovic/td/internal/store"
)

// readSteps is the epilogue a read command runs: everything but the push.
// td ls --json is the /td plugin's hottest path, and a slow remote must not sit
// inside a Claude Code turn. Hand edits still get recorded and committed.
const readSteps = epilogue.StepBump | epilogue.StepArchive | epilogue.StepCommit

// lsFlags are td ls's own flags.
type lsFlags struct {
	done bool
	all  bool
	tags []string
}

// lsResult is what td ls reports, and the shape of its --json output.
type lsResult struct {
	Scope    string       `json:"scope"`
	Items    []itemView   `json:"items"`
	Epilogue epilogueView `json:"epilogue"`
}

// newLsCmd builds td ls.
func newLsCmd(a *app) *cobra.Command {
	var f lsFlags
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List the items in the current list",
		Long: "ls shows the open items in whichever list applies here, most recently\n" +
			"updated first. Done items are hidden until --done, and appear after the\n" +
			"open ones, most recently completed first.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ls(f)
		},
	}
	cmd.Flags().BoolVar(&f.done, "done", false, "include done items")
	cmd.Flags().BoolVar(&f.all, "all", false, "merge every scope, with a scope column")
	cmd.Flags().StringArrayVarP(&f.tags, "tag", "t", nil, "only items carrying every tag given, repeatable")
	return cmd
}

// ls lists items in the current scope, or across every scope with --all.
//
// The epilogue runs first, before anything is read. A hand edit only becomes
// visible once it has been bumped, and an item the sweep moves out must not
// still appear in the listing that reported the sweep.
func (a *app) ls(f lsFlags) error {
	res, err := a.runEpilogue(readSteps, "")
	if err != nil {
		return err
	}

	var (
		entries []store.Entry
		scope   = a.scope.Scope.String()
	)
	if f.all {
		scope = "all"
		entries, err = a.store.ListAll(store.Active)
	} else {
		entries, err = a.store.List(a.scope.Scope, store.Active)
	}
	// A file that will not parse is reported, not fatal: the rest of the list
	// is still worth showing, and td is how you would go fix it.
	if err != nil {
		if len(entries) == 0 {
			return err
		}
		a.out.Warn(err)
	}

	entries = filterItems(entries, f.done, cleanTags(f.tags))
	sortItems(entries)

	out := lsResult{
		Scope:    scope,
		Items:    newItemViews(entries, false),
		Epilogue: newEpilogueView(res),
	}
	return a.out.Emit(out, func(w io.Writer) error {
		return lsTable(entries, f).Write(w)
	})
}

// filterItems drops items the flags exclude: done items unless --done, and
// anything missing one of the tags asked for.
func filterItems(entries []store.Entry, withDone bool, tags []string) []store.Entry {
	kept := make([]store.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Item.Done() && !withDone {
			continue
		}
		if !hasEveryTag(e.Item.Tags, tags) {
			continue
		}
		kept = append(kept, e)
	}
	return kept
}

// hasEveryTag reports whether item carries all of want. Several -t flags narrow
// the list rather than widening it.
func hasEveryTag(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if strings.EqualFold(h, w) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// sortItems orders a listing: open items first, most recently updated first,
// with created breaking a tie; then done items, most recently completed first.
// Ids break any remaining tie, so the order is stable run to run.
//
// Timestamps are stored to the second, so two items touched in the same second
// fall through to the id, which is why the tiebreaks matter at all.
func sortItems(entries []store.Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].Item, entries[j].Item
		if a.Done() != b.Done() {
			return !a.Done() // open items come first
		}
		if a.Done() {
			if !a.DoneAt.Equal(*b.DoneAt) {
				return a.DoneAt.After(*b.DoneAt)
			}
		}
		if !a.Updated.Equal(b.Updated) {
			return a.Updated.After(b.Updated)
		}
		if !a.Created.Equal(b.Created) {
			return a.Created.After(b.Created)
		}
		// Ids encode creation time, so the newer id first keeps this consistent
		// with the created tiebreak above when both land in the same second.
		return a.ID > b.ID
	})
}

// lsTable renders a listing for a person. Columns that would be empty for every
// row are left out, so a list with no due dates does not carry a blank column.
func lsTable(entries []store.Entry, f lsFlags) output.Table {
	var (
		showScope = f.all
		showDone  = f.done
		showDue   bool
		showTags  bool
	)
	for _, e := range entries {
		if e.Item.Due != nil {
			showDue = true
		}
		if len(e.Item.Tags) > 0 {
			showTags = true
		}
	}

	table := output.Table{Header: []string{"ID"}}
	if showScope {
		table.Header = append(table.Header, "SCOPE")
	}
	if showDone {
		table.Header = append(table.Header, "STATUS")
	}
	if showDue {
		table.Header = append(table.Header, "DUE")
	}
	if showTags {
		table.Header = append(table.Header, "TAGS")
	}
	table.Header = append(table.Header, "TITLE")

	for _, e := range entries {
		row := []string{e.Item.ID}
		if showScope {
			row = append(row, e.Ref.Scope.String())
		}
		if showDone {
			status := ""
			if e.Item.Done() {
				status = "done"
			}
			row = append(row, status)
		}
		if showDue {
			due := ""
			if e.Item.Due != nil {
				due = e.Item.Due.String()
			}
			row = append(row, due)
		}
		if showTags {
			row = append(row, strings.Join(e.Item.Tags, ","))
		}
		table.Rows = append(table.Rows, append(row, e.Item.Title))
	}
	return table
}
