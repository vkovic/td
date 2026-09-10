---
name: td
description: Todo capture and management with the td CLI. Fires on /td, "todo", "add a todo", "note that for later", "put it on the list", "add what we discussed", "what's on my list", "mark X done", "drop that todo".
argument-hint: "[free text: <new item> | ls | done <title> | rm <title>]"
allowed-tools: Bash(td:*)
---

# td

Turn the request into `td` commands and run them. The request is `$ARGUMENTS`, or the message that raised the todo when `$ARGUMENTS` is empty; a bare `/td` means `td ls`.

Run every command from the current directory as-is: the `.td` marker decides project or global scope. Add `-g` only when the request says global, `-p <name>` only when it names another project.

## 1. Pick the command

| Request | Command |
| --- | --- |
| a new item, or "add what we discussed" | one `td add` per distinct item, shaped per step 3 |
| what is open, what is on the list | `td ls`; `--done` when done items are wanted, `--all` for every scope, `-t <tag>` to narrow |
| the full text of one item | `td show <id>` |
| an item is done / reopen it / drop it / bring it back / change it | `td done` / `td undo` / `td rm` / `td restore` / `td edit`, with the id from step 2 |

## 2. Resolve a title to an id

`show`, `done`, `undo`, `rm`, `restore` and `edit` take ids only. For an item the user named in prose:

1. `td ls --json`, adding `--done` when the item may be closed and `--all` when it may sit in another scope.
2. Match the user's words against `title` in `items`. One match → use its `id`. Several → ask which, listing them with ids. None → say so and stop.
3. Exit 4 means the id prefix was ambiguous: the message lists the candidates, retry with the full id.

## 3. Shape an add

- **Title** - one imperative line, **always single-quoted**. `td` is happy with bare words, but the *shell* mangles an unquoted title before `td` ever sees it: a `$` or a backtick silently stores the wrong text, an apostrophe is a syntax error. A title cannot begin with `-`; reword it.
- **Body** - the context that dies in scrollback otherwise: why, the `file:line` it concerns, what was decided. Markdown over stdin:

```bash
td add 'Retry the fetcher on 429' --source claude --session-id ${CLAUDE_SESSION_ID} --session-name fetcher-retry --body-file - <<'EOF'
`internal/fetcher/client.go:88` returns on 429 without backoff; add retry with jitter.
EOF
```

- `-t <tag>` and `--due YYYY-MM-DD` only when the request gives them.
- **Session stamp** - `--source claude --session-id ${CLAUDE_SESSION_ID} --session-name <topic>` on every `add`, where `<topic>` is a kebab-case label for what this conversation is about, like `fetcher-retry` above.

## 4. Report

One line per item touched: id, title, scope. Relay any stderr warning verbatim. `td` commits and pushes on its own, so never offer to do it — but do not claim it did either: `auto_commit` and `auto_push` can be off, and then it did not. The turn ends there.

`references/td-cli.md` holds every command with its flags, the `--json` shapes of `ls`, `show` and the mutating commands, and the exit codes. Open it for a request or flag this file does not show, or before reading a `--json` field.
