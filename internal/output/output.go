// Package output renders what a command produced, either as a table for a
// person or as JSON for a program. Every command goes through it, so --json is
// a property of the CLI rather than something each subcommand reinvents.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Printer writes a command's result. Warnings always go to the error stream, so
// standard output stays clean enough to pipe into jq even when something went
// sideways.
type Printer struct {
	Out  io.Writer
	Err  io.Writer
	JSON bool
}

// New returns a Printer writing to the given streams.
func New(out, errOut io.Writer, asJSON bool) *Printer {
	return &Printer{Out: out, Err: errOut, JSON: asJSON}
}

// Emit renders a result: value as JSON when --json is set, otherwise whatever
// human writes to the stream it is handed.
func (p *Printer) Emit(value any, human func(w io.Writer) error) error {
	if p.JSON {
		return p.emitJSON(value)
	}
	if human == nil {
		return nil
	}
	return human(p.Out)
}

// emitJSON writes one indented JSON document followed by a newline.
func (p *Printer) emitJSON(value any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return fmt.Errorf("writing JSON output: %w", err)
	}
	return nil
}

// Printf writes a line for a person. It does nothing under --json, so a
// progress message can sit next to a JSON result without corrupting it.
func (p *Printer) Printf(format string, args ...any) {
	if p.JSON {
		return
	}
	_, _ = fmt.Fprintf(p.Out, format, args...)
}

// Warn reports something that did not fail the command — a push that could not
// reach its remote, an item file that will not parse, a misspelled config key.
// It always goes to the error stream, under --json as much as without it.
func (p *Printer) Warn(err error) {
	if err == nil {
		return
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			_, _ = fmt.Fprintln(p.Err, "td: warning:", line)
		}
	}
}

// Warnings reports a batch of warnings.
func (p *Printer) Warnings(errs []error) {
	for _, err := range errs {
		p.Warn(err)
	}
}

// Table is a set of rows under a header, rendered in aligned columns.
type Table struct {
	Header []string
	Rows   [][]string
}

// Write renders the table with columns padded to line up. An empty table writes
// nothing at all, so a command with no results does not print a bare header.
func (t Table) Write(w io.Writer) error {
	if len(t.Rows) == 0 {
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if len(t.Header) > 0 {
		_, _ = fmt.Fprintln(tw, strings.Join(t.Header, "\t"))
	}
	for _, row := range t.Rows {
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}
