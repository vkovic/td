package store

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestIDUniqueAndAscending(t *testing.T) {
	const n = 10000
	ids := make([]string, n)
	seen := make(map[string]bool, n)
	for i := range ids {
		id := NewID()
		if len(id) != idLen {
			t.Fatalf("NewID() = %q, want %d characters", id, idLen)
		}
		if seen[id] {
			t.Fatalf("NewID() repeated %q at call %d", id, i)
		}
		seen[id] = true
		if i > 0 && id <= ids[i-1] {
			t.Fatalf("NewID() went backwards at call %d: %q after %q", i, id, ids[i-1])
		}
		ids[i] = id
	}
	if !sort.StringsAreSorted(ids) {
		t.Error("10k sequential ids are not lexically sorted")
	}
}

func TestIDCharsetIsCrockford(t *testing.T) {
	for i := 0; i < 1000; i++ {
		for _, r := range NewID() {
			if !strings.ContainsRune(crockford, r) {
				t.Fatalf("id character %q is outside the Crockford alphabet", r)
			}
		}
	}
}

func TestIDConcurrentCallsAreUnique(t *testing.T) {
	const workers, each = 8, 500
	var mu sync.Mutex
	seen := make(map[string]bool, workers*each)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				id := NewID()
				mu.Lock()
				if seen[id] {
					t.Errorf("concurrent NewID() repeated %q", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}

func TestIDEncodesTimeOrder(t *testing.T) {
	// Ids minutes apart must sort in time order, independent of the monotonic
	// tiebreak that only kicks in within one millisecond.
	early := newIDAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	late := newIDAt(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if early >= late {
		t.Errorf("id ordering does not follow time: %q (Jan) >= %q (Jun)", early, late)
	}
}

func TestIDStaysEightCharsThroughRange(t *testing.T) {
	for _, when := range []time.Time{idEpoch, time.Now(), time.Date(2054, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if got := newIDAt(when); len(got) != idLen {
			t.Errorf("newIDAt(%v) = %q, want %d characters", when, got, idLen)
		}
	}
}

func TestSlug(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"plain", "Wire the epilogue", "wire-the-epilogue"},
		{"punctuation", "Fix: the parser's #1 bug!", "fix-the-parser-s-1-bug"},
		{"leading and trailing junk", "  ...Hello...  ", "hello"},
		{"collapses runs", "a  ---  b", "a-b"},
		{"digits kept", "Ship v2 by 2026", "ship-v2-by-2026"},
		{"path hostile", "docs/api: v1\\draft", "docs-api-v1-draft"},
		{"unicode letters kept", "Привет мир", "привет-мир"},
		{"unicode accents kept", "Café résumé", "café-résumé"},
		{"emoji dropped", "Ship it 🚀 now", "ship-it-now"},
		{"empty", "", ""},
		{"nothing alphanumeric", "!!! ... ???", ""},
		{"only separators", "---", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Slug(tc.title); got != tc.want {
				t.Errorf("Slug(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestSlugTruncates(t *testing.T) {
	long := strings.Repeat("alpha bravo ", 30) // 360 characters
	got := Slug(long)
	if len(got) > slugMaxLen {
		t.Errorf("Slug truncated to %d bytes, want at most %d: %q", len(got), slugMaxLen, got)
	}
	if strings.HasSuffix(got, "-") || strings.HasPrefix(got, "-") {
		t.Errorf("Slug(%q...) = %q, want no leading or trailing hyphen", long[:20], got)
	}
	if !strings.HasSuffix(got, "alpha") && !strings.HasSuffix(got, "bravo") {
		t.Errorf("Slug cut mid-word: %q", got)
	}
}

func TestSlugTruncatesOnRuneBoundary(t *testing.T) {
	// Two-byte runes with no hyphen to fall back to: the cut lands mid-rune.
	got := Slug(strings.Repeat("ф", 200))
	if len(got) > slugMaxLen {
		t.Errorf("Slug is %d bytes, want at most %d", len(got), slugMaxLen)
	}
	if !utf8.ValidString(got) {
		t.Errorf("Slug produced invalid UTF-8: %q", got)
	}
}
