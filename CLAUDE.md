# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What td is

A tmux-native todo list driven by Claude Code. Items are markdown files with YAML
frontmatter under `~/.td/`, split into a global scope and per-project scopes resolved
from a `.td` marker file. Every command commits its own change, so the store is always
a readable git repo. Two surfaces: the cobra CLI (`cmd/td`) and a Bubble Tea pane
(`internal/tui`). See `README.md` for the user-facing behaviour, keys and config.

## Commands

`make all` (`fmt-check lint test`) is what CI runs and what to run before pushing.

```bash
make build          # go build -ldflags -X main.version=… -o bin/td ./cmd/td
make install        # same, into $PREFIX (default ~/.local/bin), then prints --version
make test           # go test ./...
make race           # go test -race ./...  — the TUI watcher and off-loop epilogue live here
make cover          # coverage profile + total
make lint           # go vet ./... plus golangci-lint, version pinned in the Makefile
make fmt            # gofmt -l -w .
make tidy-check     # fails if go.mod/go.sum would change under go mod tidy
```

One test, one package:

```bash
go test ./internal/store -run TestSkeletonCarriesEveryKeyTdOwns -v
go test . -run TestLifecycle -v                   # e2e_test.go; TestMain builds the binary first
```

Never `cp` a new binary over one already on `$PATH` — macOS caches the code signature
against the file and every later run is killed with no output. `go build -o` and
`make install` rename into place, so they are safe.

## Architecture

```
cmd/td             cobra CLI              → task, epilogue, store, config, output, tui
internal/tui       Bubble Tea pane        → task, epilogue, store, config
internal/task      what an operation does → store
internal/epilogue  the tail every change runs → store, config
internal/store     items as files on disk → gitx
internal/gitx      the only entry point to git (always `git -C <root>`)
internal/config    config.toml and its TD_ overrides
internal/output    the CLI's --json / plain-text printer
internal/tdtest    fixtures, imported only from _test.go
```

**A surface never reimplements an operation.** Add, done, undo, rm, restore and edit all
live in `internal/task` as methods on `task.Service` returning a `task.Result`. A surface
parses input (flags for the CLI, a keystroke for the pane), builds a request, renders the
result. A new operation is a new file in `internal/task` plus a caller in each surface
that wants it — not a second implementation.

**The service does not run the epilogue.** It mutates the store and hands back entries
plus a commit message; the caller decides when the commit happens. `cmd/td` runs it
synchronously (`(*app).finish` in `cmd/td/mutate.go`); `internal/tui` runs it as a
`tea.Cmd` off the event loop, because `epilogue.Run` takes a blocking flock that a CLI
process may hold.

**Time and identity come from the `task.Service`, not from package globals.** Stamps
(`created`, `updated`, `done_at`) use `store.Now`, truncated to the second, because
`store.Save` pins the file's mtime to `Item.Updated` and a nanosecond clock makes td read
its own write back as a hand edit. Ids read an untruncated clock — they encode a
millisecond. Ids are minted by the process-wide `store.NewID`, never a per-Service
generator: running the pane builds two Services in one process.

**Epilogue order is fixed**: bump (record hand edits) → archive (sweep done items past
their TTL) → commit → push, all under one exclusive store lock. The maintenance commands
(`td bump/archive/commit/push`) each run just their own `epilogue.Step`.

**Store layout is the record.** An item's scope (global vs. project directory) and area
(`Active` = the scope dir, `archived/`, `deleted/`) are its location on disk and nothing
else. `deleted/` is gitignored, so trash never reaches a remote.

**Adding a frontmatter field** means a field on `store.Item` *and* a row in the `fields`
table in `internal/store/item.go` — the decoder, key order, skeleton and both writers all
derive from that one table. `TestSkeletonCarriesEveryKeyTdOwns` fails if you forget the
fixture. Unknown keys in a hand-written file are preserved in place: `Item.parsed` keeps
the original `yaml.Node` so a rewrite edits the file's shape rather than rebuilding it.

## Conventions

- **Exit codes are contract** (`cmd/td/errors.go`): 0 ok, 1 store/git failure, 2 usage,
  3 not found, 4 ambiguous id. `e2e_test.go` repeats them deliberately, so a change has
  to be made in two places. The `--json` action strings are contract too — removal reports
  `"remove"`, not `"rm"`, and `cmd/td/mutate.go` branches on that word.
- **Every exported identifier is documented**, and most unexported ones. `revive`'s
  `exported` and `package-comments` rules are on; comments explain *why*, not what.
- **Tests isolate git** with `tdtest.IsolateGit(t)` — never rely on the developer's
  `~/.gitconfig`. Set `TD_ROOT` to point the store at a throwaway directory.
- `internal/tui/readme_test.go` parses the key table out of `README.md` — change a TUI
  keybinding and the README must change with it.
- Windows is unsupported: the store lock is a `flock` (`internal/epilogue/lock_unix.go`).

## The /td plugin

`td-plugin/skills/td/SKILL.md` is the skill Claude Code drives td through; the flag and
`--json` reference is `td-plugin/skills/td/references/td-cli.md`. Changing a command's
flags, JSON shape or exit codes means updating those too.
