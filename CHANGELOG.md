# Changelog

All notable changes to `journal`. The format follows
[Keep a Changelog](https://keepachangelog.com); this project adheres to semantic
versioning. Build-time design rationale lives in
[`docs/DECISIONS.md`](docs/DECISIONS.md).

## [Unreleased]

### Added

- **`journal log` recording toggle — mic capture (Phase 3, macOS only).** The bare
  `journal log` command now toggles mic recording: the first press starts a detached
  background recorder (`ffmpeg -f avfoundation`, 16 kHz/mono/16-bit PCM) and prints
  "● recording"; the second press stops it and hands the finalized WAV off to the
  existing transcribe→shape→assemble→land→index pipeline asynchronously, so both
  presses return immediately. `--start`/`--stop`/`--cancel`/`--status` give explicit
  control (`--cancel` discards the recording — no note is produced). The toggle state
  lives in a lockfile (`$XDG_RUNTIME_DIR/journal-log.lock` or
  `<tmp>/journal-log/journal-log.lock`) holding `{pid, wav_path, started_at}`; a dead
  PID is detected and cleaned up automatically on the next press. `log.audio.max_duration`
  (default 900s) self-finalizes a long recording; `log.audio.silence_autostop` (default
  off) is an optional safety-net stop after sustained silence. The recorded WAV is
  deleted after a successful run unless `log.audio.keep_wav: true`, in which case its
  path is recorded in the landed note's `audio:` frontmatter field. A WAV passed
  directly (`journal log <file>.wav`) is never auto-deleted, regardless of this
  setting — only recorder-produced scratch files are.
- **`internal/audio` package**: lockfile primitives (`ReadLock`/`WriteLock`/`RemoveLock`,
  injectable `PIDAlive`) and an injectable `Recorder` interface (`FfmpegRecorder` for
  production, `FakeRecorder` for tests) so the mic/ffmpeg dependency never runs in tests.
- **`log.audio.{tmp_dir,max_duration,silence_autostop,keep_wav}` config keys** and the
  `LogAudioTmpDirAbs()` config accessor, additive to the existing `log.audio.{device,
  sample_rate,channels}` keys.
- **`journal log <audio.wav>` — transcribe stage (Phase 2b).** Pass a WAV file as
  a positional argument to transcribe it locally via `whisper.cpp` and run the full
  transcribe→shape→assemble→land→index pipeline. No network access: a missing model
  fails fast with "run `journal models pull`". Silent recordings skip the pipeline;
  transcription errors are retryable (WAV is kept). The `transcriber` frontmatter
  records the backend/model used; `duration_sec` is derived from the WAV header.
- **`log.transcriber.{backend,model,model_dir}` config keys** replace the previous
  stub `log.transcriber.{engine,model}` keys. `backend` selects the engine
  (`whisper.cpp`, the only built-in); `model_dir` defaults to the same path as
  `transcriber.model_dir` so a single `journal models pull` serves both paths.
- **`LogTranscriberModelDirAbs()`** helper on `*config.Config` mirrors
  `TranscriberModelDirAbs()` for the log transcriber path.
- **`internal/log.Transcriber` interface** with a `FakeTranscriber` for tests and a
  `WhisperCPP` default implementation. The boundary is injectable so commands never
  exec a real binary in tests.
- **`journal log --text "..."` — voice note capture (Phase 1).** The full
  shape→assemble→land→index pipeline is now available for typed text. The LLM
  shaping step (configurable via `log.shaping.enabled`) cleans disfluencies,
  generates a title and summary, extracts `@todo`/`@decision`/`@question` markers,
  and tags the note. Notes always land to `logs/YYYY-MM-DD-HHMM-<slug>.md` even
  when shaping is unavailable (raw fallback). Index failure is non-fatal.
- **`SourceVoice = "voice"` source constant** in `internal/store`; voice chunks
  are indexed separately from notes and transcripts. No schema migration required —
  the `source TEXT` column already accepts any value.
- **`journal search --source voice`** (aliases `log`/`logs`) scopes search results
  to voice-note chunks.
- **`log:` config namespace** with `shaping`, `landing`, `audio`, and `transcriber`
  keys; `LogAbsPath()`/`LogRelPath()` helpers mirror `TranscriptsAbsPath()`.

## [2.7.1] — 2026-06-25

### Added

- **`journal dismiss` — bulk-dismiss open todos in one commit.** A new `dismiss`
  subcommand selects all open `@todo`s matching a `--project` filter, an
  `--before`/`--older-than` age window, or both, and rewrites each to `@done
  YYYY-MM-DD` (same rewrite as `journal done`). Requires explicit confirmation
  (`--yes` or an interactive `y` prompt). All file edits and re-indexing land in a
  **single** auto-commit whose message records the filter used (e.g. `dismissed 5
  todo(s) (project=acme, before=4w)`). The optional `--resolution` flag appends a
  `Resolution:` line to each dismissed block. Single `journal done` behavior is
  unchanged.

### Fixed

- **`journal today` now aggregates all of a day's notes, not just the daily file.**
  The Notes section previously read only `daily/YYYY/MM/YYYY-MM-DD.md`, so a day
  with notes captured via `--project` (or any project note) was reported empty.
  It now queries the index for all note chunks with `source=note` and
  `created_at >= midnight`, grouping project note sections with a source label.
  Meeting transcripts are excluded from Notes and continue to appear under
  Today's meetings. The MCP `today` tool inherits the fix (it routes through
  `gatherToday`). The `journal://today` MCP resource is unchanged — it remains
  the literal daily file; the aggregated view is the `today` *tool*.

- **Embed retry now rides through transient Ollama runner restarts.** The retry
  window for transient embed-runner crashes (400 with `EOF` / `do embedding
  request` body — a known llama.cpp/Metal SIGTRAP flake on Apple Silicon) has
  been extended from ~1.4 s to a 45 s wall-clock budget, sized to outlast a
  model reload. Backoff is exponential (n²×100 ms), capped per-sleep at 5 s,
  and jittered ±25%. Transport-level dial failures (`ErrUnreachable`) still fail
  fast via `maxRetries`; non-retryable 4xx (model not found, bad dimensions)
  still fail immediately.

## [2.7.0] — 2026-06-19

This release rounds out the **MCP surface** (the agent-facing side of journal),
makes **decisions & todos** first-class, and adds project tooling — a docs site,
a curated release pipeline, and the agentic-workflow layer.

### Added

- **MCP `synth` tool.** The MCP server now exposes synthesis as a `synth` tool so
  MCP clients (e.g. an agent drafting a weekly Slack summary) can run
  `weekly|daily|meetings|decisions|stale` synthesis jobs without shelling out to
  the CLI. By default the tool calls the synthesis provider and returns the
  generated text without writing a file (`persist: false`); set `persist: true` to
  also write the draft note to disk, mirroring `journal synth --write`. Honors
  `synth_provider`, `local_only`, and `voice_profile` exactly as the CLI does;
  returns a clean `{"error":"…"}` when synthesis is unavailable.
  Optional scoping params: `kind` (default `weekly`), `days`, `project`, `date`.
- **MCP `ask` tool** — runs retrieve→synthesize and returns
  `{answer, citations}` (grounded text + `path:line` references) so clients get a
  direct "what did I decide about X" answer instead of raw chunks. When no chunks
  match, returns "No relevant notes found." rather than calling the model
  ungrounded. Honors `local_only` / provider availability with a clear error
  ([#32](https://github.com/ericmann/journal/issues/32)).
- **MCP `stats` and `today` tools** — the same stable JSON as
  `journal stats --json` / `journal today --json`, for "how's my note volume" and
  "what does my day look like" ([#31](https://github.com/ericmann/journal/issues/31)).
- **MCP resources** — `journal://today`, `journal://recent`, and
  `journal://projects/{slug}/index` expose raw Markdown as addressable context via
  `resources/list` / `resources/read`, so clients can pull journal context without
  orchestrating tool calls. URIs are stable across server runs
  ([#37](https://github.com/ericmann/journal/issues/37)).
- **MCP prompts** — `weekly reflection`, `decisions review`, and `project status`
  are exposed as first-class, one-click prompts with journal context pre-assembled
  (reusing `synth.AssembleWeekly` / `AssembleDecisions`)
  ([#44](https://github.com/ericmann/journal/issues/44)).
- **`journal tags`** — list distinct `#tags` with usage counts, and
  `journal tags rename <old> <new>` rewrites a tag across all notes, re-indexes the
  affected files, and auto-commits (`--dry-run` previews). Boundary-aware so `#foo`
  isn't matched inside `#foobar` ([#33](https://github.com/ericmann/journal/issues/33)).
- **First-class decisions & todos.** Dedicated capture commands, crisp
  date/statement/citation rendering, proactive surfacing in `journal today`, and
  resolution notes on `journal done`. Fully backward compatible — existing
  `@decision`/`@todo`/`@done` blocks parse and surface unchanged
  ([#47](https://github.com/ericmann/journal/issues/47)).
- **Documentation site** at **<https://journal.eamann.com>** — an mdBook site
  (Catppuccin Latte) with warm, second-person prose across 14 chapters (install,
  capture, search, synthesis, meetings, configuration, integrations). Additive: the
  `docs/` tree is untouched ([#38](https://github.com/ericmann/journal/issues/38)).
- **One-click releases.** A `prepare-release` workflow (Actions → Prepare Release →
  version) finalizes the `## [Unreleased]` CHANGELOG section to the version, tags,
  and triggers the Release workflow; and the GitHub Release notes now come from the
  curated CHANGELOG section (`scripts/changelog-section.sh` + GoReleaser
  `--release-notes`) instead of an auto-generated commit list. See
  [docs/RELEASING.md](docs/RELEASING.md). Deterministic, project-specific tooling.
- **Agentic-workflow layer + `CLAUDE.md`.** GitHub Actions for agent-ready issues
  (auto-label → trigger → PR), a planning-approval gate for high-complexity work,
  `@claude` PR feedback (gated to write-access collaborators), and a retro flow;
  plus issue/PR templates, a label set, and a contributor guide grounded in the
  real layout. Contributor/CI tooling — no change to the shipped binary
  ([#25](https://github.com/ericmann/journal/issues/25)).

### Changed

- **Better LLM-as-reranker quality.** The rerank prompt is now a structured
  relevance rubric (0/5/10 anchors) instead of "reply with only the number", and
  score parsing is a robust multi-strategy parser (`N/10` fraction → labelled
  `Score: N` → last in-range number, skipping negatives). More reliable precision
  on the optional reranking path ([#36](https://github.com/ericmann/journal/issues/36)).

### Fixed

- **`journal index --watch` survives transient embed failures.** A per-file embed
  error during a watch pass is now logged and skipped (`continue`) instead of
  aborting the whole batch; only context cancellation still stops the run.
  Debounce coalescing of rapid edits is now covered by tests
  ([#35](https://github.com/ericmann/journal/issues/35)).
- **Indexing retries transient Ollama embed-server failures** (sporadic `400` /
  EOF) instead of failing the run, hardening large `journal index` passes
  ([#7](https://github.com/ericmann/journal/issues/7)).

## [2.6.1] — 2026-06-18

### Changed

- **Transcript chunking is now section-aware**, so a transcript's `## Notes`
  summary is its own clean, high-signal chunk instead of being diluted in a
  line-window with YAML frontmatter and the participant list. `ChunkTranscript`
  now drops the frontmatter, emits each short `##` section (notably `## Notes`)
  as a single chunk, and line-windows only the long transcript body. Benefits
  both Quill and `journal transcribe` (WhisperX) transcripts — both share the
  `## Notes` / `## Transcript` shape. Transcripts re-chunk (and re-embed) on the
  next `journal index`; no `--rebuild` needed (per-file reconcile handles it).

## [2.6.0] — 2026-06-18

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
