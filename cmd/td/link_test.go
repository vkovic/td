package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/vkovic/td/internal/gitx"
	"github.com/vkovic/td/internal/store"
	"github.com/vkovic/td/internal/tdtest"
)

// harness is a td process's worth of state: an isolated store root and an
// isolated working directory.
type harness struct {
	t    *testing.T
	root string
	wd   string
}

// newHarness points TD_ROOT at a temp store, isolates git, and chdirs into a
// temp working directory.
func newHarness(t *testing.T) *harness {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".td")
	t.Setenv(store.EnvRoot, root)
	tdtest.IsolateGit(t)

	wd := t.TempDir()
	// macOS hands out /var symlinks for temp directories; resolve them so the
	// paths a command reports match the ones a test compares against.
	if resolved, err := filepath.EvalSymlinks(wd); err == nil {
		wd = resolved
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })

	return &harness{t: t, root: root, wd: wd}
}

// run executes the CLI with the given arguments, returning its streams.
func (h *harness) run(args ...string) (stdout, stderr string, err error) {
	h.t.Helper()
	return h.runStdin("", args...)
}

// runStdin executes the CLI with the given text on standard input.
func (h *harness) runStdin(stdin string, args ...string) (stdout, stderr string, err error) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	cmd := newRootCmdIO(&out, &errOut, strings.NewReader(stdin))
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

// mustRun executes the CLI and fails the test if the command errors.
func (h *harness) mustRun(args ...string) string {
	h.t.Helper()
	stdout, stderr, err := h.run(args...)
	if err != nil {
		h.t.Fatalf("td %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}
	return stdout
}

// mutation runs a mutating command with --json and decodes its result.
func (h *harness) mutation(args ...string) mutationResult {
	h.t.Helper()
	stdout := h.mustRun(append(args, "--json")...)
	var res mutationResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		h.t.Fatalf("td %s: output is not JSON: %v\n%s", strings.Join(args, " "), err, stdout)
	}
	return res
}

// openStore opens the harness's store for direct assertions.
func (h *harness) openStore() *store.Store {
	h.t.Helper()
	s, err := store.Open(h.root)
	if err != nil {
		h.t.Fatalf("store.Open: %v", err)
	}
	return s
}

// commits is how many commits the store's repository holds.
func (h *harness) commits() int {
	h.t.Helper()
	repo := gitx.New(h.root)
	has, err := repo.HasCommits()
	if err != nil {
		h.t.Fatal(err)
	}
	if !has {
		return 0
	}
	out, err := repo.Run("rev-list", "--count", "HEAD")
	if err != nil {
		h.t.Fatal(err)
	}
	n := 0
	for _, c := range strings.TrimSpace(out) {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestLinkUsesTheDirectoryName(t *testing.T) {
	h := newHarness(t)
	// Work in a named subdirectory, so the default name is predictable.
	dir := filepath.Join(h.wd, "acme-api")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := h.run("link")
	if err != nil {
		t.Fatalf("td link: %v", err)
	}
	if !strings.Contains(stdout, "acme-api") {
		t.Errorf("output = %q, want it to name the project", stdout)
	}

	scopeDir := filepath.Join(h.root, "acme-api")
	if _, err := os.Stat(scopeDir); err != nil {
		t.Errorf("td link did not create %s: %v", scopeDir, err)
	}
	name, err := store.ReadMarker(filepath.Join(dir, store.MarkerName))
	if err != nil {
		t.Fatalf("reading the marker: %v", err)
	}
	if name != "acme-api" {
		t.Errorf("marker names %q, want acme-api", name)
	}
}

func TestLinkWithAnExplicitName(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.run("link", "other"); err != nil {
		t.Fatalf("td link other: %v", err)
	}
	name, err := store.ReadMarker(filepath.Join(h.wd, store.MarkerName))
	if err != nil {
		t.Fatal(err)
	}
	if name != "other" {
		t.Errorf("marker names %q, want other", name)
	}
	if _, err := os.Stat(filepath.Join(h.root, "other")); err != nil {
		t.Errorf("td link other did not create the scope directory: %v", err)
	}
}

func TestLinkJSONReportsCreatedPaths(t *testing.T) {
	h := newHarness(t)
	stdout, _, err := h.run("link", "acme", "--json")
	if err != nil {
		t.Fatalf("td link --json: %v", err)
	}

	var got linkResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout)
	}
	if got.Project != "acme" {
		t.Errorf("project = %q, want acme", got.Project)
	}
	if want := filepath.Join(h.wd, store.MarkerName); got.Marker != want {
		t.Errorf("marker = %q, want %q", got.Marker, want)
	}
	if want := filepath.Join(h.root, "acme"); got.ScopeDir != want {
		t.Errorf("scope_dir = %q, want %q", got.ScopeDir, want)
	}
	if len(got.Created) != 2 {
		t.Errorf("created = %v, want the scope directory and the marker", got.Created)
	}

	// A second link over the same directory creates nothing new.
	stdout, _, err = h.run("link", "acme", "--json")
	if err != nil {
		t.Fatalf("second td link --json: %v", err)
	}
	got = linkResult{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Created) != 0 {
		t.Errorf("created = %v on a repeat link, want nothing new", got.Created)
	}
}

func TestLinkRejectsAnUnsafeName(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.run("link", "../escape"); err == nil {
		t.Error("td link ../escape = nil, want a rejection")
	}
	if _, err := os.Stat(filepath.Join(h.wd, store.MarkerName)); !os.IsNotExist(err) {
		t.Error("a rejected link still wrote a marker")
	}
}

func TestLinkThenScopeResolves(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.run("link", "acme"); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(h.wd, "src", "pkg")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	choice, err := store.ResolveScope(store.ScopeOptions{Dir: deep, Home: h.wd})
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if choice.Scope != store.Scope("acme") {
		t.Errorf("scope = %q, want acme from the marker td link wrote", choice.Scope)
	}
}

// TestRootWithNoArgsPrintsHelp pins the non-terminal branch of bare td. A
// terminal gets the TUI instead, but a test harness, a pipe and a Claude Code
// Bash call are all non-terminals, so this is the path everything but a person
// at a keyboard takes.
func TestRootWithNoArgsPrintsHelp(t *testing.T) {
	h := newHarness(t)
	if interactive() {
		t.Skip("the test process is attached to a terminal, so bare td would open the TUI")
	}
	stdout, _, err := h.run()
	if err != nil {
		t.Fatalf("td: %v", err)
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "link") {
		t.Errorf("bare td printed %q, want the help", stdout)
	}
	if !strings.Contains(stdout, "ui") {
		t.Errorf("the help does not list the ui command: %q", stdout)
	}
}

func TestGlobalAndProjectFlagsConflict(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.run("link", "acme", "-g", "-p", "other"); err == nil {
		t.Error("td -g -p = nil, want a conflict error")
	}
}

func TestUnknownConfigKeyWarnsWithoutFailing(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(h.root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(h.root, "config.toml")
	if err := os.WriteFile(cfg, []byte("done_ttl_dyas = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := h.run("link", "acme")
	if err != nil {
		t.Fatalf("td link: %v", err)
	}
	if !strings.Contains(stderr, "done_ttl_dyas") {
		t.Errorf("stderr = %q, want a warning naming the misspelled key", stderr)
	}
}

func TestVersionFlag(t *testing.T) {
	h := newHarness(t)
	stdout, _, err := h.run("--version")
	if err != nil {
		t.Fatalf("td --version: %v", err)
	}
	want := "td version " + resolvedVersion() + "\n"
	if stdout != want {
		t.Errorf("td --version printed %q, want %q", stdout, want)
	}
	if strings.Contains(stdout, "(devel)") {
		t.Errorf("td --version printed %q, which names no build", stdout)
	}
}

// TestBuildVersion: what --version says for each way td gets built. The
// toolchain stamps all of this into every binary already; before M4 nothing
// read it and every build called itself "dev".
func TestBuildVersion(t *testing.T) {
	info := func(mainVersion string, settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{
				Main:     debug.Module{Version: mainVersion},
				Settings: settings,
			}, true
		}
	}
	set := func(key, value string) debug.BuildSetting {
		return debug.BuildSetting{Key: key, Value: value}
	}
	const sha = "7c64f17834d2ab19f0e3"

	tests := []struct {
		name    string
		ldflags string
		read    func() (*debug.BuildInfo, bool)
		want    string
	}{
		{
			name:    "an ldflags version wins outright",
			ldflags: "v1.2.3",
			read:    info("v0.1.0", set("vcs.revision", sha)),
			want:    "v1.2.3",
		},
		{
			name:    "a checkout build is named by its commit",
			ldflags: devVersion,
			read:    info("(devel)", set("vcs.revision", sha), set("vcs.modified", "false")),
			want:    "7c64f17834d2",
		},
		{
			name:    "an edited checkout says so",
			ldflags: devVersion,
			read:    info("(devel)", set("vcs.revision", sha), set("vcs.modified", "true")),
			want:    "7c64f17834d2-dirty",
		},
		{
			name:    "the pseudo-version a checkout build carries is not the answer",
			ldflags: devVersion,
			read:    info("v0.0.0-20260910152159-7c64f17834d2+dirty", set("vcs.revision", sha), set("vcs.modified", "true")),
			want:    "7c64f17834d2-dirty",
		},
		{
			name:    "go install @v0.1.0 has no VCS info and is named by its tag",
			ldflags: devVersion,
			read:    info("v0.1.0"),
			want:    "v0.1.0",
		},
		{
			name:    "a build with nothing to go on stays dev",
			ldflags: devVersion,
			read:    info("(devel)"),
			want:    devVersion,
		},
		{
			name:    "no build information at all",
			ldflags: devVersion,
			read:    func() (*debug.BuildInfo, bool) { return nil, false },
			want:    devVersion,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildVersion(tc.ldflags, tc.read); got != tc.want {
				t.Errorf("buildVersion = %q, want %q", got, tc.want)
			}
		})
	}
}
