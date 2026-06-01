# Build decisions & deviations

A running log of decisions made while building `journal` against
[`TDD.md`](TDD.md) — especially anything that deviates from or extends the TDD,
so future-you doesn't have to reverse-engineer the "why".

Format: newest first. Each entry notes the decision, the rationale, and (if
relevant) what the TDD said.

---

## Phase 1 — Capture + repo skeleton

### Module path: `github.com/ericmann/journal`
Chosen at scaffold time (personal GitHub). All internal imports use this path.

### Added `journal init` (not in the TDD command surface)
The TDD §6 command surface lists `capture`/`index`/`search`/… but no `init`.
Capture needs a bootstrapped repo (a `.journal/` marker) to find the repo root
and write into. Rather than have `capture` silently create a repo in an
arbitrary CWD, `init` makes repo creation explicit: it writes
`.journal/config.yaml` (defaults), the `daily/projects/reflections` skeleton,
and a `.gitignore` entry for the index. It never clobbers an existing config.

### Project captures use the daily block format
The TDD shows `projects/<slug>/notes/` as a directory. Project captures are
written to `projects/<slug>/notes/YYYY-MM-DD.md` using the **same**
`# YYYY-MM-DD` H1 + `## HH:MM` block format as daily files, so the Phase 3
chunker can treat daily and project notes uniformly (one format definition in
`internal/note`, shared by the writer and the chunker via `ParseBlockHeading`).

### `embed_dim` default = 1024
The TDD leaves the embedding dimension configurable and notes `qwen3-embedding`
supports flexible dims. 1024 is the scaffolded default; it MUST match the live
model's output dimension and will be validated against Ollama at index time
(Phase 2/3). The `vec_chunks` virtual table is declared `float[embed_dim]`.

### Tags/markers: union of flags and inline tokens, case-folded + deduped
`capture` merges `--tags`/`--marker` flag values with any `#tag`/`@marker`
tokens found inline in the note text. Tags are lower-cased; markers are
restricted to the recognized set (`decision`, `question`, `todo`). Order is
first-seen. The note **body is never mutated** — inline tokens are also hoisted
onto the block header for retrieval, but the original text is preserved verbatim.

---

## Phase 3 — Index + search + structured queries (MVP)

### Reranking: LLM-as-reranker via `/api/generate`, with graceful fallback ⚠️
Ollama has no dedicated rerank endpoint. `Ollama.Rerank` scores each candidate
by prompting the reranker model (`/api/generate`, temperature 0) to emit a 0–10
relevance number, parsed and normalized to [0,1], run over a bounded worker
pool. `search` reranks the KNN candidates and, **if reranking errors**, falls
back to vector-distance order (score = `1/(1+distance)`) so search still works.

> **Needs validation during dogfooding:** whether `qwen3-reranker` behaves well
> through `/api/generate`, and the latency of reranking ~50 candidates against
> the <5s search budget. If it's too slow or the model isn't suited to the
> generate path, options are: lower `candidateN`, lower the rerank worker count,
> or swap to a proper cross-encoder rerank endpoint when Ollama exposes one. The
> reranker sits behind the `embed.Embedder` interface, so this is a localized
> change.

### `--json` schema + error/empty distinction
Read commands emit a stable schema. `search`/`recent`/`decisions` use
`{"results":[{path,line_start,line_end,heading,snippet,score,tags,markers}]}`.
`threads` is project-shaped, so it uses `{"threads":[{project,last_activity,
chunks,open_questions,stale,days_since}]}`. On failure, commands emit
`{"error":"..."}` to stdout and exit non-zero (via an internal `errSilent`
sentinel so the root doesn't also print to stderr) — so a machine consumer can
tell an error from a legitimately empty result set.

### Search candidate count
`candidateN = 50` nearest chunks fetched by brute-force KNN, then reranked down
to `--k` (default 5). No ANN index (per the TDD; brute force is fine at ≤25k).

### Citations
Results render as `path:line_start-line_end`. Line numbers track the `##` block:
`line_start` is the heading line, `line_end` the last non-blank content line.

## Phase 4 — Watch + doctor

### Watcher: fsnotify + debounce, re-index only changed files
`journal index --watch` uses `fsnotify` (pure Go). It watches every
non-excluded directory (adding newly-created dirs as they appear), debounces
event bursts (default 500ms), and re-indexes only the files that changed. A
deleted file is re-indexed as empty content, which removes its chunks. The
`.journal/index/**` exclude keeps the DB's own writes from causing a feedback
loop. `ProcessChanges` is the unit-tested core; `Run` is covered by a real
fsnotify end-to-end test. Ctrl-C/SIGTERM cancels via a signal-bound context for
clean shutdown.

### Watcher delivery default: tmux pane (launchd documented as optional)
Confirmed default is a dedicated tmux pane; the README also documents a launchd
per-user agent for hands-off operation.

### doctor: injected model-lister for network-free tests
`runDoctor` takes a `modelLister` interface (just `Tags`), so health checks are
tested with a fake. Checks: repo/config, Ollama reachability, embed + rerank
model presence (tolerating a missing `:tag`), and index schema/chunk count. The
Anthropic key is informational only (synth-only) and never fails the verdict.
Exits non-zero on any failure; `--json` emits `{ok, checks:[{name,ok,detail}]}`.

### Integrations documented
[INTEGRATIONS.md](INTEGRATIONS.md) covers Claude Code (the first-class path via
`skills/journal/SKILL.md` + `search --json`), the Claude desktop app (filesystem
connector now; an MCP shim wrapping the stable `--json` as the recommended
retrieval path — not yet shipped), and Ollama (local embed/rerank; cloud Claude
for inference/synth). A first-party `journal mcp` subcommand is noted as natural
future work.

## Tooling / process

### Commit signing
Commits are SSH-signed via 1Password using the key `eric@eamann.com`
(`SHA256:bI2Sr+FYrVtGZkzTO39bIh5ugSl8PKe1qi6GWcjUGKE`). Local verification uses
`~/.config/git/allowed_signers` (configured globally via
`gpg.ssh.allowedSignersFile`). If 1Password can't unlock the key in a given
environment, fall back to unsigned commits rather than blocking.

### Watcher delivery default: tmux pane
Per the TDD Open Question, the documented default for running
`journal index --watch` is a dedicated `tmux` pane (simple, visible, easy to
restart). A `launchd` user-agent recipe will also be documented when the
watcher ships in Phase 4.

### CI
GitHub Actions (`.github/workflows/ci.yml`) runs on push/PR to `main`:
`gofmt` check, `go vet`, `go build`, the test suite with coverage, a scoped race
run, and `golangci-lint` (pinned v1.64.8).

Two CI-specific gotchas hit and fixed:

- **golangci-lint vs go1.26 module:** the prebuilt v1.64.8 binary is built with
  go1.24 and cannot *typecheck* a module whose `go.mod` targets go1.26 (its
  go/packages loader rejects the packages; `run.go` in config doesn't help). Fix:
  `install-mode: goinstall` so the action builds golangci-lint from source with
  CI's Go 1.26 toolchain — exactly how it works locally.
- **Race detector vs the sqlite-vec WASM runtime:** `go test -race ./...` faults
  intermittently on linux/amd64 (`fatal error: fault` / `unsafe.Slice: len out
  of range`) inside wazero while executing the SQLite WASM — a wazero/runtime
  interaction, not our code (passed under race in one run, crashed the next).
  Fix: the full suite runs **without** `-race` (with coverage), and `-race` runs
  only on `internal/embed` (the concurrency-sensitive package — the rerank
  worker pool — that doesn't touch wazero). Local `go test -race ./...` on
  darwin/arm64 happens to pass, but we don't rely on it in CI.

### sqlite-vec loading: pure-Go via `ncruces/go-sqlite3` (no cgo) ✅
Ground rule: verify the `sqlite-vec` extension actually loads before building on
it, preferring a pure-Go (no-cgo) driver. I spiked three paths:

- **`modernc.org/sqlite`** — pure Go, but cannot load the `sqlite-vec` C
  extension. Not viable.
- **cgo `mattn/go-sqlite3` + `sqlite-vec-go-bindings/cgo`** — compiles, but the
  bindings register vec via `sqlite3_auto_extension`, which is a **no-op on
  Apple platforms**, so `vec_version()` is "no such function". Would need a
  loadable `.dylib` shipped alongside the binary (breaks single-static-binary).
- **`ncruces/go-sqlite3` (WASM via wazero) + `sqlite-vec-go-bindings/ncruces`**
  — **pure Go, `CGO_ENABLED=0`, works.** sqlite-vec is statically compiled into
  the bundled WASM. Full KNN round-trip verified.

**Decision:** use the ncruces path. Version lock matters and is narrow — the
bindings (v0.1.6) require `sqlite3.Binary` (removed in newer ncruces), and the
vec-enabled WASM needs wazero atomics, which ncruces **only enables in
v0.20.3–v0.21.3** (v0.22.0+ disabled them by default, giving
`i32.atomic.store ... feature "" is disabled` at first query). We pin
**v0.21.3** (highest in the working window). This keeps the single-static-binary,
easy-cross-compile property the project is built around (`CGO_ENABLED=0`).

> If we later need a newer SQLite, the upgrade path is to publish a vec-enabled
> WASM built for current ncruces (or move to the cgo path with a per-connection
> `sqlite3_vec_init` hook). Tracked as future work, not needed at this scale.

Driver registration: import `_ "github.com/ncruces/go-sqlite3/driver"` (registers
the `sqlite3` database/sql driver) and `sqlite-vec-go-bindings/ncruces` (sets the
vec-enabled WASM binary + provides `SerializeFloat32`).
