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

	// stdout and stderr are held so a subcommand can write outside the printer,
	// and in is held so --body-file - can be driven by a test.
	stdout io.Writer
	stderr io.Writer
	in     io.Reader
}

// stdin is where --body-file - reads from.
func (a *app) stdin() io.Reader {
	if a.in != nil {
		return a.in
	}
	return os.Stdin
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

// newRootCmd builds the command tree, writing to the given streams and reading
// standard input from the process.
func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	return newRootCmdIO(stdout, stderr, os.Stdin)
}

// newRootCmdIO builds the command tree over explicit streams, so a test can
// drive --body-file - without touching the process's own.
func newRootCmdIO(stdout, stderr io.Writer, stdin io.Reader) *cobra.Command {
	a := &app{stdout: stdout, stderr: stderr, in: stdin}

	root := &cobra.Command{
		Use:   "td",
		Short: "A tmux-native todo list driven by Claude Code",
		Long: "td keeps todo items as plain markdown under ~/.td, split into a global\n" +
			"list and per-project lists resolved from a .td marker file. Every command\n" +
			"commits its change, so the store is always a readable git history.",
		Version:       resolvedVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		// With no subcommand, a terminal gets the TUI and anything else gets
		// the help. Bare td is the everyday way into the pane, but it is also
		// what a script or a Claude Code Bash call runs, and neither of those
		// can drive a full screen program.
		RunE: func(cmd *cobra.Command, args []string) error {
			if interactive() {
				return a.ui()
			}
			return cmd.Help()
		},
		// Cobra's own handling of an unrecognized subcommand produces a plain
		// error; naming it here makes it classify as usage, like every other
		// command line mistake.
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usagef("unknown command %q for %q", args[0], cmd.CommandPath())
			}
			return nil
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

	root.AddCommand(
		newLinkCmd(a),
		newAddCmd(a),
		newEditCmd(a),
		newDoneCmd(a),
		newUndoCmd(a),
		newRemoveCmd(a),
		newRestoreCmd(a),
		newLsCmd(a),
		newShowCmd(a),
		newUICmd(a),
	)
	root.AddCommand(newMaintenanceCmds(a)...)

	markUsageErrors(root)
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
	// ExecuteC hands back the command that failed, so a usage error can print
	// the usage of the subcommand that was actually typed.
	cmd, err := root.ExecuteC()
	if err == nil {
		return exitOK
	}
	fmt.Fprintln(os.Stderr, "td:", err)
	code := exitCode(err)
	if code == exitUsage && cmd != nil {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, cmd.UsageString())
	}
	return code
}
