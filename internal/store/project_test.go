package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fixtureTree builds a directory tree under a temp root and optionally drops a
// .td marker somewhere in it. It returns the root, which stands in for $HOME.
func fixtureTree(t *testing.T, markerDir, markerBody string) (home string) {
	t.Helper()
	home = t.TempDir()
	deep := filepath.Join(home, "code", "acme", "api", "internal")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if markerDir != "" {
		dir := filepath.Join(home, markerDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, MarkerName), []byte(markerBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestProjectMarkerInCwd(t *testing.T) {
	home := fixtureTree(t, filepath.Join("code", "acme", "api", "internal"), "acme-api\n")
	dir := filepath.Join(home, "code", "acme", "api", "internal")

	got, err := ResolveScope(ScopeOptions{Dir: dir, Home: home})
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got.Scope != Scope("acme-api") {
		t.Errorf("Scope = %q, want acme-api", got.Scope)
	}
	if got.Reason != ReasonMarker {
		t.Errorf("Reason = %q, want %q", got.Reason, ReasonMarker)
	}
	if want := filepath.Join(dir, MarkerName); got.Marker != want {
		t.Errorf("Marker = %q, want %q", got.Marker, want)
	}
}

func TestProjectMarkerThreeLevelsUp(t *testing.T) {
	home := fixtureTree(t, filepath.Join("code", "acme"), "acme\n")
	dir := filepath.Join(home, "code", "acme", "api", "internal")

	got, err := ResolveScope(ScopeOptions{Dir: dir, Home: home})
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got.Scope != Scope("acme") {
		t.Errorf("Scope = %q, want acme", got.Scope)
	}
	if want := filepath.Join(home, "code", "acme", MarkerName); got.Marker != want {
		t.Errorf("Marker = %q, want %q", got.Marker, want)
	}
}

func TestProjectNoMarkerAnywhere(t *testing.T) {
	home := fixtureTree(t, "", "")
	dir := filepath.Join(home, "code", "acme", "api", "internal")

	got, err := ResolveScope(ScopeOptions{Dir: dir, Home: home})
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got.Scope != Global {
		t.Errorf("Scope = %q, want the global scope", got.Scope)
	}
	if got.Reason != ReasonDefault {
		t.Errorf("Reason = %q, want %q", got.Reason, ReasonDefault)
	}
	if got.Marker != "" {
		t.Errorf("Marker = %q, want empty", got.Marker)
	}
}

func TestProjectGlobalFlagBeatsMarker(t *testing.T) {
	home := fixtureTree(t, filepath.Join("code", "acme"), "acme\n")
	dir := filepath.Join(home, "code", "acme", "api", "internal")

	got, err := ResolveScope(ScopeOptions{Dir: dir, Home: home, Global: true})
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got.Scope != Global {
		t.Errorf("Scope = %q, want the global scope", got.Scope)
	}
	if got.Reason != ReasonFlagGlobal {
		t.Errorf("Reason = %q, want %q", got.Reason, ReasonFlagGlobal)
	}
}

func TestProjectFlagBeatsMarker(t *testing.T) {
	home := fixtureTree(t, filepath.Join("code", "acme"), "acme\n")
	dir := filepath.Join(home, "code", "acme")

	got, err := ResolveScope(ScopeOptions{Dir: dir, Home: home, Project: "  other  "})
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got.Scope != Scope("other") {
		t.Errorf("Scope = %q, want other", got.Scope)
	}
	if got.Reason != ReasonFlagProject {
		t.Errorf("Reason = %q, want %q", got.Reason, ReasonFlagProject)
	}
}

func TestProjectBothFlagsConflict(t *testing.T) {
	home := fixtureTree(t, "", "")
	_, err := ResolveScope(ScopeOptions{Dir: home, Home: home, Global: true, Project: "acme"})
	if !errors.Is(err, ErrScopeConflict) {
		t.Errorf("ResolveScope error = %v, want ErrScopeConflict", err)
	}
}

func TestProjectWalkStopsAtHome(t *testing.T) {
	// A marker above $HOME must not be picked up.
	outer := t.TempDir()
	home := filepath.Join(outer, "home")
	dir := filepath.Join(home, "code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outer, MarkerName), []byte("above-home\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveScope(ScopeOptions{Dir: dir, Home: home})
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got.Scope != Global {
		t.Errorf("Scope = %q, want the global scope: the walk escaped $HOME", got.Scope)
	}
}

func TestProjectMarkerInHomeIsUsed(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, MarkerName), []byte("catch-all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveScope(ScopeOptions{Dir: home, Home: home})
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got.Scope != Scope("catch-all") {
		t.Errorf("Scope = %q, want catch-all", got.Scope)
	}
}

func TestProjectStoreDirectoryIsNotAMarker(t *testing.T) {
	// ~/.td is the store itself, a directory. The walk must not read it as a
	// marker naming a project.
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, MarkerName), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveScope(ScopeOptions{Dir: dir, Home: home})
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got.Scope != Global {
		t.Errorf("Scope = %q, want the global scope", got.Scope)
	}
}

func TestReadMarker(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"bare name", "acme\n", "acme"},
		{"whitespace", "  acme  \n", "acme"},
		{"comments skipped", "# written by td link\n\nacme\n", "acme"},
		{"extra lines ignored", "acme\nnotes\n", "acme"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, MarkerName)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := ReadMarker(path)
			if err != nil {
				t.Fatalf("ReadMarker: %v", err)
			}
			if got != tc.want {
				t.Errorf("ReadMarker = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadMarkerEmptyTakesDirectoryName(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "acme-api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, MarkerName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if got != "acme-api" {
		t.Errorf("ReadMarker = %q, want the directory name acme-api", got)
	}
}

func TestReadMarkerRejectsUnsafeName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, MarkerName)
	if err := os.WriteFile(path, []byte("../escape\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMarker(path); err == nil {
		t.Error("ReadMarker accepted a name with a path separator, want an error")
	}
}

func TestWriteMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteMarker(dir, "acme-api")
	if err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	if want := filepath.Join(dir, MarkerName); path != want {
		t.Errorf("WriteMarker returned %q, want %q", path, want)
	}
	name, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if name != "acme-api" {
		t.Errorf("ReadMarker = %q, want acme-api", name)
	}

	// Rewriting replaces rather than appends.
	if _, err := WriteMarker(dir, "renamed"); err != nil {
		t.Fatalf("WriteMarker again: %v", err)
	}
	name, err = ReadMarker(path)
	if err != nil {
		t.Fatal(err)
	}
	if name != "renamed" {
		t.Errorf("ReadMarker after rewrite = %q, want renamed", name)
	}
}

func TestCleanProjectName(t *testing.T) {
	good := []struct{ in, want string }{
		{"acme", "acme"},
		{"  acme-api  ", "acme-api"},
		{"Acme_API 2", "Acme_API 2"},
	}
	for _, tc := range good {
		got, err := CleanProjectName(tc.in)
		if err != nil {
			t.Errorf("CleanProjectName(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CleanProjectName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	bad := []string{"", "   ", ".", "..", "../escape", "a/b", `a\b`, ".hidden", "archived", "deleted"}
	for _, in := range bad {
		if _, err := CleanProjectName(in); err == nil {
			t.Errorf("CleanProjectName(%q) = nil error, want a rejection", in)
		}
	}
}
