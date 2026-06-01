# journal

A local-first developer journal with semantic retrieval and AI-assisted synthesis.

`journal` turns a folder of plain-markdown notes into a searchable, AI-queryable
corpus. Capture is frictionless and append-only; retrieval is a local RAG stack
(Ollama embeddings + reranking, vectors in `sqlite-vec`); weekly synthesis is
backed by cloud Claude. **Markdown in git is the single source of truth** — the
vector index is a disposable, rebuildable cache and is never committed.

> **Status:** built phase by phase against [`docs/TDD.md`](docs/TDD.md).
> Phase 1 (capture + repo skeleton) is complete. Indexing, search, the watcher,
> synthesis, and the Claude Code skill land in later phases — see
> [Roadmap](#roadmap). Sections below are marked _(coming in Phase N)_ where the
> feature isn't wired up yet.

---

## Why

Working notes get scattered across loose text files, per-project `/docs`, and
paper — none of it discoverable or reusable. `journal` keeps one durable
substrate (markdown in git) and adds a thin tool layer that makes capture
zero-friction and retrieval excellent, without a server, daemon, or cloud store.

The full rationale, architecture, and acceptance criteria live in
[`docs/TDD.md`](docs/TDD.md). Build-time decisions and deviations are tracked in
[`docs/DECISIONS.md`](docs/DECISIONS.md).

---

## Install

`journal` is a single static binary with no runtime dependencies (the binary
itself; embeddings and synthesis talk to Ollama/Anthropic over HTTP).

**Prerequisite:** Go 1.26+ to build (`brew install go`).

```sh
git clone git@github.com:ericmann/journal.git
cd journal
make build            # produces ./journal
```

Put it on your `PATH`:

```sh
install -m 0755 ./journal /usr/local/bin/journal   # or anywhere on PATH
```

Confirm:

```sh
journal --help
```

### Local models (for indexing & search — coming in Phases 2–3)

Retrieval runs entirely on your machine via [Ollama](https://ollama.com):

```sh
ollama pull qwen3-embedding:4b    # default embedding model
ollama pull qwen3-reranker        # reranker for precision
```

`journal doctor` _(coming in Phase 4)_ verifies Ollama is reachable and these
models are present before you rely on indexing.

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
```

_Coming in later phases:_ `index [--watch|--rebuild|--since]`, `search`,
`recent`, `decisions`, `threads`, `synth weekly|decisions|stale`, `doctor`.
Every read command will support `--json` with a stable schema.

---

## The watcher _(coming in Phase 4)_

Indexing stays fresh via a long-running `journal index --watch`. The
**recommended way to run it is a dedicated `tmux` pane** — simple, visible, and
easy to restart:

```sh
tmux new-session -d -s journal 'cd ~/journal && journal index --watch'
# attach to watch it:  tmux attach -t journal
```

If you'd rather have it survive reboots unattended, a `launchd` user agent is
also supported; that recipe will be documented here when the watcher ships.

---

## Configuration & secrets

Non-secret settings live in `.journal/config.yaml` (committed):

```yaml
embed_model: qwen3-embedding:4b
reranker: qwen3-reranker
ollama_base_url: http://localhost:11434
chunk_strategy: heading
embed_dim: 1024
excludes:
  - reflections/**
  - .journal/**
store_path: .journal/index/journal.db
```

**Secrets never go in config.** The Anthropic API key for synthesis is read from
the `ANTHROPIC_API_KEY` environment variable only, and is never written to disk
or logged. Workspace separation (personal vs. A8c) is enforced by which repo
you're in and which env is loaded — clone the repo elsewhere and export a
different key.

---

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
| 2 | `sqlite-vec` store: schema, migrations, CRUD/KNN | ⏳ |
| 3 | Chunking + hashing, embed client, `index`, `search`, structured queries | ⏳ (MVP) |
| 4 | `index --watch` (debounced), `doctor` | ⏳ |
| 5 | Synthesis: Anthropic client, prompt assembly, `synth` | ⏳ |
| 6 | `skills/journal/SKILL.md`, second-workspace validation | ⏳ |

See [`docs/TDD.md`](docs/TDD.md) §7 for the per-story acceptance criteria.
