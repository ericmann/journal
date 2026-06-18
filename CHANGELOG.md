# Changelog

All notable changes to `journal`. The format follows
[Keep a Changelog](https://keepachangelog.com); this project adheres to semantic
versioning. Build-time design rationale lives in
[`docs/DECISIONS.md`](docs/DECISIONS.md).

## [Unreleased]

### Added — `journal transcribe` for non-Quill recordings

- **`journal transcribe <whisperx.json>`** ingests any recording's WhisperX JSON
  into an indexed transcript: speaker-labeled, timestamped Markdown (same shape
  as Quill transcripts — frontmatter + `# Title` + `## Notes` + `## Transcript`,
  stamped `source: whisperx`) in the `transcripts/` landing zone, dated to the
  meeting, then indexed. `--title`, `--date`, `--no-summary`.
- **Generated `## Notes` summary for retrieval.** It summarizes the transcript
  with your configured `synth_provider` (Ollama / Anthropic / OpenAI-compatible)
  and puts it at the top, so search hits a concise entry point instead of
  trawling a multi-hour meeting's windows — the same benefit Quill's AI notes
  give. Falls back to transcript-only (with a notice) if no provider is reachable.
- **Bundled helper + docs.** `scripts/transcribe.py` wraps the heavy
  ffmpeg+WhisperX step (reads the HF token from `HF_TOKEN`, never argv) and
  `docs/TRANSCRIBE.md` documents the full pipeline and the gotchas (HF token,
  accepting gated pyannote models, Python/dep conflicts). The binary stays
  pure-Go — the transcription step is an optional, documented external tool.

## [2.5.0] — 2026-06-17

### Added — pluggable OpenAI-compatible providers

- **`synth_provider: openai`** points synthesis and grounded search answers at
  any OpenAI-compatible Chat Completions endpoint (OpenAI, **OpenRouter**, Groq,
  Together, a local server, …) via `synth_openai_base_url` + `synth_openai_model`,
  authed with `OPENAI_API_KEY`. Lets you use, e.g., OpenRouter's free Gemma for
  synthesis without a Claude bill or a local GPU. Cloud egress — refused under
  `local_only`.
- **`embed_provider: openai`** does the same for embeddings against an
  OpenAI-compatible `/embeddings` endpoint. Caveats: no LLM-as-reranker (Ollama
  only), and `embed_dim` must match the model (e.g. 1536 for
  `text-embedding-3-small`) with a `journal index --rebuild`. Also cloud egress —
  refused under `local_only`. Default stays `ollama` (local).
- `journal doctor` reports the active synth/embed providers, only contacts
  Ollama when something actually uses it, and fails if an `openai` provider is
  selected without `OPENAI_API_KEY`. Keys are read from the environment only.

## [2.4.5] — 2026-06-17

### Fixed

- **`journal index` crash on Go 1.26 / linux-amd64
  ([#2](https://github.com/ericmann/journal/issues/2)).** The bundled wazero
  (the Wasm runtime that executes the embedded SQLite via `ncruces/go-sqlite3`)
  was pinned at v1.8.2, which mis-JIT-compiles under the Go 1.26 runtime ABI and
  faulted (`SIGSEGV` in `runtime.memmove`) the moment indexing touched the
  store. Bumped the wazero floor to **v1.12.0** — no source changes;
  `ncruces/go-sqlite3` v0.21.3 compiles unchanged against it. Also resolves the
  intermittent `internal/store` "wasm out of bounds" CI flake (same root cause).
  Added an end-to-end index→search regression test through the real wazero store.

## [2.4.4] — 2026-06-16

### Fixed

- **LICENSE copyright set to Eric Mann.** The MIT holder was left as the
  templated default `Displace Technologies, LLC` placeholder; corrected to the
  personal attribution (matching note in DECISIONS.md fixed too).

## [2.4.3] — 2026-06-16

### Documentation

- **Fully documented Jan as a local MCP client.** LOCAL-SETUP and CLIENTS now
  cover every Jan gotcha verified in practice: the per-model **Tools capability
  toggle** (required — without it Jan attaches no tools and the model just
  narrates), **one-argument-per-line** MCP config (plus the macOS Smart-Dashes
  `--`→`—` trap), the **`Origin: null`** CORS 403 and its launchd-env/LaunchAgent
  fix, the placeholder API key, and steering clear of Jan's web-search assistant
  prompt. Corrects the earlier wrong guidance to allowlist `http://tauri.localhost`.

## [2.4.2] — 2026-06-16

### Fixed

- **`local_only` + a non-`ollama` `synth_provider` is now a clear config error.**
  Previously the combination loaded silently and then `synth --write` failed at
  call time (cloud synthesis is refused under `local_only`), and `journal doctor`
  showed contradictory `synth`/`egress` lines. `Validate` now rejects it with a
  message that names the right key (`synth_provider`, not `synth_model`).
- **`local_only` no longer disables `journal sync`.** `local_only` targets
  cloud-AI egress (cloud synthesis, MCP clients, networked Ollama); git sync
  backs up to your *own* remote and is now governed solely by `sync_enabled`, so
  local-only AI and git backup can coexist. For a fully sealed "nothing leaves
  this machine" posture, keep `sync_enabled: false` (the default). `journal
  doctor`'s `egress` line reflects which posture you're in.

## [2.4.1] — 2026-06-16

### Changed

- **Homebrew tap moved to `ericmann/tap`.** Install with
  `brew install ericmann/tap/journal`; GoReleaser now publishes the cask to
  `ericmann/homebrew-tap`. The old `DisplaceTech/homebrew-tap` cask is
  deprecated (frozen at v2.4.0, with a `deprecate!` pointer).
- **README + `journal doctor` clarify cloud vs. local synthesis up front.** The
  new `doctor` `synth` check names the active provider and model and how to
  switch (cloud Claude ⇄ local Ollama); the READMEs present both paths
  succinctly instead of implying cloud-only.

## [2.4.0] — 2026-06-16

### Added — local-first synthesis & the egress kill-switch

- **Local synthesis via Ollama.** `synth_provider: ollama` runs `journal synth`
  and grounded search answers against a local model (`synth_ollama_model`,
  default `gemma4:12b`) — no API key, no note content leaving the machine.
  Every request sends `synth_num_ctx` (default 32768) explicitly because
  Ollama's 4096 default truncates prompts silently. `journal doctor` verifies
  the synth model is pulled when the provider is ollama.
- **`local_only: true`** — one-line egress kill-switch: refuses cloud synthesis,
  disables `journal sync`, blocks `journal mcp` (MCP clients may forward note
  content to cloud models), and requires a loopback `ollama_base_url`. The new
  `egress` doctor check reports the posture either way.
- **`local_only_mcp: allow`** — re-enables `journal mcp` under `local_only` for
  local-model MCP clients (LM Studio, Jan, …). An explicit attestation: stdio
  gives the server no trustworthy client identity, so the egress responsibility
  for the MCP path shifts to the user. Default `block`.
- **Docs:** [DATA-FLOWS.md](docs/DATA-FLOWS.md) (what's stored, what can leave,
  HIPAA posture), [CLIENTS.md](docs/CLIENTS.md) (fully-local MCP GUI
  alternatives to Claude Desktop: LM Studio, Jan, AnythingLLM), and
  [LOCAL-SETUP.md](docs/LOCAL-SETUP.md) (end-to-end zero-egress guide: Ollama
  install → models → journal config → LM Studio MCP integration).

## [2.3.0] — 2026-06-12

### Added

- **MCP `show` tool.** Reads a note file's full raw markdown by date
  (`YYYY-MM-DD`, `today`, `yesterday`), repo-relative path, or a
  `path:line-line` citation taken verbatim from another tool's results — so a
  connected Claude can always dereference a citation into complete content.

### Changed

- **Search results carry the full chunk body.** `journal search --json` and the
  MCP `search` tool now include a `body` field alongside the 240-char `snippet`
  (which MCP clients couldn't read past). Additive — existing fields unchanged.
  List commands (`recent`, `decisions`, `todos`) remain snippet-only.

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
