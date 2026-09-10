# td

`td` is a tmux-native todo list driven by Claude Code. Ideas raised mid-session
currently die in scrollback; `td` gives each one a file, a pane, and a git
history. Items are plain markdown with YAML frontmatter under `~/.td/`, split
into a global scope and per-project scopes resolved from a `.td` marker file.
Every command commits its own change, so the store is always a readable git
repo you can edit by hand.

## Status

Milestone 2 — the `/td` Claude Code plugin, on top of Milestone 1's binary and
its CLI subcommands. The Bubble Tea TUI follows in Milestone 3.

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
