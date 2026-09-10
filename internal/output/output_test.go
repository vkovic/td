package output

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func newTest(asJSON bool) (*Printer, *bytes.Buffer, *bytes.Buffer) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return New(out, errOut, asJSON), out, errOut
}

func TestEmitHuman(t *testing.T) {
	p, out, _ := newTest(false)
	err := p.Emit(map[string]string{"id": "7k3m9q2x"}, func(w io.Writer) error {
		_, err := io.WriteString(w, "7k3m9q2x  Wire the epilogue\n")
		return err
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if got := out.String(); got != "7k3m9q2x  Wire the epilogue\n" {
		t.Errorf("output = %q, want the human rendering", got)
	}
}

func TestEmitJSON(t *testing.T) {
	p, out, _ := newTest(true)
	err := p.Emit(map[string]string{"id": "7k3m9q2x"}, func(w io.Writer) error {
		_, err := io.WriteString(w, "this must not be written\n")
		return err
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"id": "7k3m9q2x"`) {
		t.Errorf("output = %q, want indented JSON", got)
	}
	if strings.Contains(got, "must not be written") {
		t.Error("the human renderer ran under --json")
	}
	if !strings.HasSuffix(got, "\n") {
		t.Error("JSON output does not end in a newline")
	}
}

func TestEmitJSONDoesNotEscapeHTML(t *testing.T) {
	p, out, _ := newTest(true)
	if err := p.Emit(map[string]string{"title": "a & b <c>"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "a & b <c>") {
		t.Errorf("output = %q, want the title unescaped", out.String())
	}
}

func TestPrintfIsSilentUnderJSON(t *testing.T) {
	p, out, _ := newTest(true)
	p.Printf("linked %s\n", "td")
	if out.Len() != 0 {
		t.Errorf("Printf wrote %q under --json, want nothing", out.String())
	}

	p, out, _ = newTest(false)
	p.Printf("linked %s\n", "td")
	if got := out.String(); got != "linked td\n" {
		t.Errorf("output = %q", got)
	}
}

func TestWarnGoesToStderrInBothModes(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		p, out, errOut := newTest(asJSON)
		p.Warn(errors.New("push failed: no route to host"))
		if out.Len() != 0 {
			t.Errorf("json=%v: a warning reached standard output: %q", asJSON, out.String())
		}
		if !strings.Contains(errOut.String(), "td: warning: push failed") {
			t.Errorf("json=%v: stderr = %q", asJSON, errOut.String())
		}
	}
}

func TestWarnSplitsJoinedErrors(t *testing.T) {
	p, _, errOut := newTest(false)
	p.Warn(errors.Join(errors.New("first"), errors.New("second")))
	lines := strings.Count(errOut.String(), "td: warning:")
	if lines != 2 {
		t.Errorf("printed %d warning lines, want one per joined error:\n%s", lines, errOut.String())
	}
}

func TestWarnIgnoresNil(t *testing.T) {
	p, _, errOut := newTest(false)
	p.Warn(nil)
	p.Warnings(nil)
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", errOut.String())
	}
}

func TestTableAligns(t *testing.T) {
	var buf bytes.Buffer
	table := Table{
		Header: []string{"ID", "TITLE"},
		Rows: [][]string{
			{"7k3m9q2x", "Wire the epilogue"},
			{"01hx2b9f", "Buy milk"},
		},
	}
	if err := table.Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("wrote %d lines, want a header and two rows:\n%s", len(lines), buf.String())
	}
	col := strings.Index(lines[0], "TITLE")
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line[col:], "Wire") && !strings.HasPrefix(line[col:], "Buy") {
			t.Errorf("column is not aligned at %d:\n%s", col, buf.String())
		}
	}
}

func TestEmptyTableWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	if err := (Table{Header: []string{"ID", "TITLE"}}).Write(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("an empty table wrote %q, want nothing rather than a bare header", buf.String())
	}
}
