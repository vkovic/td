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
due: 2026-09-30
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
			// The shape a person types: no tags, no done_at, a bare due, and
			// priority between title and created. td rewrote all four the
			// first time it touched such a file.
			name: "a hand-written file",
			src: `---
id: 01hx2b9f
title: Buy milk
due: 2026-09-30
priority: high
created: 2026-09-01T08:00:00Z
updated: 2026-09-01T08:00:00Z
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

// TestHandWrittenFileGainsOnlyTheKeyItNeeds: setting a field whose key the
// file does not have adds that one line, in the place §7 puts it, and leaves
// every other line where it was. The file is the user's; td is a guest in it.
func TestHandWrittenFileGainsOnlyTheKeyItNeeds(t *testing.T) {
	src := `---
id: 01hx2b9f
title: Buy milk
due: 2026-09-30
priority: high
created: 2026-09-01T08:00:00Z
updated: 2026-09-01T08:00:00Z
---

Note.
`
	it, err := ParseItem([]byte(src))
	if err != nil {
		t.Fatalf("ParseItem: %v", err)
	}
	done := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	it.DoneAt = &done

	out, err := it.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `---
id: 01hx2b9f
title: Buy milk
due: 2026-09-30
priority: high
created: 2026-09-01T08:00:00Z
updated: 2026-09-01T08:00:00Z
done_at: 2026-09-11T09:00:00Z
---

Note.
`
	if string(out) != want {
		t.Errorf("marking a hand-written item done rewrote more than done_at\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

// TestTagsGainedByAHandWrittenFileLandInOrder: tags belongs between title and
// due, and the file has neither a tags line nor an appetite for its keys being
// shuffled.
func TestTagsGainedByAHandWrittenFileLandInOrder(t *testing.T) {
	it, err := ParseItem([]byte("---\nid: a\ntitle: t\ndue: 2026-09-30\nnotes: mine\n---\n"))
	if err != nil {
		t.Fatalf("ParseItem: %v", err)
	}
	it.Tags = []string{"errand"}

	out, err := it.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := "---\nid: a\ntitle: t\ntags: [errand]\ndue: 2026-09-30\nnotes: mine\n---\n"
	if string(out) != want {
		t.Errorf("tags landed in the wrong place\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

// TestItemTdCreatedStillCarriesTheSkeleton: an item td made has no file to
// take its shape from, so it gets the full §7 skeleton — including the empty
// tags and done_at lines that show a person what the file can hold.
func TestItemTdCreatedStillCarriesTheSkeleton(t *testing.T) {
	it := &Item{
		ID:      "01hx2b9f",
		Title:   "Buy milk",
		Created: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
		Updated: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
	}
	out, err := it.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `---
id: 01hx2b9f
title: Buy milk
tags: []
created: 2026-09-01T08:00:00Z
updated: 2026-09-01T08:00:00Z
done_at:
---
`
	if string(out) != want {
		t.Errorf("a td-created item is no longer the §7 skeleton\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

// TestSkeletonCarriesEveryKeyTdOwns: a td-created item is written by skeleton,
// which used to carry its own literal list of the optional keys. A field added
// to keyOrder but not to that list round-tripped through a hand-written file
// and vanished from every item td created itself — set on the item, no error
// from Marshal, absent from the file. This asserts the two agree.
func TestSkeletonCarriesEveryKeyTdOwns(t *testing.T) {
	due, _ := ParseDate("2026-09-30")
	doneAt := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	it := &Item{
		ID:                "01hx2b9f",
		Title:             "everything at once",
		Tags:              []string{"a"},
		Due:               &due,
		Created:           time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
		Updated:           time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
		DoneAt:            &doneAt,
		Source:            "cli",
		Context:           "/tmp",
		ClaudeSessionName: "m5",
		ClaudeSessionID:   "abc",
	}
	out, err := it.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range keyOrder {
		if !strings.Contains(string(out), key+":") {
			t.Errorf("a td-created item does not carry %q, which keyOrder says td owns:\n%s", key, out)
		}
	}

	// And what it wrote parses back to the same item, so the keys are not just
	// present but readable.
	back, err := ParseItem(out)
	if err != nil {
		t.Fatalf("ParseItem of a skeleton: %v", err)
	}
	if back.Source != it.Source || back.Context != it.Context || back.ClaudeSessionID != it.ClaudeSessionID {
		t.Errorf("a skeleton round trip lost provenance: %+v", back)
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
	if it.parsed == nil {
		t.Fatal("ParseItem kept no mapping, so a rewrite cannot preserve the file's shape")
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
