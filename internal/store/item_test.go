package store

import (
	"strings"
	"testing"
	"time"
)

func TestItemParseFull(t *testing.T) {
	src := `---
id: 7k3m9q2x
title: Wire the epilogue
tags: [cli, epilogue]
due: 2026-09-30
created: 2026-09-01T08:00:00Z
updated: 2026-09-09T17:24:00Z
done_at: 2026-09-09T18:00:00Z
source: claude
context: td
claude_session_name: milestone-1
claude_session_id: 3a28d128-0d9c-43ee-b318-4079815fab1c
---

Body line one.

Body line two.
`
	it, err := ParseItem([]byte(src))
	if err != nil {
		t.Fatalf("ParseItem: %v", err)
	}
	if it.ID != "7k3m9q2x" {
		t.Errorf("ID = %q, want 7k3m9q2x", it.ID)
	}
	if it.Title != "Wire the epilogue" {
		t.Errorf("Title = %q", it.Title)
	}
	if got, want := strings.Join(it.Tags, ","), "cli,epilogue"; got != want {
		t.Errorf("Tags = %q, want %q", got, want)
	}
	if it.Due == nil || it.Due.String() != "2026-09-30" {
		t.Errorf("Due = %v, want 2026-09-30", it.Due)
	}
	if want := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC); !it.Created.Equal(want) {
		t.Errorf("Created = %v, want %v", it.Created, want)
	}
	if want := time.Date(2026, 9, 9, 17, 24, 0, 0, time.UTC); !it.Updated.Equal(want) {
		t.Errorf("Updated = %v, want %v", it.Updated, want)
	}
	if !it.Done() {
		t.Error("Done() = false, want true")
	}
	if want := time.Date(2026, 9, 9, 18, 0, 0, 0, time.UTC); !it.DoneAt.Equal(want) {
		t.Errorf("DoneAt = %v, want %v", it.DoneAt, want)
	}
	if it.Source != "claude" || it.Context != "td" {
		t.Errorf("Source/Context = %q/%q", it.Source, it.Context)
	}
	if it.ClaudeSessionName != "milestone-1" {
		t.Errorf("ClaudeSessionName = %q", it.ClaudeSessionName)
	}
	if it.ClaudeSessionID != "3a28d128-0d9c-43ee-b318-4079815fab1c" {
		t.Errorf("ClaudeSessionID = %q", it.ClaudeSessionID)
	}
	if want := "Body line one.\n\nBody line two.\n"; it.Body != want {
		t.Errorf("Body = %q, want %q", it.Body, want)
	}
}

func TestItemRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "every field",
			src: `---
id: 7k3m9q2x
title: Wire the epilogue
tags: [cli, epilogue]
due: "2026-09-30"
created: 2026-09-01T08:00:00Z
updated: 2026-09-09T17:24:00Z
done_at: 2026-09-09T18:00:00Z
source: claude
context: td
claude_session_name: milestone-1
claude_session_id: 3a28d128
---

The body.
`,
		},
		{
			name: "required fields only",
			src: `---
id: 01hx2b9f
title: Buy milk
tags: []
created: 2026-09-01T08:00:00Z
updated: 2026-09-01T08:00:00Z
done_at:
---
`,
		},
		{
			name: "unknown keys survive",
			src: `---
id: 01hx2b9f
title: Buy milk
tags: []
created: 2026-09-01T08:00:00Z
updated: 2026-09-01T08:00:00Z
done_at:
priority: high
estimate: 3
---

Note.
`,
		},
		{
			name: "title needing quotes",
			src: `---
id: 01hx2b9f
title: "true"
tags: [a]
created: 2026-09-01T08:00:00Z
updated: 2026-09-01T08:00:00Z
done_at:
---
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			it, err := ParseItem([]byte(tc.src))
			if err != nil {
				t.Fatalf("ParseItem: %v", err)
			}
			out, err := it.Marshal()
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(out) != tc.src {
				t.Errorf("round trip differs\n--- got ---\n%s\n--- want ---\n%s", out, tc.src)
			}
		})
	}
}

func TestItemUnknownKeysPreserved(t *testing.T) {
	src := `---
id: 01hx2b9f
title: Buy milk
tags: []
created: 2026-09-01T08:00:00Z
updated: 2026-09-01T08:00:00Z
done_at:
priority: high
estimate: 3
---
`
	it, err := ParseItem([]byte(src))
	if err != nil {
		t.Fatalf("ParseItem: %v", err)
	}
	if len(it.residue) != 4 {
		t.Fatalf("residue holds %d nodes, want 4 (two key/value pairs)", len(it.residue))
	}
	// A rewrite after a field change must still carry the unknown keys.
	it.Title = "Buy oat milk"
	out, err := it.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{"priority: high", "estimate: 3", "title: Buy oat milk"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rewrite dropped %q:\n%s", want, out)
		}
	}
}

func TestItemEmptyDoneAt(t *testing.T) {
	for _, src := range []string{
		"---\nid: a\ntitle: t\ndone_at:\n---\n",
		"---\nid: a\ntitle: t\ndone_at: null\n---\n",
		"---\nid: a\ntitle: t\n---\n",
	} {
		it, err := ParseItem([]byte(src))
		if err != nil {
			t.Fatalf("ParseItem(%q): %v", src, err)
		}
		if it.DoneAt != nil {
			t.Errorf("ParseItem(%q): DoneAt = %v, want nil", src, it.DoneAt)
		}
		if it.Done() {
			t.Errorf("ParseItem(%q): Done() = true, want false", src)
		}
	}
}

func TestItemParseErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want error
	}{
		{"no frontmatter", "Just a body.\n", ErrNoFrontmatter},
		{"unterminated", "---\nid: a\ntitle: t\n", ErrUnterminatedFrontmatter},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseItem([]byte(tc.src)); err != tc.want {
				t.Errorf("ParseItem error = %v, want %v", err, tc.want)
			}
		})
	}

	for _, src := range []string{
		"---\nid: a\ntitle: t\ndue: nope\n---\n",
		"---\nid: a\ntitle: t\ncreated: yesterday\n---\n",
		"---\n- not\n- a mapping\n---\n",
		"---\nid: [unclosed\n---\n",
	} {
		if _, err := ParseItem([]byte(src)); err == nil {
			t.Errorf("ParseItem(%q) = nil error, want a parse failure", src)
		}
	}
}

func TestItemBodyNormalization(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"no body", "---\nid: a\n---\n", ""},
		{"blank line only", "---\nid: a\n---\n\n\n", ""},
		{"extra trailing newlines", "---\nid: a\n---\n\nText.\n\n\n", "Text.\n"},
		{"no separating blank line", "---\nid: a\n---\nText.\n", "Text.\n"},
		{"crlf", "---\r\nid: a\r\n---\r\n\r\nText.\r\n", "Text.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			it, err := ParseItem([]byte(tc.src))
			if err != nil {
				t.Fatalf("ParseItem: %v", err)
			}
			if it.Body != tc.want {
				t.Errorf("Body = %q, want %q", it.Body, tc.want)
			}
		})
	}
}

func TestItemTimestampsWrittenInUTC(t *testing.T) {
	zone := time.FixedZone("CEST", 2*60*60)
	it := &Item{
		ID:      "a",
		Title:   "t",
		Created: time.Date(2026, 9, 1, 10, 0, 0, 0, zone),
		Updated: time.Date(2026, 9, 1, 10, 0, 0, 0, zone),
	}
	out, err := it.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), "created: 2026-09-01T08:00:00Z") {
		t.Errorf("timestamps not normalized to UTC:\n%s", out)
	}
}
