package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/config"
	"github.com/vkovic/td/internal/epilogue"
	"github.com/vkovic/td/internal/output"
	"github.com/vkovic/td/internal/store"
)

// globalFlags are the flags every subcommand accepts.
type globalFlags struct {
	global      bool
	project     string
	json        bool
	noEpilogue  bool
	source      string
	sessionName string
	sessionID   string
}

// app is what a subcommand is handed once the root has resolved the store, the
// configuration, and the scope this invocation acts on.
type app struct {
	flags globalFlags
	out   *output.Printer
	store *store.Store
	cfg   config.Config
	scope store.ScopeChoice

	// stdout and stderr are held so a subcommand can write outside the printer.
	stdout io.Writer
	stderr io.Writer
}

// Provenance is where an item came from, recorded on items the /td plugin
// creates so a todo can be traced back to the session that raised it.
type Provenance struct {
	Source      string
	SessionName string
	SessionID   string
}

// provenance reads the provenance flags.
func (a *app) provenance() Provenance {
	return Provenance{
		Source:      a.flags.source,
		SessionName: a.flags.sessionName,
		SessionID:   a.flags.sessionID,
	}
}

// runEpilogue performs the shared tail unless --no-epilogue was passed, and
// reports its warnings. The epilogue's own failures are returned; the failures
// it classes as warnings are printed and swallowed, as they must be.
func (a *app) runEpilogue(steps epilogue.Step, message string) (epilogue.Result, error) {
	if a.flags.noEpilogue {
		return epilogue.Result{}, nil
	}
	res, err := epilogue.Run(epilogue.Options{
		Store:   a.store,
		Config:  a.cfg,
		Steps:   steps,
		Message: message,
	})
	a.out.Warnings(res.Warnings)
	return res, err
}

// newRootCmd builds the command tree, writing to the given streams.
func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	a := &app{stdout: stdout, stderr: stderr}

	root := &cobra.Command{
		Use:   "td",
		Short: "A tmux-native todo list driven by Claude Code",
		Long: "td keeps todo items as plain markdown under ~/.td, split into a global\n" +
			"list and per-project lists resolved from a .td marker file. Every command\n" +
			"commits its change, so the store is always a readable git history.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// With no subcommand there is nothing to do yet: the TUI is a later
		// milestone, so print the help and leave.
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	f := &a.flags
	pf := root.PersistentFlags()
	pf.BoolVarP(&f.global, "global", "g", false, "act on the global list, ignoring any .td marker")
	pf.StringVarP(&f.project, "project", "p", "", "act on the named project list")
	pf.BoolVar(&f.json, "json", false, "emit JSON instead of a table")
	pf.BoolVar(&f.noEpilogue, "no-epilogue", false, "skip the bump, archive, commit and push tail")
	pf.StringVar(&f.source, "source", "", "record what created the item, such as claude")
	pf.StringVar(&f.sessionName, "session-name", "", "record the Claude Code session name on the item")
	pf.StringVar(&f.sessionID, "session-id", "", "record the Claude Code session id on the item")

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return a.setup()
	}

	root.AddCommand(newLinkCmd(a))
	return root
}

// setup resolves everything a subcommand needs: the printer, the store, the
// configuration, and which scope this invocation acts on.
func (a *app) setup() error {
	a.out = output.New(a.stdout, a.stderr, a.flags.json)

	root, err := store.DefaultRoot()
	if err != nil {
		return err
	}
	if a.store, err = store.Open(root); err != nil {
		return err
	}
	if a.cfg, err = config.Load(root); err != nil {
		return err
	}
	for _, key := range a.cfg.UnknownKeys {
		a.out.Warn(fmt.Errorf("unrecognized key %q in %s", key, config.FileName))
	}

	a.scope, err = store.ResolveScope(store.ScopeOptions{
		Global:  a.flags.global,
		Project: a.flags.project,
	})
	return err
}

// Execute runs the CLI and returns the process's exit code.
func Execute() int {
	root := newRootCmd(os.Stdout, os.Stderr)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "td:", err)
		return 1
	}
	return 0
}
