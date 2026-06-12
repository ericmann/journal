# Changelog

All notable changes to `journal`. The format follows
[Keep a Changelog](https://keepachangelog.com); this project adheres to semantic
versioning. Build-time design rationale lives in
[`docs/DECISIONS.md`](docs/DECISIONS.md).

## [2.2.1] — 2026-06-12

### Fixed

- **Chunker dropped note content after non-date `#` headings.** Only a date H1
  (`# YYYY-MM-DD`) is structural; any other `# ` line — e.g. markdown pasted
  into a capture block — previously terminated the open `##` block, leaving an
  empty chunk and silently un-indexing everything after it. Such lines now stay
  in the block body. Affected notes re-embed automatically on the next
  `journal index` (the body change produces new chunk IDs — no `--rebuild`
  needed).

## [2.2.0] — 2026-06-09

### Added — the productivity loop

- **Todo lifecycle.** A new `@done` marker joins the recognized set.
  `journal todos` lists open `@todo` items newest-first with `path:line` citations
  (`--done`, `--all`, `--project`, `--since`, `--json`); `journal done <ref>`
  (citation or unique text fragment) rewrites that one `@todo` token to
  `@done YYYY-MM-DD` in the note, re-indexes, and auto-commits. New MCP **`todos`**
  and **`done`** tools let a connected Claude read and check off your list.
- **Read commands.** `journal show [date|path]` renders any note (glamour on a
  TTY); `journal today` is a day-at-a-glance dashboard (today's notes + open
  todos + today's meetings, `--json` structured); `journal edit [date]` opens a
  daily file in your editor (creating it with its date header) and auto-commits.
- **`journal stats`** — capture volume by source, projects/meetings, marker
  counts, current & longest daily streak, this-week-vs-last, top tags (`--json`).
- **`journal tui`** — full-screen interactive dashboard (bubbletea): Today,
  Todos (press `d` to complete), semantic Search, Recent, Meetings, Stats.

### Notes

- All additive — no store schema change, no config keys, no breaking CLI changes.
- New Go deps: charmbracelet bubbletea/lipgloss/bubbles (the line already adopted
  via glamour). Builds remain pure-Go/static.

## [2.1.0] — 2026-06-08

### Added

- **Global `--journal-dir` flag** (and `JOURNAL_DIR` env var) on every command, so
  you can capture into / query a specific journal from any directory — e.g.
  `journal capture "…" --journal-dir ~/Projects/devnotes`, or
  `export JOURNAL_DIR=~/Projects/devnotes`. The flag wins over the env; both expand
  `~` and accept the repo root or any subdirectory. `mcp --repo` still takes
  precedence for the MCP server.

## [2.0.1] — 2026-06-08

### Fixed

- **`quill-sync` stopped finding new meetings after the first sync.** Quill stores
  `Meeting.start`/`end` as epoch-millisecond integers, but the incremental filter
  compared that integer column to an RFC3339 text watermark in SQL — always false
  in SQLite, so every meeting was filtered out once a watermark existed. Meetings
  are now filtered on the parsed Go timestamp instead, and `parseQuillTime` accepts
  epoch ms/sec/µs integers.

## [2.0.0] — 2026-06-05

### Added — Quill / meeting-transcript integration (headline)

- **`journal quill-sync`** — pulls meeting transcripts from the local
  [Quill](https://www.quillmeetings.com) app's SQLite database (read-only, from a
  temp copy) and renders each meeting to Markdown in a gitignored `transcripts/`
  landing zone. Incremental via a watermark under `.journal/`; `--full` re-renders
  all, `--db` overrides the path. **macOS/Windows only** (Quill doesn't run on
  Linux); exits cleanly with guidance when no database is present. See
  [`docs/QUILL.md`](docs/QUILL.md).
- **Transcript indexing** — the watcher and one-shot `index` now also index the
  `transcripts/` landing zone as a `transcript` source (line-windowed chunks,
  tagged `meeting`), **never auto-committed**. Dropped-in `.qm` exports are
  rendered to Markdown and indexed (`quill.accept_qm_imports`).
- **`--source notes|transcript|all`** filter on `search` (CLI + MCP).
- **`journal meetings`** (CLI) and a **`meetings`** MCP tool — recent transcripts,
  newest first (filename, timestamp, title, snippet).
- **`journal synth meetings`** — AI digest of meeting transcripts over the last N
  days (default 7) → `reflections/meetings-<date>.md`.
- New config blocks `transcripts:` and `quill:`, and a `schema_version` key.
- `journal doctor` checks the transcripts landing zone and the Quill database
  (meeting count vs. synced).

### Changed

- **`journal init` now upgrades existing repos non-destructively**: it scaffolds
  `transcripts/`, gitignores it, adds any missing config keys (preserving your
  values), bumps the config schema to **2.0**, and prints a summary of changes.
- Store schema migrates to **v2** in place (adds a `source` column) — no rebuild
  required.

### Notes

- No new dependencies — the Quill database is read with the existing pure-Go
  sqlite driver. Quill's schema is app-internal/undocumented; journal reads it
  defensively behind a single adapter (`internal/quill`) and never writes to it.

Earlier releases (v1.0–v1.5) predate this changelog; see
[`docs/DECISIONS.md`](docs/DECISIONS.md) for their history.
