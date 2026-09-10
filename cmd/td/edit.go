package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/store"
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
		return fmt.Errorf("use either --body or --body-file, not both")
	}
	if (changed("body") || changed("body-file")) && changed("append") {
		return fmt.Errorf("use either a replacement body or --append, not both")
	}
	if !changed("title") && !changed("body") && !changed("body-file") &&
		!changed("append") && !changed("tag") && !changed("due") {
		return fmt.Errorf("edit needs something to change: --title, --body, --body-file, --append, --tag, or --due")
	}

	entries, err := a.resolveAll(prefixes, store.Active)
	if err != nil {
		return err
	}

	var body string
	if changed("body") || changed("body-file") {
		if body, err = readBody(f.body, f.bodyFile, a.stdin()); err != nil {
			return err
		}
	}

	stamp := now()
	for _, e := range entries {
		it := e.Item
		if changed("title") {
			if f.title == "" {
				return fmt.Errorf("an item needs a title")
			}
			it.Title = f.title
		}
		if changed("body") || changed("body-file") {
			it.Body = body
		}
		if changed("append") {
			it.Body = appendParagraph(it.Body, f.appendTo)
		}
		if changed("tag") {
			it.Tags = cleanTags(f.tags)
		}
		if changed("due") {
			if f.due == "" {
				it.Due = nil
			} else {
				due, err := store.ParseDate(f.due)
				if err != nil {
					return err
				}
				it.Due = &due
			}
		}
		it.Updated = stamp
		if _, err := a.store.Save(e.Ref.Scope, e.Ref.Area, it); err != nil {
			return err
		}
	}
	return a.finish("edit", commitMessage("edit", entries), entries)
}

// appendParagraph adds text to a body, separated by a blank line so the result
// is still readable markdown.
func appendParagraph(body, text string) string {
	text = normalizeBodyText(text)
	if text == "" {
		return body
	}
	if body == "" {
		return text
	}
	return body + "\n" + text
}
