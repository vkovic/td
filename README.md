# td

`td` is a tmux-native todo list driven by Claude Code. Ideas raised mid-session
currently die in scrollback; `td` gives each one a file, a pane, and a git
history. Items are plain markdown with YAML frontmatter under `~/.td/`, split
into a global scope and per-project scopes resolved from a `.td` marker file.
Every command commits its own change, so the store is always a readable git
repo you can edit by hand.

## Status

Milestone 1 — the `td` binary and its CLI subcommands. The `/td` Claude Code
plugin and the Bubble Tea TUI follow in later milestones.

## Build

```
go build ./cmd/td
./td --version
```
