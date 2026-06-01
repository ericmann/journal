# journal

A local-first developer journal with semantic retrieval and AI-assisted synthesis.

`journal` turns a folder of plain-markdown notes into a searchable, AI-queryable
corpus. Capture is frictionless and append-only; retrieval is a local RAG stack
(Ollama embeddings + reranking, vectors in `sqlite-vec`); weekly synthesis is
backed by cloud Claude. **Markdown in git is the single source of truth** — the
vector index is a disposable, rebuildable cache and is never committed.

> **Status:** all six build phases (capture, store, index+search, watch+doctor,
> synthesis, and the Claude Code skill) are complete — see [Roadmap](#roadmap).
> Built phase by phase against [`docs/TDD.md`](docs/TDD.md); build decisions in
> [`docs/DECISIONS.md`](docs/DECISIONS.md).

---

## Why

Working notes get scattered across loose text files, per-project `/docs`, and
paper — none of it discoverable or reusable. `journal` keeps one durable
substrate (markdown in git) and adds a thin tool layer that makes capture
zero-friction and retrieval excellent, without a server, daemon, or cloud store.

The full rationale, architecture, and acceptance criteria live in
[`docs/TDD.md`](docs/TDD.md). Build-time decisions and deviations are tracked in
[`docs/DECISIONS.md`](docs/DECISIONS.md). For wiring `journal` into **Claude
Code**, the **Claude desktop app**, and **Ollama**, see
[`docs/INTEGRATIONS.md`](docs/INTEGRATIONS.md).

---

## Install

`journal` is a single static binary with no runtime dependencies (the binary
itself; embeddings and synthesis talk to Ollama/Anthropic over HTTP).

**Option A — download a release binary.** Grab the static binary for your
platform from the [releases page](https://github.com/ericmann/journal/releases)
(darwin/linux, arm64/amd64) and put it on your `PATH`:

```sh
install -m 0755 ./journal_v1.0.0_darwin_arm64 /usr/local/bin/journal
journal --version
```

**Option B — build from source** (Go 1.26+, `brew install go`):

```sh
git clone git@github.com:ericmann/journal.git
cd journal
make build                                          # version-stamped static binary
install -m 0755 ./journal /usr/local/bin/journal    # or anywhere on PATH
```

Cross-compile all platforms with `make release VERSION=v1.0.0` (output in
`dist/`). The binary is fully static (`CGO_ENABLED=0`) — no runtime deps.

Confirm:

```sh
journal --help
```

### Local models (for indexing & search — coming in Phases 2–3)

Retrieval runs entirely on your machine via [Ollama](https://ollama.com). Only
the embedding model is required:

```sh
ollama pull qwen3-embedding:4b    # required; outputs 2560-dim vectors
```

**Reranking is optional and off by default.** Ollama has no native rerank API
and there is no official reranker model, so `journal` does LLM-as-reranker: set
`reranker` in config to any small generate model you have (e.g. `qwen3:4b`) for
a precision lift, or leave it empty — vector search with `qwen3-embedding:4b` is
strong on its own.

> **Embedding dimension must match the model.** The default `embed_dim: 2560` is
> correct for `qwen3-embedding:4b`. If you use a different embedding model, run
> `journal doctor` — it probes the model and tells you the exact `embed_dim` to
> set (then `journal index --rebuild`).

`journal doctor` verifies Ollama is reachable and these models are present (and
checks the index schema + repo/config) before you rely on indexing:

```sh
journal doctor          # or: journal doctor --json
```

It prints an actionable per-check report and exits non-zero if anything is
wrong (Ollama down, a model missing, schema mismatch).

---

## Quick start

```sh
# 1. Initialize a journal repo in the current directory
journal init

# 2. Capture throughout the day (append-only, returns instantly, no embedding)
journal capture "Routing fallback isn't triggering when Qwen OOMs #cabot #litellm"
journal capture "Declaring the dev-fund payment as business income" \
    --project canton --marker decision
```

`journal init` creates:

```
.journal/
  config.yaml          # committed: models, chunk strategy, excludes (no secrets)
  index/               # gitignored: the disposable sqlite-vec database
daily/                 # the firehose: daily/YYYY/MM/YYYY-MM-DD.md
projects/              # long-lived threads: projects/<slug>/notes/ + _index.md
reflections/           # synthesis OUTPUT only (weekly drafts)
```

It also appends `.journal/index/` to `.gitignore`. Commit the rest; **never
commit `.journal/index/`** (it's a binary blob and fully rebuildable).

---

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
- **`@markers`** → structured annotations the query and synthesis layers key
  off. The recognized set is **`@decision`**, **`@question`**, **`@todo`**.
  Pass `--marker decision` _or_ write `@decision` inline.
- **Heading blocks (`##`)** are the unit of chunking and retrieval.

`--project <name>` routes a capture into `projects/<slug>/notes/YYYY-MM-DD.md`
instead of the daily file, using the same block format (the project name is
slugified, e.g. `"Canton COI"` → `canton-coi`).

### Command surface

```
journal init    [path]                          # bootstrap a repo
journal capture <text> [--tags a,b] [--project slug] [--marker decision|question|todo]
journal index   [--rebuild] [--since 2w]        # embed changed notes (one-shot)
journal search  <query> [--k 5] [--tag t] [--project slug] [--since 2w] [--json]
journal recent  [--tag t] [--project slug] [--since 1w] [--json]
journal decisions [--project slug] [--since 4w] [--json]
journal threads [--stale] [--days 14] [--json]
```

```
journal index --watch                           # continuous, debounced re-index
journal doctor [--json]                          # health checks
journal synth weekly|decisions|stale [--dry-run] [--write] [--project slug] [--days 14]
journal mcp [--repo path]                        # MCP server (stdio) for Claude Desktop
```

`journal mcp` exposes `search`/`recent`/`decisions`/`threads`/`capture` as MCP
tools (same JSON as `--json`) for the Claude desktop app — see
[`docs/INTEGRATIONS.md`](docs/INTEGRATIONS.md) §3b for the one-block config.

### Retrieval & queries

- **`search`** embeds your query (with a retrieval instruction), runs a
  brute-force vector KNN over the index, optionally reranks the top candidates
  (if a `reranker` is configured), and returns the best `--k` with
  `path:line_start-line_end` citations. With reranking off, results are in
  vector-distance order.
- **`recent`** / **`decisions`** are plain metadata queries (newest first);
  `decisions` filters to `@decision` blocks.
- **`threads`** summarizes project activity; `--stale` surfaces projects with no
  activity in `--days` (default 14).

Every read command supports **`--json`**. Results use a stable schema:

```json
{ "results": [ { "path": "daily/2026/06/2026-06-01.md", "line_start": 3,
  "line_end": 5, "heading": "09:14 #cabot", "snippet": "…", "score": 0.87,
  "tags": ["cabot"], "markers": [] } ] }
```

`threads --json` emits `{ "threads": [ { "project", "last_activity", "chunks",
"open_questions", "stale", "days_since" } ] }`. On failure, commands emit
`{ "error": "…" }` to stdout and exit non-zero — so an empty result set is
distinguishable from an error.

---

## The watcher

Indexing stays fresh via a long-running `journal index --watch`: it does an
initial index, then debounces filesystem events and re-indexes only changed
files (deletions remove their chunks). Ctrl-C stops it cleanly.

The **recommended way to run it is a dedicated `tmux` pane** — simple, visible,
and easy to restart:

```sh
tmux new-session -d -s journal 'cd ~/journal && journal index --watch'
tmux attach -t journal     # watch it;  Ctrl-b d to detach
```

### Optional: run it unattended via launchd (macOS)

If you'd rather it survive logout/reboot, install a per-user launchd agent.
Create `~/Library/LaunchAgents/com.ericmann.journal-watch.plist`:

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

The **tmux pane is the documented default**; launchd is there if you want it
hands-off.

---

## Synthesis (cloud Claude)

`journal synth` turns the indexed firehose into curated drafts using the
Anthropic API. It runs scheduled or on demand — never in the capture/search hot
path.

```sh
journal synth weekly                 # dry-run by default: prints prompt + target path
journal synth weekly --write         # calls Claude, writes reflections/YYYY-Www.md (DRAFT)
journal synth decisions --project canton --write   # appends a marked rollup to its _index.md
journal synth stale --days 21        # surface threads idle > 21 days
```

- **`--dry-run` is the default** (and is explicit-safe): it assembles and prints
  the prompt and the intended output path, and makes **no** network call and
  **no** file write — useful for cost control and verifying token boundaries.
- **`--write`** calls the API and writes output. `weekly`/`stale` write a new
  file under `reflections/` (refusing to clobber an existing one, since you edit
  those). `decisions --project <slug>` appends a **clearly-marked rollup block**
  to `projects/<slug>/_index.md` — it never mutates your note bodies.
- Requires **`ANTHROPIC_API_KEY` in the environment** (only for `--write`); it's
  never written to config or logged. The model is `synth_model` in config.

### Writing in your voice

If `docs/VOICE_PROFILE.md` exists (path configurable via `voice_profile`), its
contents are injected into every synthesis prompt as a **style reference** so
drafts sound like you — matching your language patterns and honoring any
anti-AI word/phrase guardrails it lists. The profile is treated as style only:
the prompt explicitly tells the model to ignore meta-instructions in it (e.g.
"ask which platform") since the destination is fixed. Evolve the profile over
time; it's plain markdown. Omit the file and synthesis still works, just without
the voice section.

## Configuration & secrets

Non-secret settings live in `.journal/config.yaml` (committed):

```yaml
embed_model: qwen3-embedding:4b
reranker: ""                          # optional; e.g. qwen3:4b to enable reranking
ollama_base_url: http://localhost:11434
chunk_strategy: heading
embed_dim: 2560                       # must match the embed model (doctor verifies)
excludes:
  - reflections/**
  - .journal/**
store_path: .journal/index/journal.db
synth_model: claude-sonnet-4-6        # Anthropic model for `journal synth`
synth_max_tokens: 4096
voice_profile: docs/VOICE_PROFILE.md  # optional; injected into synth prompts
```

**Secrets never go in config.** The Anthropic API key for synthesis is read from
the `ANTHROPIC_API_KEY` environment variable only, and is never written to disk
or logged. Workspace separation (personal vs. A8c) is enforced by which repo
you're in and which env is loaded — clone the repo elsewhere and export a
different key.

---

## A second, isolated workspace (e.g. Displace)

The whole pattern clones to a separate workspace by copying a repo and swapping a
config + env token — no shared state:

```sh
# a brand-new, independent journal repo (or `git clone` an existing one)
journal init ~/displace-journal
cd ~/displace-journal

# its own gitignored index, built from its own notes:
journal index

# its own synthesis token, supplied by the environment (never stored in config):
ANTHROPIC_API_KEY=$A8C_TOKEN journal synth weekly --write
```

Each repo resolves its own root (the nearest `.journal/`), keeps its own
`.journal/index/journal.db` (gitignored), and reads whatever `ANTHROPIC_API_KEY`
is in the environment at invocation. There is no cross-workspace contamination —
searching one repo never returns another's notes. Workspace separation is
enforced by *which repo you're in* and *which env is loaded*, not by the tool
holding multiple profiles. (Verified by `TestWorkspaceIsolation`.)

## Development

```sh
make build    # go build -o journal .
make test     # unit + integration tests (no network; fakes for Ollama/Anthropic)
make cover    # coverage summary
make lint     # gofmt check + go vet (+ golangci-lint if installed)
make fmt      # gofmt -w .
```

CI runs build, `gofmt`, `go vet`, race-enabled tests, and `golangci-lint` on
every push and PR (see [`.github/workflows/ci.yml`](.github/workflows/ci.yml)).

Tests never touch the network: the Ollama and Anthropic clients sit behind
interfaces with deterministic fakes, and integration tests use a temp-file
`sqlite-vec` database.

---

## Roadmap

| Phase | Scope | Status |
|-------|-------|--------|
| 1 | Repo skeleton, config, note format, `capture`, `init` | ✅ done |
| 2 | `sqlite-vec` store: schema, migrations, CRUD/KNN | ✅ done |
| 3 | Chunking + hashing, embed client, `index`, `search`, structured queries | ✅ done (MVP) |
| 4 | `index --watch` (debounced), `doctor` | ✅ done |
| 5 | Synthesis: Anthropic client, prompt assembly, `synth` | ✅ done |
| 6 | `skills/journal/SKILL.md`, second-workspace validation | ✅ done |

See [`docs/TDD.md`](docs/TDD.md) §7 for the per-story acceptance criteria.
