package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/tui"
)

// newUICmd builds td ui, the terminal interface.
func newUICmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Open the terminal interface",
		Long: "ui opens td's list in the terminal, refreshed as the store changes.\n" +
			"It is what bare td runs in a terminal, and is meant for a tmux pane\n" +
			"beside a Claude Code session.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ui()
		},
	}
}

// ui runs the terminal interface over the store, configuration and scope the
// root already resolved, so the pane lists exactly what td ls would have here.
func (a *app) ui() error {
	m, err := tui.New(tui.Options{
		Store:  a.store,
		Config: a.cfg,
		Scope:  a.scope,
		Watch:  true,
	})
	if err != nil {
		return err
	}
	defer m.Close()
	// The alternate screen keeps the list off the scrollback: the pane is a
	// view of the store, not a transcript of one.
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return fmt.Errorf("running the terminal interface: %w", err)
	}
	return nil
}

// interactive reports whether this process is attached to a terminal on both
// ends. Bare td opens the TUI only when it is, so td in a pipe, in a script or
// in a Claude Code Bash call keeps printing its help.
//
// The check reads the process's own descriptors rather than a.stdout, because a
// test drives the root over buffers and must take the non-terminal branch.
func interactive() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}
