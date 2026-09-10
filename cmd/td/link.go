package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/store"
)

// linkResult is what td link reports, and the shape of its --json output.
type linkResult struct {
	Project  string `json:"project"`
	Marker   string `json:"marker"`
	ScopeDir string `json:"scope_dir"`
	StoreDir string `json:"store_dir"`
	// Created lists the paths this run brought into existence, so a second run
	// over the same directory reports an empty list.
	Created []string `json:"created"`
}

// newLinkCmd builds td link.
func newLinkCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "link [name]",
		Short: "Tie the current directory to a project list",
		Long: "link writes a .td marker in the current directory naming a project, and\n" +
			"creates that project's directory in the store. Items added from anywhere\n" +
			"at or below here then land in that list instead of the global one.\n\n" +
			"With no name, the current directory's own name is used.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return a.link(name)
		},
	}
}

// link creates the project's directory in the store and writes the marker that
// points at it.
func (a *app) link(name string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("locating the working directory: %w", err)
	}
	if name == "" {
		name = filepath.Base(dir)
	}
	name, err = store.CleanProjectName(name)
	if err != nil {
		return &usageError{err: err}
	}

	var created []string
	scopeDir := a.store.Dir(store.Scope(name), store.Active)
	if _, statErr := os.Stat(scopeDir); os.IsNotExist(statErr) {
		if err := os.MkdirAll(scopeDir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", scopeDir, err)
		}
		created = append(created, scopeDir)
	}

	marker := filepath.Join(dir, store.MarkerName)
	_, statErr := os.Stat(marker)
	markerIsNew := os.IsNotExist(statErr)
	marker, err = store.WriteMarker(dir, name)
	if err != nil {
		return err
	}
	if markerIsNew {
		created = append(created, marker)
	}

	res := linkResult{
		Project:  name,
		Marker:   marker,
		ScopeDir: scopeDir,
		StoreDir: a.store.Root(),
		Created:  created,
	}
	if res.Created == nil {
		res.Created = []string{}
	}
	return a.out.Emit(res, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "Linked %s to %s\nItems added here land in %s\n", dir, name, scopeDir)
		return err
	})
}
