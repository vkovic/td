package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openTemp opens a store in a throwaway directory.
func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// newItem builds a minimal item for tests.
func newItem(id, title string) *Item {
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	return &Item{ID: id, Title: title, Created: now, Updated: now}
}

func TestOpenCreatesStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", ".td")
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.Root() != root {
		t.Errorf("Root() = %q, want %q", s.Root(), root)
	}
	for _, name := range []string{"config.toml", ".gitignore", ".git"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("Open did not create %s: %v", name, err)
		}
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "**/deleted/") {
		t.Errorf(".gitignore = %q, want it to ignore **/deleted/", ignore)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	root := t.TempDir()
	if _, err := Open(root); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	custom := "# mine\ndone_ttl_days = 30\n"
	cfg := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfg, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err != nil {
		t.Fatalf("second Open: %v", err)
	}
	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != custom {
		t.Errorf("Open overwrote config.toml: %q", got)
	}
}

func TestDefaultRootUsesEnv(t *testing.T) {
	t.Setenv(EnvRoot, "/tmp/td-root-test")
	got, err := DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/td-root-test" {
		t.Errorf("DefaultRoot() = %q, want the TD_ROOT value", got)
	}

	t.Setenv(EnvRoot, "")
	got, err = DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if want := filepath.Join(home, ".td"); got != want {
		t.Errorf("DefaultRoot() = %q, want %q", got, want)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	s := openTemp(t)
	it := newItem("7k3m9q2x", "Wire the epilogue")
	it.Tags = []string{"cli"}
	it.Body = "Details.\n"

	ref, err := s.Save(Global, Active, it)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if want := filepath.Join(s.Root(), "7k3m9q2x-wire-the-epilogue.md"); ref.Path != want {
		t.Errorf("saved to %q, want %q", ref.Path, want)
	}

	back, err := s.Load(ref.Path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if back.Title != it.Title || back.Body != it.Body || len(back.Tags) != 1 {
		t.Errorf("round trip lost data: %+v", back)
	}
}

func TestSaveLeavesNoTempFile(t *testing.T) {
	s := openTemp(t)
	for i := 0; i < 5; i++ {
		if _, err := s.Save(Global, Active, newItem(NewID(), "Item")); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	ents, err := os.ReadDir(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".td-") {
			t.Errorf("save left a temporary file behind: %s", e.Name())
		}
	}
}

func TestSaveRenamesOnTitleChange(t *testing.T) {
	s := openTemp(t)
	it := newItem("7k3m9q2x", "Old title")
	if _, err := s.Save(Global, Active, it); err != nil {
		t.Fatalf("Save: %v", err)
	}
	it.Title = "New title"
	ref, err := s.Save(Global, Active, it)
	if err != nil {
		t.Fatalf("Save after rename: %v", err)
	}
	if filepath.Base(ref.Path) != "7k3m9q2x-new-title.md" {
		t.Errorf("saved to %q, want the new slug", ref.Path)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "7k3m9q2x-old-title.md")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the file under the old title survived the rename")
	}
	entries, err := s.List(Global)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("List returned %d entries, want 1 after a rename", len(entries))
	}
}

func TestSaveUntitled(t *testing.T) {
	s := openTemp(t)
	ref, err := s.Save(Global, Active, newItem("7k3m9q2x", "!!!"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if want := "7k3m9q2x-untitled.md"; filepath.Base(ref.Path) != want {
		t.Errorf("saved to %q, want %q", filepath.Base(ref.Path), want)
	}
}

func TestSaveRequiresID(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Save(Global, Active, newItem("", "No id")); err == nil {
		t.Error("Save with no id returned nil, want an error")
	}
}

func TestSaveIntoScopeAndArea(t *testing.T) {
	s := openTemp(t)
	ref, err := s.Save(Scope("td"), Archived, newItem("7k3m9q2x", "Old news"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := filepath.Join(s.Root(), "td", "archived", "7k3m9q2x-old-news.md")
	if ref.Path != want {
		t.Errorf("saved to %q, want %q", ref.Path, want)
	}
}

func TestMove(t *testing.T) {
	s := openTemp(t)
	it := newItem("7k3m9q2x", "Buy milk")
	from, err := s.Save(Scope("td"), Active, it)
	if err != nil {
		t.Fatal(err)
	}
	to, err := s.Move(from, it, Scope("td"), Deleted)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(from.Path); !errors.Is(err, os.ErrNotExist) {
		t.Error("Move left the source file behind")
	}
	if _, err := os.Stat(to.Path); err != nil {
		t.Errorf("Move did not write the destination: %v", err)
	}
	if to.Area != Deleted {
		t.Errorf("destination area = %q, want %q", to.Area, Deleted)
	}
}

func TestListAreasAndScopes(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Save(Global, Active, newItem("aaaaaaa1", "Global open")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Global, Archived, newItem("aaaaaaa2", "Global archived")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Scope("td"), Active, newItem("bbbbbbb1", "Project open")); err != nil {
		t.Fatal(err)
	}

	active, err := s.List(Global)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Item.ID != "aaaaaaa1" {
		t.Errorf("List(Global) = %d entries, want just the active one", len(active))
	}

	both, err := s.List(Global, Active, Archived)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 {
		t.Errorf("List(Global, Active, Archived) = %d entries, want 2", len(both))
	}

	all, err := s.ListAll(Active)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAll(Active) = %d entries, want 2", len(all))
	}
	scopes, err := s.Scopes()
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || scopes[0] != Global || scopes[1] != Scope("td") {
		t.Errorf("Scopes() = %v, want [global td]", scopes)
	}
}

func TestScopesIgnoresStoreDirectories(t *testing.T) {
	s := openTemp(t)
	for _, dir := range []string{"archived", "deleted", ".git", "td"} {
		if err := os.MkdirAll(filepath.Join(s.Root(), dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scopes, err := s.Scopes()
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || scopes[1] != Scope("td") {
		t.Errorf("Scopes() = %v, want only [global td]", scopes)
	}
}

func TestListSkipsUnparsableFileAndReportsIt(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Save(Global, Active, newItem("aaaaaaa1", "Good")); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(s.Root(), "bbbbbbb1-broken.md")
	if err := os.WriteFile(bad, []byte("no frontmatter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := s.List(Global)
	if len(entries) != 1 || entries[0].Item.ID != "aaaaaaa1" {
		t.Errorf("List returned %d entries, want the one that parses", len(entries))
	}
	if err == nil {
		t.Error("List returned a nil error, want the unparsable file reported")
	} else if !strings.Contains(err.Error(), "bbbbbbb1-broken.md") {
		t.Errorf("List error = %v, want it to name the bad file", err)
	}
}

func TestListIgnoresNonItemFiles(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Save(Global, Active, newItem("aaaaaaa1", "Good")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notes.txt", ".hidden.md", "README"} {
		if err := os.WriteFile(filepath.Join(s.Root(), name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.List(Global)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("List returned %d entries, want 1", len(entries))
	}
}

func TestListMissingDirectoryIsEmpty(t *testing.T) {
	s := openTemp(t)
	entries, err := s.List(Scope("never-used"), Active, Archived, Deleted)
	if err != nil {
		t.Fatalf("List on a scope with no directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("List returned %d entries, want 0", len(entries))
	}
}

func TestResolvePrefix(t *testing.T) {
	entries := []Entry{
		{Item: newItem("7k3m9q2x", "One")},
		{Item: newItem("7k3maaaa", "Two")},
		{Item: newItem("01hx2b9f", "Three")},
	}
	tests := []struct {
		name    string
		prefix  string
		wantID  string
		wantErr error
	}{
		{"unique prefix", "01", "01hx2b9f", nil},
		{"full id", "7k3m9q2x", "7k3m9q2x", nil},
		{"uppercase input", "01HX", "01hx2b9f", nil},
		{"ambiguous", "7k3m", "", ErrAmbiguous},
		{"missing", "zz", "", ErrNotFound},
		{"empty", "", "", ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolvePrefix(tc.prefix, entries)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ResolvePrefix(%q) error = %v, want %v", tc.prefix, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePrefix(%q): %v", tc.prefix, err)
			}
			if got.Item.ID != tc.wantID {
				t.Errorf("ResolvePrefix(%q) = %q, want %q", tc.prefix, got.Item.ID, tc.wantID)
			}
		})
	}
}

func TestResolvePrefixExactMatchBeatsLongerID(t *testing.T) {
	// A full id must resolve even when another id extends it.
	entries := []Entry{
		{Item: newItem("7k3m9q2x", "Exact")},
		{Item: newItem("7k3m9q2xy", "Longer")},
	}
	got, err := ResolvePrefix("7k3m9q2x", entries)
	if err != nil {
		t.Fatalf("ResolvePrefix: %v", err)
	}
	if got.Item.Title != "Exact" {
		t.Errorf("resolved to %q, want the exact match", got.Item.Title)
	}
}

func TestResolveAmbiguousErrorNamesCandidates(t *testing.T) {
	entries := []Entry{
		{Item: newItem("7k3m9q2x", "One")},
		{Item: newItem("7k3maaaa", "Two")},
	}
	_, err := ResolvePrefix("7k3m", entries)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"7k3m9q2x", "7k3maaaa"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %v does not name %s", err, want)
		}
	}
}

func TestStoreResolveAcrossScopes(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Save(Scope("td"), Active, newItem("7k3m9q2x", "In a project")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve("7k3m", Global); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resolve in the global scope = %v, want ErrNotFound", err)
	}
	got, err := s.ResolveAll("7k3m")
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}
	if got.Ref.Scope != Scope("td") {
		t.Errorf("resolved scope = %q, want td", got.Ref.Scope)
	}
}

func TestLoadRejectsItemWithoutID(t *testing.T) {
	s := openTemp(t)
	path := filepath.Join(s.Root(), "aaaaaaa1-no-id.md")
	if err := os.WriteFile(path, []byte("---\ntitle: No id\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(path); err == nil {
		t.Error("Load accepted an item with no id, want an error")
	}
}

func TestScopeString(t *testing.T) {
	if Global.String() != "global" {
		t.Errorf("Global.String() = %q, want global", Global.String())
	}
	if Scope("td").String() != "td" {
		t.Errorf(`Scope("td").String() = %q, want td`, Scope("td").String())
	}
}
