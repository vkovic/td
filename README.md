# td

`td` is a tmux-native todo list driven by Claude Code. Ideas raised mid-session
currently die in scrollback; `td` gives each one a file, a pane, and a git
history. Items are plain markdown with YAML frontmatter under `~/.td/`, split
into a global scope and per-project scopes resolved from a `.td` marker file.
Every command commits its own change, so the store is always a readable git
repo you can edit by hand.

## Install

```
go install github.com/vkovic/td/cmd/td@latest
```

That puts `td` in `$(go env GOPATH)/bin`, which has to be on your `PATH`. From
a checkout, build it wherever you keep your own binaries instead:

```
go build -o ~/.local/bin/td ./cmd/td
```

## Requirements

- macOS or Linux. Windows is not supported: the store lock that serialises the
  commit-and-push tail every command runs is a `flock`, which has no
  implementation there, so anything that changes the store fails when it
  reaches that tail.
- `git` on your `PATH`. The store is a git repository and every command commits
  its own change.
- Go 1.24 or later, to install it.

## Build

```
go build ./cmd/td
./td --version
```

`td --version` names the build it came from: the tag for a binary installed
with `go install`, and the commit for one built from a checkout — with
`-dirty` when the tree had uncommitted changes.

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

The pane reads from both ends. Open items start at the top, the done section
sits against the bottom of the list with the `── done ──` rule over it, and the
status line and key legend hold the last two rows whatever the list is doing —
a short list leaves the gap in the middle rather than floating the footer up
the pane. Each section scrolls on its own, and open is served first: a listing
too long for the pane spends its height on open items and keeps the rule, which
is the one thing on screen saying there is a done section under it.

When rows are off screen the status line says where they went — `3 above,
8 between, 5 below`. "Between" is the fold at the rule: rows hidden where the
open section stops and the done one starts, which is neither end of the list.
Those counts are what makes the window honest, so a line too wide for the pane
drops the item total first, then elides the list's name, and only then gives up
its own tail.

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
| `g`        | pick the list to show: global, all, or a project       |
| `i`        | show or hide each item's id                            |
| `esc`      | clear the filters                                      |
| `r`        | record hand edits, sweep, commit and push now          |
| `?`        | show this help                                         |
| `q`        | quit                                                   |

### Referring to an item by its id

`i` puts each item's id on its row, ahead of the title, and `i` again takes it
away. The tag is the **last** three characters of the eight-character id, not the
first: an id encodes the millisecond it was minted, so its leading characters are
the high bits of that clock and every item created inside the same nine-hour
window shares them — a whole store's listing can read `64q`. The tail turns over
every millisecond, so it is the part that tells two items apart.

```
❯ [ ] s3r fuzzy search                              #cli  3d
  [ ] sxx ids for items for quick reference                5d
```

Those three characters are what you type back. Every command that takes an id —
`td show`, `td edit`, `td done`, `td rm` — accepts the whole id, any unique
leading prefix of it, or any unique trailing suffix, which is what makes the tag
on screen a thing you can paste into a `Bash` call or a message to Claude Code
without reading out all eight characters.

Three characters can still land on two items, and then `td done sxx` writes
nothing and exits 4, naming every candidate with its title so there is something
to choose between:

```
$ td done aaa
td: id matches more than one item: aaa matches 64qmbaaa (fix the backend), 64s3caaa (ship the release)
```

Pick the one you meant and run it again with more of its id. Resolution is judged
one form at a time — the whole id first, then prefixes, then suffixes — so a
reference that already worked cannot be made ambiguous by an id that merely ends
in it.

`$EDITOR` is the only editing surface. There is no preview pane and no in-place
field editing: `e` opens the item's markdown file, and whatever you leave behind
is the item. Quitting the editor without saving changes nothing.

`x`, `d`, `e` and `a` each end in the same tail the matching CLI command runs —
record hand edits, sweep the archive, commit, push — and honour `auto_commit`
and `auto_push` exactly as `td done`, `td rm`, `td edit` and `td add` do. `r` is
the exception: asking for the tail outright overrides both settings, the way
`td commit` commits with `auto_commit` off.

`g` opens the list picker: the global list, every scope merged, then every
project under `~/.td/`, by name and including the ones with nothing open. `j`
and `k` move, `⏎` picks, `esc` cancels and leaves the pane where it was. The
cursor opens on the list already showing, so `g` `⏎` changes nothing. That is
how you read another project's items without leaving the pane — `td ls -p acme`
from a second shell is the same list.

Picking is view state, like the filters. The pane files an `a` into the list on
screen, so an add while `acme` is showing lands in `acme`; the merged view is
the exception and keeps filing into the scope the directory resolved to, which
is where `td add` would have put it. A refresh keeps the picked list, and
starting `td` again opens on the marker's scope: nothing about the pick is
written down, so the pane and `td ls` never disagree about what "this project"
means.

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

## How the code is laid out

td has two surfaces and one place where an operation lives.

```
cmd/td             the cobra CLI            → task, epilogue, store, config, output, tui
internal/tui       the Bubble Tea pane      → task, epilogue, store, config
internal/task      what an operation does   → store
internal/epilogue  the tail every change runs → store, config
internal/store     items as files on disk   → gitx
internal/gitx      the git commands td runs
internal/config    config.toml and its TD_ overrides
internal/output    the CLI's --json and plain-text printer
internal/tdtest    fixtures, imported only from _test.go files
```

**A surface never reimplements an operation.** Adding, completing, removing,
restoring and editing an item all live in `internal/task`, and `cmd/td` and
`internal/tui` both call the same `task.Service`. A surface parses what the
person gave it — flags for the CLI, a keystroke for the pane — builds a request,
and renders what comes back. That is the whole of its job.

This is worth stating because the alternative is what td used to do. Every
operation was written twice, once in each surface, and the two copies were kept
in agreement by whoever remembered to change both. `commitMessage` had two
implementations whose comments each said they had to match the other's, and
`plural` had three.

So a new operation is a new file in `internal/task`, a method on `Service`
returning a `task.Result`, and a caller in each surface that wants it. A new
frontmatter field is a field on `store.Item` and a row in the `fields` table in
`internal/store/item.go` — plus the fixture in `TestSkeletonCarriesEveryKeyTdOwns`,
which fails loudly if you forget it.

**The service does not run the epilogue.** It changes the store and hands back
the entries and a commit message; the caller decides when the commit happens.
`cmd/td` runs the epilogue synchronously because it is about to exit.
`internal/tui` runs it as a `tea.Cmd`, off the event loop, because the epilogue
takes a blocking flock on the store — one a CLI process may be holding — and
waiting for it on the UI goroutine would freeze the pane. What the epilogue does
is shared; when it runs is each surface's own business.

**Time and identity come from the service, not from the package.** `task.Service`
carries the clock that stamps `created`, `updated` and `done_at`, and the
`store.IDGen` that mints ids. They are separate on purpose: stamps use
`store.Now`, which truncates to the second, because `Save` pins a file's mtime
to `Item.Updated` and a clock carrying nanoseconds leaves the two unequal —
which is exactly what td reads back as an edit made outside td. Ids encode a
millisecond, so they read an untruncated clock.

## License

MIT — see [LICENSE](LICENSE).
