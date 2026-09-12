// Package e2e drives the td binary the way a person or the /td plugin would:
// building it, running it as a process, and reading its output and exit code.
// Everything below the CLI has its own tests; this is the contract itself.
package e2e_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vkovic/td/internal/tdtest"
)

// Exit codes td documents. They are repeated here rather than imported, so a
// change to them has to be made deliberately in two places.
const (
	exitOK        = 0
	exitFailure   = 1
	exitUsage     = 2
	exitNotFound  = 3
	exitAmbiguous = 4
)

// binary is the td built for this run.
var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "td-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating a temp directory:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	binary = filepath.Join(dir, "td")
	build := exec.Command("go", "build", "-o", binary, "./cmd/td")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building td: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// cli is a td installation: a store root and a working directory.
type cli struct {
	t    *testing.T
	root string
	dir  string
}

// newCLI sets up an isolated store and working directory.
func newCLI(t *testing.T) *cli {
	t.Helper()
	base := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	dir := filepath.Join(base, "work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &cli{t: t, root: filepath.Join(base, "store"), dir: dir}
}

// run executes td and returns its output and exit code.
func (c *cli) run(args ...string) (stdout, stderr string, code int) {
	c.t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = c.dir
	cmd.Env = append(os.Environ(), "TD_ROOT="+c.root)
	cmd.Env = append(cmd.Env, tdtest.GitEnv(filepath.Dir(c.root))...)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()
	code = 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			c.t.Fatalf("running td %s: %v", strings.Join(args, " "), err)
		}
		code = exit.ExitCode()
	}
	return out.String(), errOut.String(), code
}

// ok runs td and fails the test unless it succeeded.
func (c *cli) ok(args ...string) string {
	c.t.Helper()
	stdout, stderr, code := c.run(args...)
	if code != exitOK {
		c.t.Fatalf("td %s exited %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout
}

// jsonOut runs td with --json and decodes the result into v.
func (c *cli) jsonOut(v any, args ...string) {
	c.t.Helper()
	stdout := c.ok(append(args, "--json")...)
	if err := json.Unmarshal([]byte(stdout), v); err != nil {
		c.t.Fatalf("td %s: output is not JSON: %v\n%s", strings.Join(args, " "), err, stdout)
	}
}

// item is the part of td's JSON item shape the end-to-end tests assert on.
type item struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Tags    []string   `json:"tags"`
	Due     string     `json:"due"`
	Updated time.Time  `json:"updated"`
	DoneAt  *time.Time `json:"done_at"`
	Done    bool       `json:"done"`
	Scope   string     `json:"scope"`
	Area    string     `json:"area"`
	Path    string     `json:"path"`
	Body    string     `json:"body"`
	Source  string     `json:"source"`
}

type listResult struct {
	Scope string `json:"scope"`
	Items []item `json:"items"`
}

type mutation struct {
	Action   string `json:"action"`
	Items    []item `json:"items"`
	Epilogue struct {
		Bumped    []string `json:"bumped"`
		Archived  []string `json:"archived"`
		Committed bool     `json:"committed"`
		Message   string   `json:"message"`
		Pushed    bool     `json:"pushed"`
		Warnings  []string `json:"warnings"`
	} `json:"epilogue"`
}

// add creates an item and returns it.
func (c *cli) add(args ...string) item {
	c.t.Helper()
	var res mutation
	c.jsonOut(&res, append([]string{"add"}, args...)...)
	if len(res.Items) != 1 {
		c.t.Fatalf("td add reported %d items, want 1", len(res.Items))
	}
	return res.Items[0]
}

// list reads the current list.
func (c *cli) list(args ...string) []item {
	c.t.Helper()
	var res listResult
	c.jsonOut(&res, append([]string{"ls"}, args...)...)
	return res.Items
}

// git runs a git command against the store.
func (c *cli) git(args ...string) string {
	c.t.Helper()
	full := append([]string{"-C", c.root}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		c.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// commits is how many commits the store holds.
func (c *cli) commits() int {
	c.t.Helper()
	out := strings.TrimSpace(c.git("rev-list", "--count", "HEAD"))
	n := 0
	for _, r := range out {
		n = n*10 + int(r-'0')
	}
	return n
}

// TestLifecycle drives one item through every state the CLI can put it in, and
// checks that each step leaves exactly one commit behind.
func TestLifecycle(t *testing.T) {
	c := newCLI(t)

	// Link the working directory to a project, so items land in its list.
	c.ok("link", "acme")
	if _, err := os.Stat(filepath.Join(c.dir, ".td")); err != nil {
		t.Fatalf("td link wrote no marker: %v", err)
	}

	// Add, and see it in the list, open.
	added := c.add("Buy", "oat", "milk", "-t", "shopping", "--due", "2026-10-01", "-b", "Two cartons.")
	if added.Scope != "acme" {
		t.Errorf("scope = %q, want acme from the marker", added.Scope)
	}
	items := c.list()
	if len(items) != 1 || items[0].ID != added.ID {
		t.Fatalf("td ls listed %+v, want the item just added", items)
	}
	if items[0].Done {
		t.Error("a new item is done")
	}
	commits := c.commits()
	if commits != 1 {
		t.Fatalf("commit count after link and add = %d, want 1", commits)
	}

	// Edit it.
	c.ok("edit", added.ID, "--title", "Buy oat milk, two cartons", "--append", "The blue one.")
	items = c.list()
	if items[0].Title != "Buy oat milk, two cartons" {
		t.Errorf("title = %q", items[0].Title)
	}
	if got := c.commits(); got != commits+1 {
		t.Errorf("commit count after edit = %d, want %d", got, commits+1)
	}
	commits++

	// show carries the body.
	var shown struct {
		Item item `json:"item"`
	}
	c.jsonOut(&shown, "show", added.ID[:6])
	if !strings.Contains(shown.Item.Body, "Two cartons.") || !strings.Contains(shown.Item.Body, "The blue one.") {
		t.Errorf("body = %q, want both paragraphs", shown.Item.Body)
	}

	// Done hides it from the list, and --done brings it back.
	c.ok("done", added.ID)
	if got := c.list(); len(got) != 0 {
		t.Errorf("td ls listed %+v after done, want nothing", got)
	}
	done := c.list("--done")
	if len(done) != 1 || !done[0].Done || done[0].DoneAt == nil {
		t.Errorf("td ls --done listed %+v, want the item marked done", done)
	}
	commits++

	// Undo reopens it.
	c.ok("undo", added.ID)
	if got := c.list(); len(got) != 1 || got[0].Done {
		t.Errorf("td ls listed %+v after undo, want the item open again", got)
	}
	commits++

	// rm puts it in the trash, which is not committed, and restore brings it
	// back with its body intact.
	c.ok("rm", added.ID)
	if got := c.list(); len(got) != 0 {
		t.Errorf("td ls listed %+v after rm", got)
	}
	trash := filepath.Join(c.root, "acme", "deleted")
	entries, err := os.ReadDir(trash)
	if err != nil || len(entries) != 1 {
		t.Fatalf("the trash holds %v (%v), want the removed item", entries, err)
	}
	if tracked := c.git("ls-files"); strings.Contains(tracked, "deleted/") {
		t.Errorf("the trash was committed:\n%s", tracked)
	}
	commits++

	c.ok("restore", added.ID)
	restored := c.list()
	if len(restored) != 1 {
		t.Fatalf("td ls listed %+v after restore, want the item back", restored)
	}
	c.jsonOut(&shown, "show", added.ID)
	if !strings.Contains(shown.Item.Body, "Two cartons.") {
		t.Error("the restored item lost its body")
	}
	commits++

	if got := c.commits(); got != commits {
		t.Errorf("commit count = %d, want %d: one per mutation", got, commits)
	}
}

// replaceFrontmatterKey rewrites one frontmatter key's value in an item file,
// the way an editor would.
func replaceFrontmatterKey(file, key, value string) string {
	lines := strings.Split(file, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, key+":") {
			lines[i] = key + ": " + value
			break
		}
	}
	return strings.Join(lines, "\n")
}

// TestScopes checks that the global list and a project list stay separate and
// that --all merges them.
func TestScopes(t *testing.T) {
	c := newCLI(t)
	c.ok("link", "acme")

	inProject := c.add("Project item")
	global := c.add("Global item", "-g")

	if got := c.list(); len(got) != 1 || got[0].ID != inProject.ID {
		t.Errorf("the project list holds %+v, want only the project item", got)
	}
	if got := c.list("-g"); len(got) != 1 || got[0].ID != global.ID {
		t.Errorf("the global list holds %+v, want only the global item", got)
	}

	var res listResult
	c.jsonOut(&res, "ls", "--all")
	if len(res.Items) != 2 {
		t.Fatalf("td ls --all listed %d items, want 2", len(res.Items))
	}
	if res.Scope != "all" {
		t.Errorf("scope = %q, want all", res.Scope)
	}
}

// TestHandEditIsRecordedAndCommitted is the store's whole promise: edit an item
// file in an editor, and the next td command notices, dates it, and commits it.
func TestHandEditIsRecordedAndCommitted(t *testing.T) {
	c := newCLI(t)
	added := c.add("Edit me by hand")
	before := c.commits()

	path := added.Path
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Backdate updated as part of the edit. td stores timestamps to the second,
	// so an edit made in the same second the item was added would otherwise bump
	// to a value that is equal rather than later, and prove nothing.
	old := time.Now().UTC().AddDate(0, 0, -3).Truncate(time.Second)
	edited := strings.Replace(string(body), "title: Edit me by hand", "title: Edited in vim", 1)
	edited = replaceFrontmatterKey(edited, "updated", old.Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(edited+"\nA paragraph typed by hand.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var res listResult
	c.jsonOut(&res, "ls")
	if len(res.Items) != 1 {
		t.Fatalf("td ls listed %d items, want 1", len(res.Items))
	}
	if res.Items[0].Title != "Edited in vim" {
		t.Errorf("title = %q, want the hand-edited one", res.Items[0].Title)
	}
	if !res.Items[0].Updated.After(old) {
		t.Errorf("updated = %v, want it bumped past the hand-written %v", res.Items[0].Updated, old)
	}
	if got := c.commits(); got != before+1 {
		t.Errorf("commit count = %d, want the hand edit committed as one more than %d", got, before)
	}

	// A second read must not bump it again.
	updated := res.Items[0].Updated
	after := c.commits()
	c.jsonOut(&res, "ls")
	if !res.Items[0].Updated.Equal(updated) {
		t.Errorf("updated = %v on a second read, want it left at %v", res.Items[0].Updated, updated)
	}
	if got := c.commits(); got != after {
		t.Errorf("commit count = %d, want it unchanged at %d", got, after)
	}
}

// TestArchiveSweep checks that a done item leaves the list once its TTL has
// elapsed, and that undoing it brings it back.
func TestArchiveSweep(t *testing.T) {
	c := newCLI(t)
	added := c.add("Done long ago")
	c.ok("done", added.ID)

	// Backdate done_at past the TTL, the way time passing would.
	body, err := os.ReadFile(added.Path)
	if err != nil {
		t.Fatal(err)
	}
	longAgo := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "done_at:") {
			lines[i] = "done_at: " + longAgo
		}
	}
	if err := os.WriteFile(added.Path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	c.ok("ls")
	if got := c.list("--done"); len(got) != 0 {
		t.Errorf("td ls --done listed %+v, want the item swept into the archive", got)
	}
	archived := filepath.Join(c.root, "archived")
	entries, err := os.ReadDir(archived)
	if err != nil || len(entries) != 1 {
		t.Fatalf("archived/ holds %v (%v), want the swept item", entries, err)
	}

	// Undo reaches into the archive and puts it back in the list.
	c.ok("undo", added.ID)
	if got := c.list(); len(got) != 1 {
		t.Errorf("td ls listed %+v after undo, want the item back", got)
	}
}

// TestExitCodes pins the documented codes: a caller must be able to tell why a
// command failed without reading its message.
func TestExitCodes(t *testing.T) {
	c := newCLI(t)
	first := c.add("First")
	c.add("Second")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"ls"}, exitOK},
		{"unknown id", []string{"show", "nosuchid"}, exitNotFound},
		{"unknown id on a mutation", []string{"done", "nosuchid"}, exitNotFound},
		{"ambiguous prefix", []string{"show", first.ID[:2]}, exitAmbiguous},
		{"unknown flag", []string{"ls", "--nosuchflag"}, exitUsage},
		{"unknown command", []string{"nosuchcommand"}, exitUsage},
		{"missing argument", []string{"show"}, exitUsage},
		{"too many arguments", []string{"ls", "extra"}, exitUsage},
		{"contradictory flags", []string{"ls", "-g", "-p", "acme"}, exitUsage},
		{"nothing to change", []string{"edit", first.ID}, exitUsage},
		{"blank title", []string{"add", "   "}, exitUsage},
		{"bad due date", []string{"add", "Item", "--due", "soon"}, exitUsage},
		{"unsafe project name", []string{"link", "../escape"}, exitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := c.run(tc.args...)
			if code != tc.want {
				t.Errorf("td %s exited %d, want %d\nstdout: %s\nstderr: %s",
					strings.Join(tc.args, " "), code, tc.want, stdout, stderr)
			}
		})
	}
}

// TestNotFoundCodeIsDocumented is the plan's own check: td show nosuchid; echo $?
func TestNotFoundCodeIsDocumented(t *testing.T) {
	c := newCLI(t)
	_, stderr, code := c.run("show", "nosuchid")
	if code != exitNotFound {
		t.Errorf("td show nosuchid exited %d, want %d", code, exitNotFound)
	}
	if !strings.Contains(stderr, "no item matches") {
		t.Errorf("stderr = %q, want it to say the id was not found", stderr)
	}
}

// TestJSONIsCleanWhenSomethingIsWrong checks that --json stays parseable while
// a warning is being reported, since the /td plugin pipes it straight into a
// parser.
func TestJSONIsCleanWhenSomethingIsWrong(t *testing.T) {
	c := newCLI(t)
	c.add("Good item")

	// A config key nobody recognizes produces a warning on every command.
	cfg := filepath.Join(c.root, "config.toml")
	if err := os.WriteFile(cfg, []byte("done_ttl_dyas = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := c.run("ls", "--json")
	if code != exitOK {
		t.Fatalf("td ls --json exited %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "done_ttl_dyas") {
		t.Errorf("stderr = %q, want the misspelled key warned about", stderr)
	}
	var res listResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Errorf("standard output is not parseable JSON: %v\n%s", err, stdout)
	}
}

// TestProvenanceFlags checks the flags the /td plugin will pass.
func TestProvenanceFlags(t *testing.T) {
	c := newCLI(t)
	added := c.add("From a session",
		"--source", "claude", "--session-name", "milestone-1", "--session-id", "3a28d128")
	if added.Source != "claude" {
		t.Errorf("source = %q, want claude", added.Source)
	}

	body, err := os.ReadFile(added.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"source: claude", "claude_session_name: milestone-1", "claude_session_id: 3a28d128"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the item file does not carry %q:\n%s", want, body)
		}
	}
}

// readSources opens every .go file in the module, for the cache's benefit
// only. Nothing is done with the contents.
func readSources() error {
	return filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "td-plugin") {
			return fs.SkipDir
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		_, err = os.ReadFile(path)
		return err
	})
}

// TestSourcesAreInTheCacheKey opens every source file the binary was built
// from, so that changing any of them invalidates this package's cached result.
//
// This suite links none of td. It shells out to go build in TestMain, which
// the cache cannot see, and cmd/td is package main and so cannot be imported
// by anything. Without this, a change anywhere in td leaves the cached PASS
// standing and the contract reports green without having run — which is the
// one result a contract suite must never give.
//
// It has to be a test rather than a step in TestMain, and that is the whole
// reason the first attempt at this changed nothing. The go command records
// only the files a test opens once m.Run is under way, so the identical read
// performed in TestMain is invisible to the cache. Moving it back there costs
// nothing visible: the suite still passes, and it silently stops guarding.
func TestSourcesAreInTheCacheKey(t *testing.T) {
	if err := readSources(); err != nil {
		t.Fatal(err)
	}
}

// TestMaintenanceCommands checks that each runs only its own epilogue step.
func TestMaintenanceCommands(t *testing.T) {
	c := newCLI(t)
	added := c.add("Item")
	before := c.commits()

	body, err := os.ReadFile(added.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(added.Path, append(body, []byte("\nBy hand.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	var res struct {
		Action   string `json:"action"`
		Epilogue struct {
			Bumped    []string `json:"bumped"`
			Committed bool     `json:"committed"`
		} `json:"epilogue"`
	}
	c.jsonOut(&res, "bump")
	if len(res.Epilogue.Bumped) != 1 {
		t.Errorf("td bump reported %v, want the hand edit", res.Epilogue.Bumped)
	}
	if res.Epilogue.Committed || c.commits() != before {
		t.Error("td bump committed; it must run only its own step")
	}

	c.jsonOut(&res, "commit")
	if !res.Epilogue.Committed || c.commits() != before+1 {
		t.Error("td commit did not commit what bump left behind")
	}

	// push with no remote is a no-op and not a failure.
	if _, stderr, code := c.run("push"); code != exitOK {
		t.Errorf("td push with no remote exited %d\n%s", code, stderr)
	}
}

// TestVersion checks the binary reports a version.
func TestVersion(t *testing.T) {
	c := newCLI(t)
	if out := c.ok("--version"); !strings.Contains(out, "td") {
		t.Errorf("td --version printed %q", out)
	}
}
