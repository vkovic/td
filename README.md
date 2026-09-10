# td

`td` is a tmux-native todo list driven by Claude Code. Ideas raised mid-session
currently die in scrollback; `td` gives each one a file, a pane, and a git
history. Items are plain markdown with YAML frontmatter under `~/.td/`, split
into a global scope and per-project scopes resolved from a `.td` marker file.
Every command commits its own change, so the store is always a readable git
repo you can edit by hand.

## Status

Milestone 3 — the Bubble Tea TUI, on top of Milestone 1's binary and its CLI
subcommands and Milestone 2's `/td` Claude Code plugin.

## Build

```
go build ./cmd/td
./td --version
```

## Install the `/td` skill

The plugin lives in `td-plugin/`. A plugin skill is namespaced `plugin:skill`,
so installing `td-plugin` as a plugin would give you `/td:td`. To get the bare
`/td`, symlink the skill folder into your personal skills directory instead:

```
ln -sfn "$PWD/td-plugin/skills/td" ~/.claude/skills/td
```

Restart Claude Code, or start a new session, and `/td` appears in the slash
menu. The skill shells out to `td`, so the binary has to be on your `PATH`.

## The TUI

`td ui` opens the list in the terminal. Bare `td` does the same when it is
attached to a terminal on both ends, and prints its help anywhere else — in a
pipe, in a script, or in a Claude Code `Bash` call, none of which can drive a
full screen program.

The pane is meant to sit beside a Claude Code session, which is the split `td`
was written for:

```
tmux split-window -h -l 60 td
```

The list refreshes on its own. `td` watches the item directories under `~/.td/`
and re-reads within a second of anything changing them, so an item Claude adds
mid-session appears without you doing anything. A refresh only ever re-reads:
it never records a hand edit, sweeps, commits or pushes, because two panes
watching one store would otherwise drive each other in a loop.

### Keys

| Key        | Does                                                   |
| ---------- | ------------------------------------------------------ |
| `j` / `↓`  | move down                                              |
| `k` / `↑`  | move up                                                |
| `a`        | add an item, then open it in `$EDITOR`                 |
| `e` / `⏎`  | open the selected item in `$EDITOR`                    |
| `x`        | mark the selected item done, or reopen it              |
| `d`        | move the selected item to the trash                    |
| `/`        | filter by title                                        |
| `t`        | cycle the tag filter                                   |
| `g`        | cycle the scope: this project, global, all             |
| `esc`      | clear the filters                                      |
| `r`        | record hand edits, sweep, commit and push now          |
| `?`        | show the keys                                          |
| `q`        | quit                                                   |

`$EDITOR` is the only editing surface. There is no preview pane and no in-place
field editing: `e` opens the item's markdown file, and whatever you leave behind
is the item. Quitting the editor without saving changes nothing.

`x`, `d`, `e` and `a` each end in the same tail the matching CLI command runs —
record hand edits, sweep the archive, commit, push — and honour `auto_commit`
and `auto_push` exactly as `td done`, `td rm`, `td edit` and `td add` do. `r` is
the exception: asking for the tail outright overrides both settings, the way
`td commit` commits with `auto_commit` off.

The merged view labels every row with the list it came from, the way
`td ls --all` adds a `SCOPE` column, since that is the one view where two rows
next to each other can belong to different lists.

Two notes on the filters. They are view state, so a refresh arriving while one
is open leaves it, and the cursor, where they were. And `t` does not hide done
items the way `td ls -t` does: the CLI composes its filter with an explicit
`--done`, while the pane always shows both sections, so a tag only completed
items carry shows those items under the done rule rather than emptying the
screen for a reason nothing on screen explains.

## Configuration

`~/.td/config.toml` is written on first run with every key commented at its
default. Each one can be overridden for a single run by its `TD_` prefixed
environment variable.

| Key              | Default | Environment      |
| ---------------- | ------- | ---------------- |
| `done_ttl_days`  | `7`     | `TD_DONE_TTL_DAYS` |
| `auto_commit`    | `true`  | `TD_AUTO_COMMIT` |
| `auto_push`      | `true`  | `TD_AUTO_PUSH`   |
| `editor`         | unset   | `TD_EDITOR`      |

The editor is resolved in that order: `TD_EDITOR`, then `editor` in
`config.toml`, then `$VISUAL`, then `$EDITOR`, and `vi` if none of them is set.
The environment variable beating the file is the rule every key here follows,
not a quirk of this one. The setting may carry arguments — `editor = "code
--wait"` and `emacsclient -nw` both work — and is split into words the way a
shell splits a command line, honouring quotes and backslashes. Nothing is
expanded, so an editor setting cannot run a substitution.

## License

MIT — see [LICENSE](LICENSE).
