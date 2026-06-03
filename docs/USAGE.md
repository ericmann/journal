# Usage

The day-to-day guide: how notes are structured, the command surface, search, and
running the indexer continuously. See also
[Configuration](CONFIGURATION.md) · [Synthesis](SYNTHESIS.md) ·
[Remote backup](SYNC.md).

## Capture conventions

The daily file is intentionally minimal — an `# YYYY-MM-DD` H1 and a series of
timestamped blocks. Each `journal capture` appends one block:

```markdown
# 2026-06-01

## 09:14 #cabot #litellm
Routing fallback isn't triggering when Qwen OOMs — errors instead of
falling through to cloud.

## 14:02 #displace #canton @decision
Declaring the dev fund payment through Displace as business income.
```

- **`#tags`** → faceted retrieval. Pass `--tags a,b` _or_ just write `#tag`
  inline; both are detected, case-folded, and deduped onto the block header.
  (URL fragments and `[text](#anchor)` links are *not* treated as tags.)
- **`@markers`** → structured annotations the query and synthesis layers key
  off. The recognized set is **`@decision`**, **`@question`**, **`@todo`**.
  Pass `--marker decision` _or_ write `@decision` inline.
- **Heading blocks (`##`)** are the unit of chunking and retrieval.

`--project <name>` routes a capture into `projects/<slug>/notes/YYYY-MM-DD.md`
instead of the daily file, using the same block format (the project name is
slugified, e.g. `"Canton COI"` → `canton-coi`).

With **no text**, `journal capture` opens your editor to compose the note (like
`git commit`), or reads it from **stdin** when input is piped
(`journal capture < note.md`). The editor follows `$JOURNAL_EDITOR`, the `editor`
config key, `$VISUAL`, `$EDITOR`, then `nano`.

Capture appends instantly and, in a git repo, **auto-commits the note** (see
[Auto-commit](#auto-commit)). It does no embedding — run `journal index` (or the
watcher) to make new notes searchable.

## Command surface

```
journal init    [path]                          # bootstrap (or upgrade) a repo
journal capture [text] [--tags a,b] [--project slug] [--marker decision|question|todo]
journal index   [--rebuild] [--since 2w]        # embed changed notes (one-shot)
journal index --watch                           # continuous, debounced re-index
journal search  <query> [--k 5] [--tag t] [--project slug] [--since 2w] [--answer|--no-answer] [--json]
journal recent  [--tag t] [--project slug] [--since 1w] [--json]
journal decisions [--project slug] [--since 4w] [--json]
journal threads [--stale] [--days 14] [--json]
journal sync    [--dry-run]                      # back up notes to/from the git remote
journal synth   weekly|daily|decisions|stale [--dry-run] [--write] [--project slug] [--days 14] [--date YYYY-MM-DD]
journal doctor  [--json]                          # health checks
journal mcp     [--repo path]                     # MCP server (stdio) for Claude clients
```

`journal mcp` exposes `search`/`recent`/`decisions`/`threads`/`capture` as MCP
tools (same JSON as `--json`) — see [INTEGRATIONS.md](INTEGRATIONS.md) §3b for the
one-block config. Synthesis is documented in [SYNTHESIS.md](SYNTHESIS.md); backup
in [SYNC.md](SYNC.md).

## Retrieval & queries

- **`search`** embeds your query (with a retrieval instruction), runs a
  brute-force vector KNN over the index, optionally reranks the top candidates
  (if a `reranker` is configured), and returns the best `--k` with
  `path:line_start-line_end` citations. With reranking off, results are in
  vector-distance order.
- **`recent`** / **`decisions`** are plain metadata queries (newest first);
  `decisions` filters to `@decision` blocks.
- **`threads`** summarizes project activity; `--stale` surfaces projects with no
  activity in `--days` (default 14).

**AI answer (key-gated).** When `ANTHROPIC_API_KEY` is set, `search` also generates
a short, grounded answer to your question with the configured `synth_model` and
prints it (formatted) **above** the raw hits — the raw results are always kept. It
answers only from the retrieved notes and says so when they don't cover the
question. It's automatic when a key is present; `--no-answer` skips it, `--answer`
forces it (and errors if no key). `--json` never includes the answer. The answer
is rendered richly on a terminal and as plain markdown when piped.

Every read command supports **`--json`** with a stable schema:

```json
{ "results": [ { "path": "daily/2026/06/2026-06-01.md", "line_start": 3,
  "line_end": 5, "heading": "09:14 #cabot", "snippet": "…", "score": 0.87,
  "tags": ["cabot"], "markers": [] } ] }
```

`threads --json` emits `{ "threads": [ { "project", "last_activity", "chunks",
"open_questions", "stale", "days_since" } ] }`. On failure, commands emit
`{ "error": "…" }` to stdout and exit non-zero — so an empty result set is
distinguishable from an error.

## The watcher

Indexing stays fresh via a long-running `journal index --watch`: it does an
initial index, then debounces filesystem events and re-indexes only changed files
(deletions remove their chunks). Ctrl-C stops it cleanly.

The **recommended way to run it is a dedicated `tmux` pane** — simple, visible,
and easy to restart:

```sh
tmux new-session -d -s journal 'cd ~/journal && journal index --watch'
tmux attach -t journal     # watch it;  Ctrl-b d to detach
```

### Unattended — launchd (macOS)

To survive logout/reboot, install a per-user launchd agent at
`~/Library/LaunchAgents/com.ericmann.journal-watch.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>            <string>com.ericmann.journal-watch</string>
  <key>ProgramArguments</key> <array>
    <string>/usr/local/bin/journal</string>
    <string>index</string>
    <string>--watch</string>
  </array>
  <key>WorkingDirectory</key> <string>/Users/you/journal</string>
  <key>RunAtLoad</key>        <true/>
  <key>KeepAlive</key>        <true/>
  <key>StandardOutPath</key>  <string>/tmp/journal-watch.log</string>
  <key>StandardErrorPath</key><string>/tmp/journal-watch.log</string>
</dict>
</plist>
```

```sh
launchctl load   ~/Library/LaunchAgents/com.ericmann.journal-watch.plist
launchctl unload ~/Library/LaunchAgents/com.ericmann.journal-watch.plist   # to stop
```

### Unattended — systemd (Linux)

A per-user service at `~/.config/systemd/user/journal-watch.service`:

```ini
[Unit]
Description=journal watcher (re-index notes on change)

[Service]
Type=simple
WorkingDirectory=%h/notes                  # your journal repo
ExecStart=/usr/local/bin/journal index --watch
Restart=on-failure

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now journal-watch.service
loginctl enable-linger "$USER"   # keep it running without an active login (servers/headless)
journalctl --user -u journal-watch -f   # tail its logs
```

User services run with a **minimal PATH**, so use an absolute `ExecStart`. For the
periodic backup (`journal sync`), a systemd **timer** is the native alternative to
cron — see [SYNC.md](SYNC.md).

## Auto-commit

When the repo is a git repository, `journal capture`, `journal index`, and
`index --watch` all **auto-commit your note changes** (controlled by
`git_autocommit`, default on). **Capture commits the note immediately** — so your
words are committed the moment you capture them, watcher or not. The watcher /
`index` then keep the search index fresh and also commit any direct file edits.
You can't forget. Details:

- It commits **only your markdown** (`git add -A` honors `.gitignore`, so the
  vector index is never committed).
- It's a **no-op unless the repo root is a git top level** — it never commits your
  notes into a parent repository, and does nothing if git isn't installed or the
  folder isn't a repo.
- Commits are **unsigned by default** (`git_autocommit_sign: false`) so an
  unattended watcher doesn't trigger a signing prompt per note; set it `true` for
  signed note-commits.
- Commit failures are **logged, never fatal** — your markdown is always safe on
  disk. Messages are auto-generated, e.g.
  `📓 scribbled notes — +1 new, ~1 revised, -0 removed · Mon 2026-06-01 12:32`.

Set `git_autocommit: false` to manage commits yourself. To get notes *off* the
machine to a remote, see [Remote backup](SYNC.md).
