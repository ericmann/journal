# CLAUDE.md

Project context for AI coding agents (Claude Code, both local and CI). Read this first
on every task. It tells you the stack, how the code is organized, how to run and test
things, the conventions to follow, and what is off-limits.

Also read **[docs/LEARNINGS.md](docs/LEARNINGS.md)** — failure modes captured from real
agent runs and the "why" behind the rules here — and **[docs/DECISIONS.md](docs/DECISIONS.md)**,
the project's ADR-style decision log (it explains non-obvious choices like the serial test
run). The rules below are imperative; LEARNINGS/DECISIONS explain where they came from.

---

## What this is

`journal` is a **local-first developer journal**: a single static Go binary that turns a
folder of plain-markdown notes (in a git repo) into a searchable, AI-queryable corpus —
no server, daemon, or cloud store. Capture is frictionless and append-only; retrieval is
a fully local RAG stack (Ollama embeddings + optional reranking, vectors in `sqlite-vec`);
synthesis is on-demand via cloud Claude (default), any OpenAI-compatible endpoint, or a
fully local Ollama model. It also ships an **MCP server** exposing search/recent/decisions/
meetings to Claude Desktop and Claude Code.

**Markdown in git is the single source of truth. The vector index is a disposable,
rebuildable cache and is never committed.**

## Stack

- **Go 1.26** (`go.mod` pins the toolchain; CI builds the linter from source against it)
- **CLI:** `spf13/cobra` + `pflag` — one command per file under `cmd/`
- **Storage:** `sqlite-vec` for vectors, run as **SQLite-on-WASM via `wazero`** (pure Go,
  no cgo) — this is why tests run serially (see Testing)
- **Embeddings/RAG:** local **Ollama** (`internal/embed`), chunking/hashing in `internal/index`
- **Synthesis:** `internal/synth` — cloud Claude, OpenAI-compatible, or local Ollama
- **Distribution:** single binary via **GoReleaser**; Homebrew tap
- Package manager: **Go modules**. Lint: **golangci-lint v1.64.8**. Format: **gofmt**.

## Directory map

```
main.go             Entry point; wires cobra root.
cmd/                One cobra command per file (capture.go, search.go, synth.go, todos.go,
                    done.go, index.go, tui.go, mcp.go, quill_sync.go, transcribe.go, sync.go,
                    doctor.go, stats.go, show.go, …), each with a sibling *_test.go.
                    root.go assembles the command tree; common.go/output.go/render.go are shared.
                    cmd/templates/  note/output templates.
internal/
  config/           Loads/validates non-secret settings (secrets come from env, never files).
  note/             On-disk markdown note format + parsing (@todo extraction, frontmatter).
  index/            Markdown → stable-hashed chunks; keeps the index in sync with notes.
  store/            sqlite-vec persistence: schema, migrations, vector queries (WASM/wazero).
  embed/            Embedding + reranking boundary to local Ollama (concurrency-sensitive).
  synth/            Assembles prompts from gathered notes; runs synthesis jobs (multi-provider).
  editor/           Resolves/launches the user's $EDITOR for a note.
  transcribe/       WhisperX JSON → indexable transcript.
  quill/            Quill meetings ingestion.
  vcs/              git operations (auto-commit capture/index).
docs/               USAGE, CONFIGURATION, DATA-FLOWS, DECISIONS (ADRs), SYNTHESIS, etc.
skills/             Packaged skills.
.github/            CI (ci.yml), release (release.yml), and the agentic-workflow layer.
```

## How to run tests, lint, and build

Use the Makefile (it encodes the right flags — don't hand-roll `go test` invocations):

```bash
make build     # go build -> ./journal
make test      # go test -p 1 ./...   (serial — see below)
make cover     # coverage profile + summary
make vet       # go vet ./...
make fmt       # gofmt -w .
make lint      # vet + golangci-lint (v1.64.8)
make tidy      # go mod tidy
```

### Testing — match these conventions

- **CI (`.github/workflows/ci.yml`) is the real gate.** On every PR it runs: gofmt check
  (must be clean), `go vet ./...`, `go build ./...`, `go test -p 1 -coverprofile ./...`,
  and the **race detector on `internal/embed` only**, plus golangci-lint.
- **`go test -p 1` is deliberate — keep it.** Multiple packages run SQLite as WASM via
  wazero, and parallel test binaries race on wazero's on-disk compile cache, faulting
  intermittently on linux/amd64. Serial is fast and stable. Do **not** "optimize" tests to
  run packages in parallel, and do **not** add `-race` broadly — the WASM store faults under
  `-race` on linux/amd64 (a wazero/runtime issue, not our code). See `docs/DECISIONS.md`.
- **Tests live next to the code** as `<file>_test.go` (every `cmd/*.go` has one). Prefer
  **table-driven tests** and the standard library `testing` — match the surrounding files
  (read a sibling `*_test.go` before writing yours).
- **Never call real network services in tests.** No real Ollama, cloud Claude, OpenAI-compatible
  endpoints, Quill, or git remotes. Use the existing fakes/fixtures and local-only paths
  (see `cmd/local_only_test.go`, `cmd/providers_test.go`, `internal/synth/testdata/`). A test
  that needs the network is wrong — make the boundary injectable instead.
- **Every behavior change ships with tests.** A command/flag/output change updates or adds the
  matching `*_test.go`. When you change output or behavior, grep for tests asserting the old
  contract and update them — a green suite you silently broke is a failed task.
- **Never pair a hardcoded fixture date with an unpinned rolling `since` window** — it's a silent
  time-bomb. Either use `time.Now().Format("2006-01-02")` for the fixture date (content-in-window
  tests) or pin `now` via a `pinNow` helper and use hardcoded dates relative to that pinned instant
  (old-vs-recent boundary tests, as in `cmd/dismiss_test.go`). See docs/LEARNINGS.md.
- **CI agents (the `agent-ready-trigger` runner) may not have a Go toolchain or Ollama.** If you
  can't run `make test`, write tests rigorously and trace them against your implementation;
  `ci.yml` on the PR executes them. Never knowingly open a red PR.

## Working from an issue — reassess before you build

Issues are written ahead of time and the codebase keeps moving. **Before you implement,
reconcile the issue's notes against the current code.** If it references a file, command, or
pattern that has since changed, follow current reality, not the stale instruction, and note
the deviation in the PR body. If the divergence makes the intended approach wrong, **stop and
comment on the issue** rather than building the wrong thing.

## Conventions to follow

- **One cobra command per file in `cmd/`**, registered through `root.go`. Keep command files
  thin: parse/validate flags, call into `internal/*`, render via the shared output helpers
  (`output.go`/`render.go`). Business logic belongs in `internal/`, not in `cmd/`.
- **Errors are wrapped with context** (`fmt.Errorf("…: %w", err)`) and returned, not logged-and-
  swallowed; commands surface them to cobra. Match the existing style in neighboring files.
- **Local-first is a hard invariant.** Retrieval and indexing must work with zero network egress;
  anything that reaches the network is opt-in and behind a configured provider. Don't introduce a
  required cloud dependency.
- **Secrets come from the environment**, never committed or written to config files. `internal/config`
  loads non-secret settings only.
- **Keep it gofmt-clean and vet-clean** — CI fails otherwise. Run `make fmt vet` before finishing.
- **In `httptest` server handlers, write `_, _ = w.Write(…)`** — bare `w.Write(…)` passes `go vet` and tests but fails the `errcheck` linter.
- Commit style is **Conventional Commits** (`feat:`, `fix:`, `chore:`, …). Branches: `feature/…`,
  `fix/…`, and the agent flow uses `agent/issue-<n>`.
- **PR issue links use `Closes #NNN`** (with the `#`) — a bare number won't auto-close.

## Off-limits / be careful

- **Never commit the vector index or any `*.sqlite*` store** — it's a rebuildable cache. Markdown
  in git is the source of truth.
- Do **not** edit `.goreleaser.yaml`, `release.yml`, `install.sh`, or the Homebrew tap unless the
  issue is explicitly about release/packaging.
- Do **not** weaken the test setup to make something pass (don't drop `-p 1`, don't add broad
  `-race`, don't reach the network in tests).
- Do **not** print, commit, or hardcode secrets. Stay inside the issue's **"Out of scope"** — it's
  a hard line.

## Reference docs in this repo

`docs/DECISIONS.md` (ADRs — the "why"), `docs/USAGE.md`, `docs/CONFIGURATION.md`,
`docs/DATA-FLOWS.md`, `docs/SYNTHESIS.md`, `docs/LOCAL-SETUP.md`, `docs/TRANSCRIBE.md`,
`docs/QUILL.md`, `docs/SYNC.md`, and `docs/LEARNINGS.md` (agent failure modes). Read the ones
relevant to your task.
