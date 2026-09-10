package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/vkovic/td/internal/config"
)

// readmeKeys pulls the key column out of the README's key table, so the check
// below reads what a person reads rather than a copy of it.
func readmeKeys(t *testing.T) map[string]string {
	t.Helper()
	text, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading the README: %v", err)
	}
	keys := make(map[string]string)
	for _, line := range strings.Split(string(text), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 2 {
			continue
		}
		// The first cell may name several keys, separated by a spaced slash:
		// "`j` / `↓`". The separator is spaced precisely so that the / key's
		// own row does not split into nothing.
		for _, part := range strings.Split(cells[0], " / ") {
			key := strings.Trim(strings.TrimSpace(part), "`")
			if key != "" {
				keys[key] = strings.TrimSpace(cells[1])
			}
		}
	}
	if len(keys) == 0 {
		t.Fatal("no key table found in the README")
	}
	return keys
}

// TestReadmeDocumentsEveryKey: the README is where someone looks before opening
// the pane, so a key it does not mention is a key nobody finds.
//
// The arrows and the return key are written as symbols in the README and as
// names in the table, and the environment table shares the README's pipe
// syntax, so both are translated rather than compared raw.
func TestReadmeDocumentsEveryKey(t *testing.T) {
	documented := readmeKeys(t)
	symbols := map[string]string{"down": "↓", "up": "↑", "enter": "⏎"}

	for _, b := range keyMap() {
		found := false
		for _, key := range b.keys {
			name := key
			if sym, ok := symbols[key]; ok {
				name = sym
			}
			if _, ok := documented[name]; ok {
				found = true
			}
		}
		if !found {
			t.Errorf("the README does not document %s", b.name())
		}
	}
}

// TestReadmeDocumentsNoKeyThatDoesNothing: the other direction, so a key
// removed from the table does not linger in the README.
func TestReadmeDocumentsNoKeyThatDoesNothing(t *testing.T) {
	bound := map[string]bool{}
	symbols := map[string]string{"down": "↓", "up": "↑", "enter": "⏎"}
	for _, b := range keyMap() {
		for _, key := range b.keys {
			bound[key] = true
			if sym, ok := symbols[key]; ok {
				bound[sym] = true
			}
		}
	}
	// ctrl+c is bound but not worth a row of its own.
	bound["ctrl+c"] = true

	for key := range readmeKeys(t) {
		// The configuration table's rows are config keys, not keystrokes.
		if strings.Contains(key, "_") {
			continue
		}
		if !bound[key] {
			t.Errorf("the README documents %q, which no binding answers", key)
		}
	}
}

// TestReadmeDocumentsTheEditorChainAsItIs: the README is what someone checks
// before assuming the INTENT is current, so it has to name every link in the
// chain config.Load actually walks, in the order it walks them. Asserting the
// order and not just the names is deliberate: the README spent three
// milestones claiming config.toml beat TD_EDITOR, which is backwards, and a
// membership-only test never noticed.
func TestReadmeDocumentsTheEditorChainAsItIs(t *testing.T) {
	text, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(text)

	chain := []string{config.EnvEditor, "`editor`", "$VISUAL", "$EDITOR", "`vi`"}
	prev := -1
	for _, link := range chain {
		at := strings.LastIndex(readme, link)
		if at < 0 {
			t.Errorf("the README does not document %q in the editor chain", link)
			continue
		}
		if at < prev {
			t.Errorf("the README documents %q out of order; the chain is %s", link, strings.Join(chain, " then "))
		}
		prev = at
	}

	// And the chain really is what the README claims: the config package reads
	// $VISUAL, and reads it before $EDITOR.
	src, err := os.ReadFile("../config/config.go")
	if err != nil {
		t.Fatal(err)
	}
	visual, editor := strings.Index(string(src), `"VISUAL"`), strings.Index(string(src), `"EDITOR"`)
	if visual < 0 {
		t.Error("config does not read $VISUAL, so the README's chain is stale")
	} else if editor >= 0 && editor < visual {
		t.Error("config reads $EDITOR before $VISUAL, the opposite of the documented chain")
	}
}

// TestReadmeDocumentsEveryConfigKey: each setting's environment variable is
// named, so overriding one for a single run does not mean reading the source.
func TestReadmeDocumentsEveryConfigKey(t *testing.T) {
	text, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(text)
	for _, env := range []string{
		config.EnvDoneTTLDays, config.EnvAutoCommit, config.EnvAutoPush, config.EnvEditor,
	} {
		if !strings.Contains(readme, env) {
			t.Errorf("the README does not name %s", env)
		}
	}
}
