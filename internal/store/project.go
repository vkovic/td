package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MarkerName is the file that ties a working directory to a project scope. It
// is a regular file holding the project's name; the store's own ~/.td is a
// directory, so it is never mistaken for one.
const MarkerName = ".td"

// ErrScopeConflict is returned when both the global and project flags are set.
var ErrScopeConflict = errors.New("cannot use the global and project flags together")

// Reason records why a scope was chosen, so a command can explain itself and
// --json can report it.
type Reason string

const (
	// ReasonFlagGlobal means the global flag forced the global scope.
	ReasonFlagGlobal Reason = "flag-global"
	// ReasonFlagProject means the project flag named the scope outright.
	ReasonFlagProject Reason = "flag-project"
	// ReasonMarker means a .td file at or above the working directory named it.
	ReasonMarker Reason = "marker"
	// ReasonDefault means no marker was found, so the global scope applies.
	ReasonDefault Reason = "default"
)

// ScopeChoice is a resolved scope together with how it was arrived at.
type ScopeChoice struct {
	Scope  Scope
	Reason Reason
	// Marker is the path of the .td file that decided it, empty otherwise.
	Marker string
}

// ScopeOptions are the inputs to ResolveScope: the working directory to search
// from and the two flags that can override the search.
type ScopeOptions struct {
	// Dir is where the upward search starts. Empty means the process's working
	// directory.
	Dir string
	// Home stops the upward search. Empty means the user's home directory.
	Home string
	// Global forces the global scope, ignoring any marker.
	Global bool
	// Project names a scope outright, ignoring any marker.
	Project string
}

// ResolveScope decides which scope a command acts on: the project flag wins,
// then the global flag, then a .td marker at or above the working directory,
// and the global scope when there is none.
func ResolveScope(opts ScopeOptions) (ScopeChoice, error) {
	if opts.Global && opts.Project != "" {
		return ScopeChoice{}, ErrScopeConflict
	}
	if opts.Project != "" {
		name, err := CleanProjectName(opts.Project)
		if err != nil {
			return ScopeChoice{}, err
		}
		return ScopeChoice{Scope: Scope(name), Reason: ReasonFlagProject}, nil
	}
	if opts.Global {
		return ScopeChoice{Scope: Global, Reason: ReasonFlagGlobal}, nil
	}

	dir, err := resolveStartDir(opts.Dir)
	if err != nil {
		return ScopeChoice{}, err
	}
	home, err := resolveHome(opts.Home)
	if err != nil {
		return ScopeChoice{}, err
	}

	marker, name, err := FindMarker(dir, home)
	if err != nil {
		return ScopeChoice{}, err
	}
	if marker == "" {
		return ScopeChoice{Scope: Global, Reason: ReasonDefault}, nil
	}
	return ScopeChoice{Scope: Scope(name), Reason: ReasonMarker, Marker: marker}, nil
}

// FindMarker walks up from dir looking for a .td file, returning its path and
// the project name it holds. The walk stops after checking home and after
// checking the filesystem root, so it never escapes the user's own tree.
//
// No marker found is not an error: the path comes back empty.
func FindMarker(dir, home string) (path, name string, err error) {
	dir = filepath.Clean(dir)
	home = filepath.Clean(home)
	for {
		candidate := filepath.Join(dir, MarkerName)
		info, statErr := os.Stat(candidate)
		switch {
		case statErr == nil && info.Mode().IsRegular():
			name, err := ReadMarker(candidate)
			if err != nil {
				return "", "", err
			}
			return candidate, name, nil
		case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
			return "", "", fmt.Errorf("checking for %s: %w", candidate, statErr)
		}

		parent := filepath.Dir(dir)
		if dir == home || parent == dir {
			return "", "", nil
		}
		dir = parent
	}
}

// ReadMarker reads the project name out of a .td file. Blank lines and #
// comments are skipped; an otherwise empty marker takes its name from the
// directory holding it, so `touch .td` is enough to link a repo.
func ReadMarker(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, err := CleanProjectName(line)
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		return name, nil
	}
	return CleanProjectName(filepath.Base(filepath.Dir(path)))
}

// WriteMarker records a project name in a .td file in dir, replacing any marker
// already there.
func WriteMarker(dir, name string) (string, error) {
	name, err := CleanProjectName(name)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, MarkerName)
	contents := "# td project scope: items created here land in ~/.td/" + name + "/\n" + name + "\n"
	if err := writeAtomic(path, []byte(contents)); err != nil {
		return "", err
	}
	return path, nil
}

// CleanProjectName validates a project name and returns it trimmed. A name is
// one directory under the store root, so anything that could escape it — a
// separator, a parent reference, a leading dot — is rejected rather than
// quietly rewritten.
func CleanProjectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", errors.New("project name is empty")
	case name == "." || name == "..":
		return "", fmt.Errorf("invalid project name %q", name)
	case strings.HasPrefix(name, "."):
		return "", fmt.Errorf("invalid project name %q: cannot start with a dot", name)
	case strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0):
		return "", fmt.Errorf("invalid project name %q: cannot contain a path separator", name)
	case Area(name) == Archived || Area(name) == Deleted:
		return "", fmt.Errorf("invalid project name %q: reserved by the store", name)
	}
	return name, nil
}

// resolveStartDir returns dir, or the process's working directory when empty.
func resolveStartDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locating the working directory: %w", err)
	}
	return wd, nil
}

// resolveHome returns home, or the user's home directory when empty.
func resolveHome(home string) (string, error) {
	if home != "" {
		return home, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return h, nil
}
