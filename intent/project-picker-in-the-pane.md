# Intent: Project picker in the pane

**Author:** Vladimir Kovic · **Status:** draft

## Problem statement
The pane can show only three lists: the scope the `.td` marker resolved, the global list, and every scope merged. The `g` key cycles through them in that order (`internal/tui/help.go:61`, `internal/tui/filter.go:118`), and `ScopeMode` has exactly those three values (`internal/tui/model.go:28`). Another project's list is unreachable from the pane. To look at one you leave the pane and run `td ls -p <name>`, or open a second pane from that project's directory. The store already knows every project: `Store.Scopes()` returns global plus every project directory under `~/.td/` in name order (`internal/store/store.go:351`), and only the CLI's `-p` flag uses that reach (`cmd/td/root.go:131`).

## Proposed outcome
From one pane, you reach any list the store holds in a few keystrokes: global, every scope merged, or any single project, without leaving the pane or knowing the project's exact name. `g` becomes the one key that moves between lists, and the footer keeps naming the list on screen the way it does today (`internal/tui/render.go:435`).

## Affected users and systems
Users:
- Vladimir, working in a tmux split beside a Claude Code session, who today cannot see another project's items from the pane.
- Claude sessions writing through `td add` are unaffected. The store and the skill do not change.

Systems:
- `internal/tui`: the `g` binding in `help.go`, `cycleScope` in `filter.go`, `ScopeMode` and `currentScope` in `model.go`, `currentAddScope` in `editor.go:148`, and the overlay rendering in `render.go`, which already knows how to cover the list for help (`render.go:71`).
- `internal/store`: read only, through the existing `Scopes()` and `List()`.
- `README.md`: the key table, which `internal/tui/readme_test.go` checks against the help entries.
- `td-plugin/skills/td`: untouched. The skill shells out to the CLI, and the CLI does not change.

## Constraints
- `g` keeps its name in the key table. Its help text changes from "cycle the scope" to opening the picker, and the README row changes with it. `readme_test.go` today compares only the keys, not the description cell it already parses; it is extended to compare descriptions too, so a README row that goes stale fails the suite.
- The picker windows its rows and pins a closing hint at the bottom when it is taller than the pane, exactly as the help overlay does.
- A marker or `-p` flag can name a project whose directory does not exist yet. That scope is still a row in the picker, in name order among the projects, so the cursor has a list to open on and an add still goes where `td add` would put it.
- The title and tag filters carry over a pick unchanged, the way they survive every other reload today. `esc` clears them, as it does now.
- The picker lists exactly what `Store.Scopes()` returns plus the merged view: global, every scope, then every project directory by name, including a project with nothing open.
- The picker is an overlay in the same style as the help overlay: `j`/`k` or arrows move, enter picks, esc cancels. It fits a 60 column pane and windows its rows the way the help overlay does.
- An add while a picked project is on screen files into that project, the same rule the global view follows today. The merged view keeps filing into the directory's own scope.
- The picked project is view state, like the title and tag filters. A watcher refresh keeps it. A restart opens on the marker's scope, as today. Nothing is persisted.
- The CLI, the store layout, and the epilogue do not change.

## Success
- Pressing `g` in a pane opened from the `td` project shows a list naming global, all, and every project directory under `~/.td/`, with the cursor on the list currently shown.
- Picking a project shows that project's open items and the footer names it. Pressing `a` there files the item in that project's directory.
- Picking global, or all, behaves exactly as the old cycle did for those two.
- Esc in the picker leaves the list and the cursor where they were.
- `go test ./...` is green, including `readme_test.go` against the updated README row.

## Out of scope
- Persisting the picked project across restarts. Considered and left out: the pane and `td ls` would then disagree about what "this project" means.
- Creating a project from the pane. A project directory appears when `td add -p` or `td link` makes it.
- Open-item counts beside each project in the picker. Considered as a third listing option and left out for now.
- Any change to the CLI, the `-p` and `-g` flags, or the `/td` skill.
- A second key for the picker. `g` replaces the three-way cycle rather than sitting beside it.
