# Build decisions & deviations

A running log of decisions made while building `journal` against
[`_internal/TDD.md`](_internal/TDD.md) — especially anything that deviates from or extends the TDD,
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

## Phase 5 — Synthesis

### Synthesis model default: `claude-sonnet-4-6` (configurable) ⚠️
`synth_model` defaults to **`claude-sonnet-4-6`** — a cost/quality balance for a
recurring, scheduled job. The TDD calls for "strong long-context reasoning";
**`claude-opus-4-8`** is the most capable option and a one-line config change if
you want maximum weekly-reflection quality. Flagged for your call; either works.
`synth_max_tokens` defaults to 4096.

### `--dry-run` is the default; `--write` is explicit
Synthesis costs money and makes a network call, so the command **defaults to
dry-run** (assemble + print prompt and intended path, no API call, no write).
You must pass `--write` to actually call Claude. A test asserts dry-run makes
zero client calls and writes nothing; the dry-run path uses a `noopClient` that
errors if ever invoked.

### Output writing rules (never mutate user content)
- `weekly` → `reflections/YYYY-Www.md`; `stale` → `reflections/stale-<date>.md`.
  Both **refuse to overwrite** an existing file (you edit those drafts).
- `decisions --project <slug>` → appends a **clearly-marked block**
  (`<!-- journal:decisions-rollup ... -->` … `<!-- /journal:decisions-rollup -->`)
  to `projects/<slug>/_index.md`, preserving every existing byte. Append-only;
  re-running adds another dated block rather than rewriting.

### Prompt assembly is pure + golden-file tested
`AssembleWeekly/Decisions/Stale` are pure functions over gathered chunks, tested
against `internal/synth/testdata/*.golden` (regenerate with `go test -update`).
The runner gathers from the store (which holds note bodies), so synthesis needs
no second read path.

### Voice profile injected into synthesis prompts
A `voice_profile` config key (default `docs/VOICE_PROFILE.md`, optional) points
to a markdown description of the author's writing voice. When present, the
runner reads it and the prompt assembler injects it into every synth prompt as a
delimited **style reference**, with an explicit instruction to (a) honor its
anti-AI word/phrase guardrails and (b) *ignore* any meta-instructions inside it
(e.g. "ask which platform") since the destination is fixed. Assembly stays pure
(voice passed as a param, golden-tested both with and without); the runner does
the file read. Synthesis was not in the original design — the author wrote
weekly reflections by hand — so this keeps the drafts close to that voice.

### Anthropic client: key from env, never logged
`NewAnthropic(key)` takes the key (read via `config.AnthropicAPIKey()` from
`ANTHROPIC_API_KEY` only). Errors include the response body but **never** the
key (a test asserts this). The runner reports a one-line summary with token
counts + output path — no secrets.

### store.Open now creates its parent directory
The index dir is a rebuildable cache that may be deleted; `store.Open` now
`MkdirAll`s the parent (skipped for `:memory:`), so `index`/`synth` work even if
`.journal/index/` was removed.

## Phase 6 — SKILL.md + second-workspace validation

### `skills/journal/SKILL.md`
Authored the Claude Code skill: prefer `search --json`, read the stable result
schema, always cite `path:line_start-line_end`, choose between
search/recent/decisions/threads, distinguish `{error}` from `{results:[]}`, and
don't run `index`/`synth --write` unprompted. Discoverable from the repo or via
`~/.claude/skills/journal` (see INTEGRATIONS.md §2).

### Workspace isolation validated
`TestWorkspaceIsolation` proves two independent repos index + search with their
own gitignored `.journal/index/journal.db` and never leak into each other;
`TestWorkspaceSeparateSecrets` confirms the API key is per-environment (never in
config). README documents the clone-to-second-workspace recipe. No tool-held
profiles — separation is "which repo + which env", exactly as the TDD intends.

## Post-Phase-6 — MCP server (`journal mcp`)

Shipped a first-party MCP server (the shim noted earlier as future work) so the
Claude desktop app gets the same retrieval as Claude Code.

- Built on the **official `github.com/modelcontextprotocol/go-sdk` v1.6.1** (pure
  Go; typed `AddTool` auto-generates input schemas). Chosen over hand-rolling
  JSON-RPC for protocol correctness (can't test against Desktop locally) and
  over `mark3labs/mcp-go` since the official SDK is now a stable v1.
- Transport: stdio (newline-delimited JSON-RPC). `journal mcp [--repo path]`
  binds to a workspace; tools reuse the in-process `run*` logic (no self-exec),
  so output is byte-identical to the CLI `--json`.
- Tools: `search`, `recent`, `decisions`, `threads`, `capture`. Errors return a
  tool-error result with the same `{"error":…}` shape (distinct from empty).
- Verified end-to-end over the real JSON-RPC handshake (initialize → tools/list
  → tools/call). Handler logic is unit-tested with the fake embedder.
- Added a `version` var (default `dev`, set via `-ldflags` at release) surfaced
  as `journal --version` and the MCP serverInfo.

## v1.0.1 — reranker + embed_dim defaults corrected (dogfooding)

Setting up against a real Ollama surfaced two wrong assumptions inherited from
the generated TDD:

- **`qwen3-reranker` is not a real Ollama model**, and Ollama has **no native
  rerank API** (confirmed: only community uploads like
  `dengcao/Qwen3-Reranker-*`; standard Ollama can't serve cross-encoder
  rerankers). Fix: **reranking is now optional and off by default**
  (`reranker: ""`). When set, it's LLM-as-reranker via `/api/generate` with any
  generate model (e.g. `qwen3:4b`). `search` skips reranking entirely when
  unset and uses vector-KNN order; `doctor` treats a missing/unset reranker as
  informational (never fails the verdict). Vector search with
  `qwen3-embedding:4b` is strong on its own, so this is a sensible default.
- **`embed_dim` default was 1024 but `qwen3-embedding:4b` outputs 2560.** Fix:
  default is now **2560**. `journal doctor` gained an **embed_dim probe** (embeds
  a test string, compares to config, prints the exact value to set), and the
  indexer now fails with an actionable "set embed_dim: N and --rebuild" message
  instead of a raw length error.

Lesson: the generated TDD specified plausible-sounding model names/dims that
didn't survive contact with the actual Ollama registry — validated and corrected
during first real setup.

## v1.1.0 — auto-commit note changes

Dogfooding surfaced a real footgun: `capture` appends but doesn't commit, so a
day's notes could sit uncommitted. Fix: `index` and `index --watch` now
auto-commit note changes (new `internal/vcs` package shelling out to git).

Design choices:
- **Where:** in `index`/`watch`, not `capture` (capture must stay <2s and pure).
  Once the watcher runs, every note is committed within the debounce window.
- **Default on** (`git_autocommit: true`) — a safety net you must remember to
  enable isn't one. No-op if git is absent or the folder isn't a git repo.
- **Only commits a git *top level*** (`vcs.IsRepoRoot` compares
  `rev-parse --show-toplevel` to the resolved root) — never commits notes into a
  parent repository. The watcher also stops watching `.git/` to avoid churn.
- **`git add -A`** so the whole notes repo is captured; the gitignored index is
  naturally excluded (tested).
- **Unsigned by default** (`git_autocommit_sign: false`): the user signs commits
  via 1Password (interactive), and an unattended watcher signing every note
  would fire a biometric prompt per capture. Auto-commits are mechanical
  snapshots; signing is opt-in. (This intentionally diverges from the
  sign-everything preference for *this codebase* — notes-repo auto-commits have
  different ergonomics.)
- **Non-fatal:** commit failures are logged; markdown is always safe on disk.
- Messages are auto-generated with a little personality (rotating verbs +
  chunk deltas + timestamp).

## v1.1.1 — capture also auto-commits

v1.1.0 only auto-committed during `index`/`index --watch`, on the rationale that
`capture` stays pure. But dogfooding showed the obvious gap: a user who captures
without immediately indexing (and isn't running the watcher) still ends up with
uncommitted notes — the exact footgun the feature targets. Fix: **`capture` now
auto-commits too** (`autoCommitCapture` → `vcs.CommitAll` with a capture-specific
message), gated by the same `git_autocommit` and only when the repo root is a git
top level.

A local commit of one small file is ~100ms (well under capture's 2s budget) and
unsigned by default (no prompt), so there's no reason to defer it. `capture()`
itself stays pure (no git) — the commit lives in the command's RunE — so the
perf unit test and the function contract are unchanged. Result: your words are
committed the instant you capture; `index`/`watch` commit any direct file edits
and keep the search index fresh.

## v1.2.0–v1.4.0 — backup, editor capture, public hardening

- **`journal sync` for remote backup (v1.2.0).** Auto-commit protects notes
  locally; sync gets them to a git remote. The git logic lives in a testable Go
  command (`internal/vcs` + `cmd/sync`), with `.journal/sync.sh` as a thin cron
  wrapper — not bash git-plumbing.
- **Editor/stdin capture (v1.3.0).** `journal capture` with no text opens an
  editor (git-style precedence: `$JOURNAL_EDITOR` → `editor` config → `$VISUAL` →
  `$EDITOR` → `nano`) or reads piped stdin. Empty file aborts.
- **Tag/marker boundary tightened (v1.3.1).** Extraction required only a non-word
  char before `#`/`@`, so URL fragments (`/#comment-9835`) and markdown anchor
  links (`[x](#summary)`) were mis-parsed as tags. Boundary is now
  start-of-text-or-whitespace, matching hashtag convention.
- **Sync is off by default + `sync_conflict` (v1.4.0).** Because sync can rewrite
  local history on a divergence, it is opt-in (`sync_enabled`, default false) and
  the default conflict mode is **`manual`** — abort and let the user resolve,
  never silently discard work. `prefer-upstream`/`prefer-local` auto-resolve only
  when explicitly chosen.
- **Friendly Ollama guidance (v1.4.0).** A typed `embed.ErrUnreachable` lets
  `index`/`search`/`synth` append actionable setup steps instead of a raw
  "connection refused"; fresh `init` prints first-run next steps.
- **Capture stdin hardening (v1.4.0).** `golang.org/x/term` distinguishes a TTY
  (editor) from piped input (read it) from neither (e.g. `/dev/null` → clear
  error), instead of a char-device heuristic that could hang or launch a doomed
  editor.
- **Distribution (v1.4.0).** Homebrew via `displacetech/tap`; copyright is
  Displace Technologies, LLC. Code repo stays at `ericmann/journal`.
- **GoReleaser release pipeline (v1.4.1).** Replaced the hand-rolled `make
  release` with GoReleaser (`.goreleaser.yaml` + tag-triggered
  `release.yml`): tar.gz archives, `checksums.txt` **cosign-keyless** signed via
  GitHub OIDC, nfpm `.deb/.rpm/.apk`, and the Homebrew **cask** pushed to
  `DisplaceTech/homebrew-tap` (cask, not formula — GoReleaser deprecated `brews`,
  and casks are the recommended path for prebuilt binaries). A checksum-verifying
  `install.sh` covers Linux/macOS without a package manager. The repo went public
  to make release assets downloadable unauthenticated.

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
