package main

import (
	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/store"
	"github.com/vkovic/td/internal/task"
)

// editFlags are td edit's own flags. Which of them were actually typed decides
// what changes, so an unset flag never blanks a field.
type editFlags struct {
	title    string
	body     string
	bodyFile string
	appendTo string
	tags     []string
	due      string
}

// newEditCmd builds td edit.
func newEditCmd(a *app) *cobra.Command {
	var f editFlags
	cmd := &cobra.Command{
		Use:   "edit <id>...",
		Short: "Change an item's title, body, tags, or due date",
		Long: "edit changes the fields you name and leaves the rest alone. Ids may be\n" +
			"given as any unique prefix.\n\n" +
			"--tag replaces the item's tags rather than adding to them, and --due with\n" +
			"an empty value clears the due date.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.edit(args, f, cmd.Flags().Changed)
		},
	}
	cmd.Flags().StringVar(&f.title, "title", "", "replace the title")
	cmd.Flags().StringVarP(&f.body, "body", "b", "", "replace the body")
	cmd.Flags().StringVar(&f.bodyFile, "body-file", "", "replace the body from a file, or - for standard input")
	cmd.Flags().StringVar(&f.appendTo, "append", "", "append a paragraph to the body")
	cmd.Flags().StringArrayVarP(&f.tags, "tag", "t", nil, "replace the tags, repeatable")
	cmd.Flags().StringVar(&f.due, "due", "", "set the due date as YYYY-MM-DD, or empty to clear it")
	return cmd
}

// edit applies the named changes to every item given.
func (a *app) edit(prefixes []string, f editFlags, changed func(string) bool) error {
	if f.body != "" && f.bodyFile != "" {
		return usagef("use either --body or --body-file, not both")
	}
	if (changed("body") || changed("body-file")) && changed("append") {
		return usagef("use either a replacement body or --append, not both")
	}
	if !changed("title") && !changed("body") && !changed("body-file") &&
		!changed("append") && !changed("tag") && !changed("due") {
		return usagef("edit needs something to change: --title, --body, --body-file, --append, --tag, or --due")
	}

	// Resolved before the body is read, so that an id that does not exist is
	// reported as one rather than pre-empted by an unreadable --body-file.
	entries, err := a.tasks.ResolveAll(prefixes, a.scope.Scope, store.Active)
	if err != nil {
		return err
	}

	ch, err := a.editChange(f, changed)
	if err != nil {
		return err
	}

	res, err := a.tasks.Edit(entries, ch)
	if err != nil {
		return err
	}
	return a.finish(res)
}

// editChange turns the flags that were actually typed into the change they
// describe. Which flags were typed is cobra's to know and nobody else's, so the
// question is answered here and the answer never leaves.
func (a *app) editChange(f editFlags, changed func(string) bool) (task.Change, error) {
	var ch task.Change
	if changed("title") {
		if f.title == "" {
			return ch, usagef("%v", task.ErrEmptyTitle)
		}
		ch.Title = &f.title
	}
	if changed("body") || changed("body-file") {
		body, err := readBody(f.body, f.bodyFile, a.stdin())
		if err != nil {
			return ch, err
		}
		ch.Body = &body
	}
	if changed("append") {
		ch.Append = &f.appendTo
	}
	if changed("tag") {
		tags := cleanTags(f.tags)
		ch.Tags = &tags
	}
	if changed("due") {
		if f.due == "" {
			ch.Due = task.ClearDue()
			return ch, nil
		}
		due, err := store.ParseDate(f.due)
		if err != nil {
			return ch, &usageError{err: err}
		}
		ch.Due = task.SetDue(due)
	}
	return ch, nil
}
