# Capturing Notes

Every note you capture goes into a plain Markdown file. This means you can always read, edit, or move your notes with any text editor — journal doesn't lock you in.

## The basic capture

```sh
journal capture "the redis cache is evicting too aggressively under load"
```

This appends a timestamped block to today's daily file (`daily/YYYY/MM/YYYY-MM-DD.md`) and auto-commits it to git.

## Adding tags

Tags help you find related notes later. Add them inline with `#`:

```sh
journal capture "redis eviction is too aggressive under load #redis #infra"
```

Or pass them as a flag:

```sh
journal capture "redis eviction issue" --tags redis,infra
```

Both ways work, and journal deduplicates them. Tags are case-insensitive. When you search later, you can filter by tag with `--tag redis`.

## Adding markers

Markers are special annotations that the search and synthesis layers understand:

- `@decision` — a choice you made
- `@question` — something you're still working out
- `@todo` — an action item (see [Tracking Todos](todos.md))

Add them inline:

```sh
journal capture "decided to use Redis Cluster instead of Sentinel @decision #redis"
```

Or as a flag:

```sh
journal capture "use Redis Cluster" --marker decision
```

Later, `journal decisions` lists everything you marked `@decision`, and `journal search` can filter by marker.

## Writing longer notes in your editor

Leave off the text and journal opens your editor:

```sh
journal capture
```

Write your note, save, and close. Journal uses `$JOURNAL_EDITOR`, then `$VISUAL`, then `$EDITOR`, then falls back to nano.

You can also pipe content in from stdin:

```sh
cat meeting-notes.md | journal capture
```

## Organizing by project

By default, notes go into the daily file. If you're tracking something longer-term, route it to a project:

```sh
journal capture "pricing model decision: go with usage-based @decision" --project acme-launch
```

This writes to `projects/acme-launch/notes/YYYY-MM-DD.md`. You can search, filter, and synthesize by project independently of your daily notes.

## Capturing from any directory

You can use journal from any folder in your terminal — just tell it where your journal lives:

```sh
journal capture "quick idea" --journal-dir ~/notes
```

Or set it once in your environment:

```sh
export JOURNAL_DIR=~/notes
```

After that, every `journal` command works from any directory.

## Auto-commit

journal automatically commits your notes to git after each capture. This means your words are in version control the moment you write them — no `git add`, no `git commit`, nothing to forget.

The commit message is generated automatically, something like:  
`📓 scribbled notes — +1 new, ~1 revised -0 removed · Mon 2026-06-01 12:32`

If you prefer to manage commits yourself, set `git_autocommit: false` in `.journal/config.yaml`.

## Keeping the index current

Capture saves your note immediately, but doesn't update the search index. Run `journal index` to embed new notes, or keep the watcher running so indexing happens automatically:

```sh
journal index --watch
```

The watcher watches your notes folder, debounces changes, and re-indexes only what's changed. Run it in a background terminal or set it up as a background service (see the [Configuration Reference](configuration.md)).
