package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/vkovic/td/internal/epilogue"
)

// maintenanceResult is what a maintenance command reports.
type maintenanceResult struct {
	Action   string       `json:"action"`
	Epilogue epilogueView `json:"epilogue"`
}

// newMaintenanceCmds builds the four commands that each run one epilogue step
// on its own: bump, archive, commit and push. They exist so the parts of the
// shared tail can be driven by hand — and by the TUI, later.
func newMaintenanceCmds(a *app) []*cobra.Command {
	specs := []struct {
		use   string
		short string
		long  string
		step  epilogue.Step
	}{
		{
			use:   "bump",
			short: "Record edits made to item files outside td",
			long: "bump finds every item whose file has changed since td last wrote it and\n" +
				"brings its updated timestamp up to date, which is what moves a hand-edited\n" +
				"item back to the top of the list. It does not commit.",
			step: epilogue.StepBump,
		},
		{
			use:   "archive",
			short: "Sweep done items out of the list",
			long: "archive moves items completed longer ago than done_ttl_days into\n" +
				"archived/. It does not commit.",
			step: epilogue.StepArchive,
		},
		{
			use:   "commit",
			short: "Commit the store",
			long: "commit records the store's current state in git. It commits even when\n" +
				"auto_commit is off, since asking for it outright is the point. -m gives\n" +
				"the commit a message of your own instead of one describing the run.",
			step: epilogue.StepCommit,
		},
		{
			use:   "push",
			short: "Push the store to its remote",
			long: "push sends the store to its git remote, doing nothing when there is no\n" +
				"remote. It pushes even when auto_push is off, and a failure is reported\n" +
				"as a warning rather than failing the command.",
			step: epilogue.StepPush,
		},
	}

	cmds := make([]*cobra.Command, 0, len(specs))
	for _, spec := range specs {
		var message string
		cmd := &cobra.Command{
			Use:   spec.use,
			Short: spec.short,
			Long:  spec.long,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.maintenance(spec.use, spec.step, message)
			},
		}
		// Only commit takes a message: the other three do not write one, and a
		// -m they silently ignored would be worse than no flag at all.
		if spec.step == epilogue.StepCommit {
			cmd.Flags().StringVarP(&message, "message", "m", "", "commit message, instead of one describing the run")
		}
		cmds = append(cmds, cmd)
	}
	return cmds
}

// maintenance runs one epilogue step.
//
// Asking for a step outright overrides the setting that would otherwise skip
// it: td commit commits with auto_commit off, and td push pushes with auto_push
// off. A command you typed should do what it says.
func (a *app) maintenance(action string, step epilogue.Step, message string) error {
	switch step {
	case epilogue.StepCommit:
		a.cfg.AutoCommit = true
	case epilogue.StepPush:
		a.cfg.AutoPush = true
	}

	res, err := a.runEpilogue(step, message)
	if err != nil {
		return err
	}
	out := maintenanceResult{Action: action, Epilogue: newEpilogueView(res)}
	return a.out.Emit(out, func(w io.Writer) error {
		return writeMaintenance(w, action, res)
	})
}

// writeMaintenance says what a maintenance command did, in a line.
func writeMaintenance(w io.Writer, action string, res epilogue.Result) error {
	var line string
	switch action {
	case "bump":
		line = fmt.Sprintf("Recorded %s", plural(len(res.Bumped), "hand edit", "hand edits"))
	case "archive":
		line = fmt.Sprintf("Archived %s", plural(len(res.Archived), "item", "items"))
	case "commit":
		if !res.Committed {
			line = "Nothing to commit"
		} else {
			line = "Committed: " + res.Message
		}
	case "push":
		if !res.Pushed {
			line = "Nothing pushed"
		} else {
			line = "Pushed"
		}
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

// plural renders a count with the right noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
