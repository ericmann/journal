# Tracking Todos

journal has a lightweight todo system built directly into your notes. You don't need a separate task manager — just drop `@todo` into any note you capture, and journal tracks it for you.

## Adding a todo

Write `@todo` anywhere in a captured note:

```sh
journal capture "follow up with the Acme team on pricing @todo"
journal capture "check if the Redis eviction fix is still holding @todo #redis"
```

Or use the marker flag:

```sh
journal capture "write up the architecture decision" --marker todo
```

Once indexed, the todo appears in `journal todos`.

## Listing your todos

```sh
journal todos
```

You'll see a numbered list with a snippet of each todo and the file and line it came from:

```
1. daily/2026/06/2026-06-12.md:8 — follow up with the Acme team on pricing
2. daily/2026/06/2026-06-11.md:14 — check if the Redis eviction fix is still holding
3. projects/acme-launch/notes/2026-06-10.md:5 — write up the architecture decision
```

Those `file:line` references are exact — you can open the file in your editor at that line to read the full context.

## Checking off a todo

Mark a todo as done by passing part of its text or its citation:

```sh
journal done "follow up with the Acme team"       # by text fragment
journal done daily/2026/06/2026-06-12.md:8        # by citation
```

journal rewrites that one `@todo` to `@done 2026-06-12` in your note file (the rest of the note is untouched), re-indexes it, and auto-commits. It's the Markdown equivalent of checking a checkbox.

## Viewing completed todos

```sh
journal todos --done     # only completed items
journal todos --all      # everything: open and done
```

## Filtering

```sh
journal todos --project acme-launch      # todos in a specific project
journal todos --since 1w                 # open items from the last week
```

## Bulk dismissal

When todos pile up and become irrelevant all at once — a project wraps up, a planning doc is superseded — `journal dismiss` clears them in a single command and a single git commit:

```sh
# Dismiss all open todos in a project:
journal dismiss --project acme

# Dismiss todos older than 4 weeks:
journal dismiss --before 4w

# Combine filters, skip the prompt:
journal dismiss --project janus --before 2w --yes

# Attach a resolution note to each dismissed todo:
journal dismiss --project acme --resolution "superseded by new plan" --yes
```

`dismiss` shows you the matched set before changing anything and asks for confirmation (answer `y`, or pass `--yes` to skip the prompt). Each `@todo` is rewritten to `@done YYYY-MM-DD` — exactly what `journal done` does — and all the rewrites land in **one** git commit. At least one filter is required.

## Todos via Claude

If you use journal with Claude Desktop or Claude Code, Claude can check off todos for you. The `todos` and `done` tools are exposed through the MCP server, so you can ask:

> "What are my open todos? Mark the Acme pricing follow-up as done."

Claude calls the same `done` command behind the scenes, and the rewrite happens in your actual Markdown files.
