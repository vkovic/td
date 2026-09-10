# The `td` CLI contract

Every command below is real: this file was reconciled against the built binary,
not against the spec. `td --help` and `td <cmd> --help` are the live authority
if the two ever disagree.

## Scope: which list a command acts on

`td` walks up from the current directory looking for a `.td` marker file. Found
→ that project's list at `~/.td/<name>/`. Not found → the global list at the
root of `~/.td/`. This is the whole scope rule, and it is why running `td` from
the project directory is normally all the scoping a caller needs.

- `-g`, `--global` — force the global list, ignoring any `.td` marker.
- `-p <name>`, `--project <name>` — force a named project's list.

Passing both is a usage error (exit 2).

## Global flags

Accepted by every subcommand.

| Flag | Effect |
| --- | --- |
| `--json` | Emit JSON on stdout instead of a table |
| `-g`, `--global` | Act on the global list |
| `-p <name>`, `--project <name>` | Act on the named project list |
| `--source <what>` | Record what created the item, e.g. `claude` |
| `--session-name <name>` | Record the Claude Code session name on the item |
| `--session-id <id>` | Record the Claude Code session id on the item |
| `--no-epilogue` | Skip the bump, archive, commit and push tail |

`--source`, `--session-name` and `--session-id` are stored by `td add` only;
every other command accepts and ignores them.

## Commands

```
td link [name]
td add <title>... [-b <md> | --body-file <path>|-] [-t <tag>]... [--due YYYY-MM-DD]
td ls [--done] [--all] [-t <tag>]...
td show <id>
td edit <id>... [--title <s>] [-b <md> | --body-file <path>|-] [--append <s>] [-t <tag>]... [--due YYYY-MM-DD|""]
td done <id>...
td undo <id>...
td rm <id>...
td restore <id>...
td bump
td archive
td commit
td push
td ui
```

**`td link [name]`** writes a `.td` marker in the current directory naming a
project and creates that project's directory in the store. With no name, the
current directory's own name is used.

**`td add <title>...`** creates an item in the applicable list. The words after
`add` become the title, so `td` itself needs no quoting — but a caller invoking
it through a shell should always single-quote the title anyway, because the
shell expands `$` and backticks and chokes on an apostrophe before `td` sees the
argument. A title cannot begin with `-`: it is parsed as a flag, and `--` does
not help, since everything after it including the provenance flags is swallowed
into the title. `-b` takes a markdown body
inline; `--body-file -` reads it from standard input, which is how a long or
multi-line body is passed safely. `-t` is repeatable.

**`td ls`** lists open items, most recently updated first, with `created` and
then `id` breaking a tie so the order is stable run to run. `--done` includes
done items, which sort after the open ones, most recently completed first.
`--all` merges every scope and adds a scope column. `-t` narrows to items
carrying **every** tag given, matched case-insensitively, so several `-t` flags
narrow the list rather than widening it. Aliased as `td list`.

**`td show <id>`** prints one item's fields and its full markdown body. It finds
the item whether it is live, archived, or in the trash.

**`td edit <id>...`** changes only the fields named. `-t` *replaces* the item's
tags rather than adding to them, and `--due ""` clears the due date. `--append`
adds a paragraph to the body instead of replacing it.

**`td done`** stamps `done_at`, which is what makes an item done; **`td undo`**
clears it. **`td rm`** moves items to `deleted/`, which is gitignored and never
purged; **`td restore`** brings them back from `deleted/` or `archived/`.

**`td bump`**, **`td archive`**, **`td commit`** and **`td push`** each run only
their own step of the epilogue, on demand. A caller capturing todos has no
reason to run any of them — the epilogue already does.

**`td ui`** opens the terminal interface, and is what bare `td` runs **in a
terminal**. Anywhere else — a pipe, a script, a Claude Code Bash call — bare
`td` prints help instead, because the branch tests whether stdin and stdout are
terminals. A caller like this skill therefore never reaches the TUI, and should
never invoke `td ui`: it takes over the terminal and does not return output to
parse.

`td commit` takes no `-m`.

## Ids

An id is 8 lowercase base32 characters. **Any unique prefix works** wherever an
id is expected, so `td done 64n9gq` is fine when only one item starts that way.
Every command that takes an id takes one or more.

## The epilogue

Every command except `bump`, `archive`, `commit` and `push` ends with the same
tail: bump hand-edited items' `updated` timestamps, sweep expired done items
into `archived/`, `git add -A`, commit if dirty, push if a remote is set. This
is why the store is always a readable git history and why a file edited by hand
in an editor is picked up by the *next* `td` command. `--no-epilogue` skips it.

`td ls` and `td show` run the epilogue with the push disabled, so a slow remote
never sits inside a turn.

## JSON output

`--json` writes one object to stdout. Warnings and errors go to stderr as plain
text, so stdout stays parseable.

**`td ls`** → `{"scope": …, "items": [item…], "epilogue": {…}}` where `scope` is
the scope name, `global`, or `all`.

**`td show`** → `{"item": {…}, "epilogue": {…}}`.

**Mutating commands** (`add`, `edit`, `done`, `undo`, `rm`, `restore`) →
`{"action": …, "items": [item…], "epilogue": {…}}`. `action` is the command's
name, except that `td rm` reports `"remove"` — and that string is load-bearing
rather than cosmetic: `cmd/td/mutate.go:40` tests `action != "remove"` to decide
whether item bodies are included, which is why `td rm` is the one mutating
command whose items carry no `body`.

**Maintenance commands** (`bump`, `archive`, `commit`, `push`) →
`{"action": …, "epilogue": {…}}`, with no `items`.

**`td link`** → `{"project", "marker", "scope_dir", "store_dir", "created": […]}`
and no epilogue.

### The item object

| Field | Type | Notes |
| --- | --- | --- |
| `id` | string | 8-char base32 |
| `title` | string | |
| `tags` | array of string | `[]` when untagged, never absent |
| `due` | string | `YYYY-MM-DD`; **absent** when unset |
| `created` | string | RFC 3339 UTC |
| `updated` | string | RFC 3339 UTC; the primary `ls` sort key |
| `done_at` | string or null | `null` when open — this is the done flag of record |
| `done` | bool | Convenience mirror of `done_at != null` |
| `source` | string | `claude`, `tui`, `cli`; who *created* the item, never who last touched it, since only `td add` writes it; **absent** when unset |
| `context` | string | cwd at creation; **absent** when unset, which today is always |
| `claude_session_name` | string | **absent** when unset |
| `claude_session_id` | string | **absent** when unset |
| `scope` | string | project name, or `global` |
| `area` | string | `active`, `archived`, or `deleted` |
| `path` | string | absolute path to the item's `.md` file |
| `body` | string | markdown body; **absent** when empty |

Every field marked *absent* is omitted entirely rather than emitted as null or
`""` — read defensively.

`body` is present on `td show` and on `add`, `edit`, `done`, `undo` and
`restore`. **Neither `td ls` nor `td rm` returns `body`**; use `td show <id>`
when the body is needed — `show` finds an item in the trash, so it still works
after an `rm`.

### The epilogue object

| Field | Type | Notes |
| --- | --- | --- |
| `bumped` | array of string | ids whose `updated` the bump refreshed |
| `archived` | array of string | ids the sweep moved to `archived/` |
| `committed` | bool | whether a commit was made |
| `message` | string | the commit message; absent when nothing was committed |
| `pushed` | bool | |
| `warnings` | array of string | non-fatal problems, e.g. a failed push |

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | The store or git could not do what was asked |
| 2 | The command line was wrong: unknown flag, missing argument, `-g` with `-p` |
| 3 | No item matched the id given |
| 4 | An id prefix matched more than one item — the message lists the candidates |

Exit 4 is recoverable without asking the user: the message names every matching
id, so a longer prefix or the full id can be retried straight away.

## Configuration

`~/.td/config.toml`, every key overridable by an environment variable:
`TD_DONE_TTL_DAYS`, `TD_AUTO_COMMIT`, `TD_AUTO_PUSH`, `TD_EDITOR`. `TD_ROOT`
relocates the store itself, which is what tests use.

The editor is resolved `config.toml` → `TD_EDITOR` → `$EDITOR` → `vi`
(`internal/config/config.go:171-179`). `$VISUAL` is **not** consulted, despite
INTENT §8 listing it. This affects the TUI and hand-editing only; nothing the
skill runs opens an editor.
