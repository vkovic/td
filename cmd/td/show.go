package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/store"
)

// showResult is what td show reports, and the shape of its --json output.
type showResult struct {
	Item     itemView     `json:"item"`
	Epilogue epilogueView `json:"epilogue"`
}

// newShowCmd builds td show.
func newShowCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print one item in full",
		Long: "show prints an item's fields and its markdown body. The id may be any\n" +
			"unique prefix, and the item may be live, archived, or in the trash.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.show(args[0])
		},
	}
}

// show prints one item, looking in every area so an archived or trashed item is
// still readable.
//
// The epilogue runs first, so a hand edit is reported as td will store it and
// an item the sweep just moved is found where it now lives.
func (a *app) show(prefix string) error {
	res, err := a.runEpilogue(readSteps, "")
	if err != nil {
		return err
	}

	e, err := a.store.Resolve(prefix, a.scope.Scope, store.Active, store.Archived, store.Deleted)
	if err != nil {
		return err
	}

	out := showResult{Item: newItemView(e, true), Epilogue: newEpilogueView(res)}
	return a.out.Emit(out, func(w io.Writer) error {
		return writeItem(w, e)
	})
}

// writeItem renders one item for a person: its fields, then its body.
func writeItem(w io.Writer, e store.Entry) error {
	it := e.Item
	fields := [][2]string{
		{"id", it.ID},
		{"title", it.Title},
		{"scope", e.Ref.Scope.String()},
		{"area", areaName(e.Ref.Area)},
	}
	if len(it.Tags) > 0 {
		fields = append(fields, [2]string{"tags", strings.Join(it.Tags, ", ")})
	}
	if it.Due != nil {
		fields = append(fields, [2]string{"due", it.Due.String()})
	}
	fields = append(fields,
		[2]string{"created", it.Created.Format("2006-01-02 15:04")},
		[2]string{"updated", it.Updated.Format("2006-01-02 15:04")},
	)
	if it.DoneAt != nil {
		fields = append(fields, [2]string{"done", it.DoneAt.Format("2006-01-02 15:04")})
	}
	for _, f := range []struct{ key, val string }{
		{"source", it.Source},
		{"context", it.Context},
		{"session", it.ClaudeSessionName},
		{"session id", it.ClaudeSessionID},
	} {
		if f.val != "" {
			fields = append(fields, [2]string{f.key, f.val})
		}
	}
	fields = append(fields, [2]string{"file", e.Ref.Path})

	for _, f := range fields {
		if _, err := fmt.Fprintf(w, "%-10s %s\n", f[0], f[1]); err != nil {
			return err
		}
	}
	if it.Body != "" {
		if _, err := fmt.Fprintf(w, "\n%s", it.Body); err != nil {
			return err
		}
	}
	return nil
}
