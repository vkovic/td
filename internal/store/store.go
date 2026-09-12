// Package store is td's persistence layer: the ~/.td directory, the markdown
// files with YAML frontmatter that are the items, and the scopes and areas
// those files are filed under.
//
// The store is the only thing that reads or writes an item. Every surface —
// the CLI, the terminal interface, and the /td skill through the CLI — goes
// through it, which is what keeps them from disagreeing about what an item is.
// A file is the record: an item's location on disk says which list and which
// area it belongs to, and nothing else records that.
package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vkovic/td/internal/gitx"
)

// EnvRoot overrides the store's location, which is otherwise ~/.td. Tests and
// scripted runs set it to work against a throwaway directory.
const EnvRoot = "TD_ROOT"

// ext is the file extension every item carries.
const ext = ".md"

// untitled is the filename fragment used when a title has nothing sluggable in
// it, so a file is never named by its id and a bare hyphen.
const untitled = "untitled"

var (
	// ErrNotFound is returned when no item matches an id, prefix or suffix.
	ErrNotFound = errors.New("no item matches that id")
	// ErrAmbiguous is returned when a partial id matches more than one item.
	ErrAmbiguous = errors.New("id matches more than one item")
)

// Area is the part of a scope's directory an item file sits in. An item is
// filed under exactly one of them at a time; moving between them is a rename.
type Area string

const (
	// Active holds live items, in the scope directory itself.
	Active Area = ""
	// Archived holds items swept out after their done TTL elapsed.
	Archived Area = "archived"
	// Deleted holds removed items. It is gitignored, so trash never reaches a
	// remote, and nothing purges it.
	Deleted Area = "deleted"
)

// Scope is a store partition: the empty Scope is the global list, and any other
// value names a project directory under the root.
type Scope string

// Global is the scope items land in when no project applies.
const Global Scope = ""

// IsGlobal reports whether s is the global scope.
func (s Scope) IsGlobal() bool { return s == Global }

// String renders a scope for display, naming the global scope rather than
// showing an empty column.
func (s Scope) String() string {
	if s.IsGlobal() {
		return "global"
	}
	return string(s)
}

// Ref locates one item file: which scope and area it is filed under, and its
// path on disk.
type Ref struct {
	Scope Scope
	Area  Area
	Path  string
}

// Entry pairs a parsed item with where it was read from.
type Entry struct {
	Item *Item
	Ref  Ref
}

// Store is a td store rooted at a directory, normally ~/.td.
type Store struct {
	root string
}

// DefaultRoot returns the store directory: $TD_ROOT when set, else ~/.td.
func DefaultRoot() (string, error) {
	if r := os.Getenv(EnvRoot); r != "" {
		return filepath.Clean(r), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory for the td store: %w", err)
	}
	return filepath.Join(home, ".td"), nil
}

// Open prepares the store at root, creating what is missing: the directory
// itself, a commented config.toml, a .gitignore that keeps deleted/ out of git,
// and a git repository. Passing an empty root uses DefaultRoot. Open is
// idempotent — an existing store is left untouched.
func Open(root string) (*Store, error) {
	if root == "" {
		var err error
		if root, err = DefaultRoot(); err != nil {
			return nil, err
		}
	}
	s := &Store{root: filepath.Clean(root)}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, fmt.Errorf("creating td store at %s: %w", s.root, err)
	}
	if err := writeIfMissing(filepath.Join(s.root, "config.toml"), defaultConfigTemplate); err != nil {
		return nil, err
	}
	if err := writeIfMissing(filepath.Join(s.root, ".gitignore"), storeGitignore); err != nil {
		return nil, err
	}
	if err := s.ensureRepo(); err != nil {
		return nil, err
	}
	return s, nil
}

// Root is the store's directory.
func (s *Store) Root() string { return s.root }

// LockName is the file the epilogue takes an exclusive lock on, so a CLI
// command and the TUI cannot race on the git index.
const LockName = ".td.lock"

// LockPath is the store's lock file.
func (s *Store) LockPath() string { return filepath.Join(s.root, LockName) }

// storeGitignore keeps trash and the lock file out of git: deleted items stay
// on the machine that removed them and never reach a remote.
const storeGitignore = "**/deleted/\n" + LockName + "\n"

// defaultConfigTemplate documents every key with its value commented out, so
// the defaults live in one place — internal/config — and this file never drifts
// from them.
const defaultConfigTemplate = `# td configuration. Every key is optional; the commented value is the default.
# Each key can be overridden for one run by its TD_ prefixed environment variable.

# Days a done item stays in the list before it is swept into archived/.
# done_ttl_days = 7

# Commit every change to the store's git repository.
# auto_commit = true

# Push after a change when the repository has a remote.
# auto_push = true

# Editor used to open an item's body. Falls back to $EDITOR.
# editor = ""
`

// Repo is the store's git repository.
func (s *Store) Repo() *gitx.Repo { return gitx.New(s.root) }

// ensureRepo creates the store's git repository when it is not there yet.
func (s *Store) ensureRepo() error {
	return s.Repo().Init()
}

// Dir is the directory holding a scope's items in the given area.
func (s *Store) Dir(scope Scope, area Area) string {
	parts := []string{s.root}
	if !scope.IsGlobal() {
		parts = append(parts, string(scope))
	}
	if area != Active {
		parts = append(parts, string(area))
	}
	return filepath.Join(parts...)
}

// FileName is the name an item is stored under: its id, a hyphen, and its
// slugged title.
func FileName(it *Item) string {
	slug := Slug(it.Title)
	if slug == "" {
		slug = untitled
	}
	return it.ID + "-" + slug + ext
}

// Save writes an item into a scope and area, replacing any file already holding
// that id there — including one whose name no longer matches, so renaming an
// item's title does not leave the old file behind.
//
// The write is a temp file in the destination directory followed by a rename,
// so a reader never sees a half-written item and a crash never truncates one.
func (s *Store) Save(scope Scope, area Area, it *Item) (Ref, error) {
	if it.ID == "" {
		return Ref{}, errors.New("cannot save an item with no id")
	}
	dir := s.Dir(scope, area)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Ref{}, fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, FileName(it))

	body, err := it.Marshal()
	if err != nil {
		return Ref{}, err
	}
	if err := writeAtomic(path, body); err != nil {
		return Ref{}, err
	}
	// Pin the file's modification time to the item's own updated timestamp.
	// That is what lets the bump detector tell a hand edit — which moves mtime
	// past updated — from td's own writes, which leave the two equal.
	if !it.Updated.IsZero() {
		if err := os.Chtimes(path, time.Time{}, it.Updated); err != nil {
			return Ref{}, fmt.Errorf("setting the modification time of %s: %w", path, err)
		}
	}

	// Drop a stale file for the same id left by a previous title.
	stale, err := s.pathsForID(dir, it.ID)
	if err != nil {
		return Ref{}, err
	}
	for _, p := range stale {
		if p == path {
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Ref{}, fmt.Errorf("removing the previous file for %s: %w", it.ID, err)
		}
	}
	return Ref{Scope: scope, Area: area, Path: path}, nil
}

// Now is the clock every surface stamps an item with, and the truncation is
// load-bearing rather than cosmetic. Marshal writes updated as whole-second
// RFC 3339, while Save pins the file's mtime to the in-memory value. A clock
// carrying nanoseconds therefore leaves a file whose mtime is a fraction of a
// second past the updated it reads back as — which is precisely the condition
// HandEdited defines as an edit made outside td, so td would report its own
// write as somebody else's.
//
// UTC for the same reason ordering is textual: two machines write the same
// bytes for the same instant.
func Now() time.Time { return time.Now().UTC().Truncate(time.Second) }

// HandEdited reports whether a file has been changed outside td since the item
// in it was last written: its modification time has moved past the updated
// timestamp in its own frontmatter. td's own writes pin the two together, so
// this is true only for an edit td did not make.
//
// An item with no updated timestamp has never been written by td and counts as
// hand edited, so it gets one.
func HandEdited(e Entry) (bool, error) {
	info, err := os.Stat(e.Ref.Path)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", e.Ref.Path, err)
	}
	if e.Item.Updated.IsZero() {
		return true, nil
	}
	return info.ModTime().After(e.Item.Updated), nil
}

// Load reads and parses one item file.
func (s *Store) Load(path string) (*Item, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	it, err := ParseItem(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if it.ID == "" {
		return nil, fmt.Errorf("%s: item has no id in its frontmatter", path)
	}
	return it, nil
}

// Move relocates an item to another scope and area, rewriting its file name
// from the current title. The source file is removed once the destination is
// written, so an interrupted move leaves a copy rather than nothing.
func (s *Store) Move(from Ref, it *Item, toScope Scope, toArea Area) (Ref, error) {
	to, err := s.Save(toScope, toArea, it)
	if err != nil {
		return Ref{}, err
	}
	if to.Path == from.Path {
		return to, nil
	}
	if err := os.Remove(from.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Ref{}, fmt.Errorf("removing %s after moving it to %s: %w", from.Path, to.Path, err)
	}
	return to, nil
}

// List reads every item in one scope. With no areas given it reads Active only.
//
// A file that will not parse is skipped rather than failing the whole listing —
// the store is meant to be hand-edited, and one bad file must not block the
// commands that would fix it. Those failures come back as a joined error
// alongside the entries that did parse, for the caller to report as warnings.
func (s *Store) List(scope Scope, areas ...Area) ([]Entry, error) {
	if len(areas) == 0 {
		areas = []Area{Active}
	}
	var entries []Entry
	var problems []error
	for _, area := range areas {
		dir := s.Dir(scope, area)
		names, err := itemFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			path := filepath.Join(dir, name)
			it, err := s.Load(path)
			if err != nil {
				problems = append(problems, err)
				continue
			}
			entries = append(entries, Entry{Item: it, Ref: Ref{Scope: scope, Area: area, Path: path}})
		}
	}
	return entries, errors.Join(problems...)
}

// ListAll reads every scope in the store, global first and then each project in
// name order. Its error behaves as List's does.
func (s *Store) ListAll(areas ...Area) ([]Entry, error) {
	scopes, err := s.Scopes()
	if err != nil {
		return nil, err
	}
	var entries []Entry
	var problems []error
	for _, scope := range scopes {
		got, err := s.List(scope, areas...)
		if err != nil {
			problems = append(problems, err)
		}
		entries = append(entries, got...)
	}
	return entries, errors.Join(problems...)
}

// Scopes lists the store's scopes: the global scope, then every project
// directory, in name order.
func (s *Store) Scopes() ([]Scope, error) {
	ents, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("reading the td store at %s: %w", s.root, err)
	}
	// os.ReadDir returns entries sorted by name, so the projects come out in
	// name order with no further sorting.
	scopes := []Scope{Global}
	for _, e := range ents {
		if !e.IsDir() || !isProjectDir(e.Name()) {
			continue
		}
		scopes = append(scopes, Scope(e.Name()))
	}
	return scopes, nil
}

// isProjectDir reports whether a directory under the root is a project scope
// rather than one of the store's own directories.
func isProjectDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	return Area(name) != Archived && Area(name) != Deleted
}

// Resolve finds the one item in a scope whose id begins with prefix, searching
// the given areas — Active only when none are named.
func (s *Store) Resolve(prefix string, scope Scope, areas ...Area) (Entry, error) {
	entries, err := s.List(scope, areas...)
	if err != nil && len(entries) == 0 {
		return Entry{}, err
	}
	return ResolveRef(prefix, entries)
}

// ResolveAll is Resolve across every scope, for the commands that take a bare
// id and should find it wherever it lives.
func (s *Store) ResolveAll(prefix string, areas ...Area) (Entry, error) {
	entries, err := s.ListAll(areas...)
	if err != nil && len(entries) == 0 {
		return Entry{}, err
	}
	return ResolveRef(prefix, entries)
}

// ResolveRef picks the single candidate whose id matches ref. Three forms are
// tried in turn, and the first that matches anything decides: the whole id, a
// leading prefix of it, then a trailing suffix. Each round is judged on its own
// — a ref matching one item by prefix is that item even where several ids end
// in it — so widening the match can never make a reference that already worked
// ambiguous.
//
// The suffix round is what makes a ShortID typeable. It is the form the TUI
// prints, and an id's leading characters are too nearly identical across a
// store to reference by (see ShortID).
//
// A round matching several items is an error naming each id and its title, so
// the reader has something to choose between; that is distinct from matching
// none.
func ResolveRef(ref string, candidates []Entry) (Entry, error) {
	if ref == "" {
		return Entry{}, fmt.Errorf("%w: empty id", ErrNotFound)
	}
	ref = strings.ToLower(ref)

	var prefixed, suffixed []Entry
	for _, e := range candidates {
		id := strings.ToLower(e.Item.ID)
		switch {
		case id == ref:
			return e, nil
		case strings.HasPrefix(id, ref):
			prefixed = append(prefixed, e)
		case strings.HasSuffix(id, ref):
			suffixed = append(suffixed, e)
		}
	}
	for _, matches := range [][]Entry{prefixed, suffixed} {
		switch len(matches) {
		case 0:
			continue
		case 1:
			return matches[0], nil
		default:
			return Entry{}, fmt.Errorf("%w: %s matches %s", ErrAmbiguous, ref, describeMatches(matches))
		}
	}
	return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, ref)
}

// describeMatches lists what an ambiguous ref hit, in id order, each id with the
// title that tells it apart. A bare list of ids says only that the reader must
// guess again; the titles are how they pick without opening all of them.
func describeMatches(matches []Entry) string {
	described := make([]string, len(matches))
	for i, m := range matches {
		described[i] = m.Item.ID + " (" + m.Item.Title + ")"
	}
	sort.Strings(described)
	return strings.Join(described, ", ")
}

// pathsForID returns the files in dir holding the given id, matching on the
// name's id prefix so a file renamed by a title change is still found.
func (s *Store) pathsForID(dir, id string) ([]string, error) {
	names, err := itemFiles(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, name := range names {
		if name == id+ext || strings.HasPrefix(name, id+"-") {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths, nil
}

// itemFiles lists the item file names in dir, in name order — which is id
// order, and so creation order. A missing directory is an empty listing, not an
// error: a scope exists as soon as something is filed in it.
func itemFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || filepath.Ext(e.Name()) != ext {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// writeAtomic writes data to path through a temp file in the same directory,
// renaming it into place. The temp file is removed on every failure path, so a
// failed save never leaves debris beside the items.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".td-*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temporary file in %s: %w", dir, err)
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("syncing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("setting permissions on %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s into place at %s: %w", tmp, path, err)
	}
	return nil
}

// writeIfMissing creates a file with the given contents, leaving an existing
// file alone.
func writeIfMissing(path, contents string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	return nil
}
