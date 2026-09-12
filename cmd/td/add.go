package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/store"
	"github.com/vkovic/td/internal/task"
)

// addFlags are td add's own flags.
type addFlags struct {
	body     string
	bodyFile string
	tags     []string
	due      string
}

// newAddCmd builds td add.
func newAddCmd(a *app) *cobra.Command {
	var f addFlags
	cmd := &cobra.Command{
		Use:   "add <title>...",
		Short: "Add an item to the current list",
		Long: "add creates an item in whichever list applies here: the project named by\n" +
			"a .td marker, or the global list. The words after add become the title,\n" +
			"so quoting it is optional.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.add(strings.Join(args, " "), f)
		},
	}
	cmd.Flags().StringVarP(&f.body, "body", "b", "", "markdown body for the item")
	cmd.Flags().StringVar(&f.bodyFile, "body-file", "", "read the body from a file, or - for standard input")
	cmd.Flags().StringArrayVarP(&f.tags, "tag", "t", nil, "tag the item, repeatable")
	cmd.Flags().StringVar(&f.due, "due", "", "due date, as YYYY-MM-DD")
	return cmd
}

// add creates one item and commits it.
//
// The title is checked here as well as in the service so that the complaint a
// person sees is the first thing wrong with what they typed: an add with no
// title and a bad --body-file has two faults, and the missing title is the one
// worth reporting. The message is the service's either way.
func (a *app) add(title string, f addFlags) error {
	if strings.TrimSpace(title) == "" {
		return usagef("%v", task.ErrEmptyTitle)
	}
	if f.body != "" && f.bodyFile != "" {
		return usagef("use either --body or --body-file, not both")
	}

	body, err := readBody(f.body, f.bodyFile, a.stdin())
	if err != nil {
		return err
	}

	p := a.provenance()
	req := task.AddRequest{
		Scope:       a.scope.Scope,
		Title:       title,
		Tags:        cleanTags(f.tags),
		Body:        body,
		Source:      p.Source,
		SessionName: p.SessionName,
		SessionID:   p.SessionID,
	}
	if f.due != "" {
		due, err := store.ParseDate(f.due)
		if err != nil {
			return &usageError{err: err}
		}
		req.Due = &due
	}

	res, err := a.tasks.Add(req)
	if err != nil {
		return err
	}
	return a.finish(res)
}

// readBody resolves the body from the flag, a file, or standard input.
func readBody(body, bodyFile string, stdin io.Reader) (string, error) {
	if bodyFile == "" {
		return normalizeBodyText(body), nil
	}
	var (
		b   []byte
		err error
	)
	if bodyFile == "-" {
		b, err = io.ReadAll(stdin)
	} else {
		b, err = os.ReadFile(bodyFile)
	}
	if err != nil {
		return "", fmt.Errorf("reading the item body: %w", err)
	}
	return normalizeBodyText(string(b)), nil
}

// normalizeBodyText trims a body to the shape an item file stores: no leading
// or trailing blank lines, and one closing newline when there is anything.
func normalizeBodyText(s string) string {
	s = strings.Trim(s, "\n")
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s + "\n"
}

// cleanTags trims and de-duplicates tags, keeping the order they were given.
func cleanTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		// A comma-separated -t is a natural thing to type, so accept it.
		for _, tag := range strings.Split(raw, ",") {
			tag = strings.TrimSpace(tag)
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			out = append(out, tag)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
