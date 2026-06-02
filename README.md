# journal

A local-first developer journal with semantic retrieval and AI-assisted synthesis.

`journal` turns a folder of plain-markdown notes into a searchable, AI-queryable
corpus. Capture is frictionless and append-only; retrieval is a local RAG stack
(Ollama embeddings + reranking, vectors in `sqlite-vec`); weekly synthesis is
backed by cloud Claude. **Markdown in git is the single source of truth** — the
vector index is a disposable, rebuildable cache and is never committed.

**What you get**

- 📝 **Frictionless capture** — `journal capture` appends a timestamped note (inline, in your editor, or from stdin) and auto-commits it.
- 🔎 **Local semantic search** — Ollama embeddings + `sqlite-vec`, optional LLM reranking. No data leaves your machine.
- 🤖 **AI synthesis** — weekly rollups and decision digests via cloud Claude, on demand and in your own voice.
- 💾 **Backup & sync** — opt-in `journal sync` keeps a git remote in step for off-machine backup.
- 🔌 **Integrations** — an MCP server exposes search/recent/decisions to the Claude desktop app and Claude Code.

Everything is a single static binary. **Markdown in git is the source of truth;**
the vector index is a disposable, rebuildable cache that's never committed.

---

## Why

Working notes get scattered across loose text files, per-project `/docs`, and
paper — none of it discoverable or reusable. `journal` keeps one durable
substrate (markdown in git) and adds a thin tool layer that makes capture
zero-friction and retrieval excellent, without a server, daemon, or cloud store.

**Docs:** [Configuration reference](docs/CONFIGURATION.md) ·
[Remote backup](docs/SYNC.md) · [Integrations](docs/INTEGRATIONS.md) ·
[Design decisions](docs/DECISIONS.md) · [Technical design](docs/TDD.md).

---

## Requirements

- **git** — notes live in a git repo; capture/index auto-commit.
- **[Ollama](https://ollama.com) + an embedding model** — required for indexing
  and search (all local). The default model is `qwen3-embedding:4b`:
  `ollama pull qwen3-embedding:4b`. Run `journal doctor` to verify.
- **An Anthropic API key** — *only* for `journal synth` (cloud Claude). Set
  `ANTHROPIC_API_KEY`; never stored in config. Skip it if you don't use synthesis.

Capture, the watcher, and backup work without Ollama; only embedding-backed
features (index, search, synth) need it.

---

## Install

`journal` is a single static binary. (Embeddings and synthesis talk to
Ollama/Anthropic over HTTP — see [Requirements](#requirements).)

**Option A — Homebrew** (macOS):

```sh
brew install displacetech/tap/journal
journal --version
```

The cask clears the macOS quarantine flag on install, so `journal` runs
immediately — no "Allow Anyway" trip through System Settings. (The binary is
ad-hoc signed but not Apple-notarized.)

**Option B — install script** (Linux/macOS, no package manager). Downloads the
latest release, **verifies its SHA-256**, and installs it on your `PATH`:

```sh
curl -fsSL https://raw.githubusercontent.com/ericmann/journal/main/install.sh | sh
```

Prefer to read before running? Download, inspect, then run:

```sh
curl -fsSL https://raw.githubusercontent.com/ericmann/journal/main/install.sh -o install.sh
less install.sh && sh install.sh
```

Pin a version with `JOURNAL_VERSION=v1.4.1`, or change the location with
`PREFIX=$HOME/.local`.

**Option C — Linux packages.** Each release attaches `.deb`, `.rpm`, and `.apk`
artifacts (amd64/arm64) on the [releases page](https://github.com/ericmann/journal/releases):

```sh
sudo dpkg -i journal_*_linux_amd64.deb     # Debian/Ubuntu
sudo rpm -i journal_*_linux_amd64.rpm      # Fedora/RHEL
sudo apk add --allow-untrusted journal_*_linux_amd64.apk
```

**Option D — download an archive directly.** Grab the `tar.gz` for your platform
from the [releases page](https://github.com/ericmann/journal/releases), verify it
against `checksums.txt`, and put the binary on your `PATH`. The checksums file is
signed with [cosign](https://docs.sigstore.dev/) (keyless), so you can verify
provenance:

```sh
cosign verify-blob --signature checksums.txt.sig --certificate checksums.txt.pem \
  --certificate-identity-regexp 'https://github.com/ericmann/journal' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
```

**Option E — build from source** (Go 1.26+, `brew install go`):

```sh
git clone git@github.com:ericmann/journal.git
cd journal
make install                 # builds a version-stamped static binary AND puts it on PATH
journal --version
```

`make install` is build+install in one step, so you never run a stale binary by
forgetting to copy it. It installs to `/usr/local/bin` by default — override with
`make install PREFIX=$HOME/.local`. (`make build` alone just produces `./journal`
in the repo.)

**Updating from source:** `git pull && make install`. The version is stamped
from `git describe`, so pull first or you'll rebuild the older tag.

**For active development** you can instead symlink the live binary once, so a
bare `make build` updates what's on PATH:

```sh
ln -sf "$PWD/journal" /usr/local/bin/journal   # then just `make build` to update
```
(Caveat: `make clean` removes `./journal` and breaks the symlink; use `make
install` for a stable setup.)

Release artifacts (all platforms, packages, checksums, signatures, and the
Homebrew cask) are produced by [GoReleaser](https://goreleaser.com) on each tag
via [`.github/workflows/release.yml`](.github/workflows/release.yml); build them
locally without publishing using `make snapshot`. The binary is fully static
(`CGO_ENABLED=0`) — no runtime deps.

Confirm:

```sh
journal --help
```

### Local models (for indexing & search)

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

Capture appends instantly and, in a git repo, **auto-commits the note** (see
[Auto-commit](#auto-commit-never-lose-a-days-work)). It still does no embedding —
run `journal index` (or the watcher) to make new notes searchable.

### Command surface

```
journal init    [path]                          # bootstrap (or upgrade) a repo
journal capture [text] [--tags a,b] [--project slug] [--marker decision|question|todo]
journal index   [--rebuild] [--since 2w]        # embed changed notes (one-shot)
journal search  <query> [--k 5] [--tag t] [--project slug] [--since 2w] [--json]
journal recent  [--tag t] [--project slug] [--since 1w] [--json]
journal decisions [--project slug] [--since 4w] [--json]
journal threads [--stale] [--days 14] [--json]
```

```
journal index --watch                           # continuous, debounced re-index
journal sync    [--dry-run]                      # back up notes to/from the git remote
journal doctor [--json]                          # health checks
journal synth weekly|decisions|stale [--dry-run] [--write] [--project slug] [--days 14]
journal mcp [--repo path]                        # MCP server (stdio) for Claude Desktop
```

With **no text**, `journal capture` opens your editor to compose the note (like
`git commit`), or reads it from **stdin** when input is piped
(`journal capture < note.md`). The editor follows `$JOURNAL_EDITOR`, the
`editor` config key, `$VISUAL`, `$EDITOR`, then `nano`.

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

### Auto-commit (never lose a day's work)

When the repo is a git repository, `journal capture`, `journal index`, and
`index --watch` all **auto-commit your note changes** (controlled by
`git_autocommit`, default on). **Capture commits the note immediately** — so your
words are committed the moment you capture them, watcher or not. The watcher /
`index` then keep the search index fresh and also commit any direct file edits.
You can't forget. Details:

- It commits **only your markdown** (`git add -A` honors `.gitignore`, so the
  vector index is never committed).
- It's a **no-op unless the repo root is a git top level** — it will never
  commit your notes into a parent repository, and does nothing if git isn't
  installed or the folder isn't a repo.
- Commits are **unsigned by default** (`git_autocommit_sign: false`) so an
  unattended watcher doesn't trigger a signing prompt per note; set it `true`
  for signed note-commits.
- Commit failures are **logged, never fatal** — your markdown is always safe on
  disk. Messages are auto-generated, e.g.
  `📓 scribbled notes — +1 new, ~1 revised, -0 removed · Mon 2026-06-01 12:32`.

Set `git_autocommit: false` to manage commits yourself.

### Remote backup (`journal sync`, opt-in)

Auto-commit keeps your notes safe *locally*; `journal sync` gets them off the
machine to a git remote (and pulls in notes captured elsewhere). It is **off by
default** — it pushes, pulls, and can rewrite local history on a divergence, so
it's strictly opt-in.

Enable it after pointing the repo at a remote:

```sh
git remote add origin git@github.com:you/your-journal.git && git push -u origin HEAD
# then in .journal/config.yaml:  sync_enabled: true
journal sync --dry-run      # preview ahead/behind + planned action
```

Once enabled, each run commits pending notes, then **pushes** when ahead,
**pulls + re-indexes** when behind, and handles a **divergence** per
`sync_conflict` (`manual` — the safe default — aborts and asks you to resolve;
`prefer-upstream`/`prefer-local` auto-resolve toward one side). With no upstream
it's a no-op. `journal init` drops a cron wrapper at **`.journal/sync.sh`** plus
**cron**/**launchd** setup so an hourly backup runs hands-off.

→ Full guide, conflict-mode trade-offs, and the data-loss warning: **[docs/SYNC.md](docs/SYNC.md)**.

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
  - README.md                         # the init-generated usage/cron guide
store_path: .journal/index/journal.db
synth_model: claude-sonnet-4-6        # Anthropic model for `journal synth`
synth_max_tokens: 4096
voice_profile: docs/VOICE_PROFILE.md  # optional; injected into synth prompts
git_autocommit: true                  # auto-commit notes during index/watch (if a git repo)
git_autocommit_sign: false            # sign those commits (off avoids watcher signing prompts)
editor: ""                            # `journal capture` (no text) editor; else $JOURNAL_EDITOR/$VISUAL/$EDITOR, then nano
sync_enabled: false                   # opt-in remote backup (see docs/SYNC.md)
sync_conflict: manual                 # manual | prefer-upstream | prefer-local
```

→ Every key, with defaults and guidance: **[docs/CONFIGURATION.md](docs/CONFIGURATION.md)**.

**Secrets never go in config.** The Anthropic API key for synthesis is read from
the `ANTHROPIC_API_KEY` environment variable only, and is never written to disk
or logged. Keep separate journals (e.g. personal vs. work) by cloning the repo
into different directories and exporting a different key in each.

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

## License

MIT © Displace Technologies, LLC ([displace.tech](https://displace.tech)). See
[`LICENSE`](LICENSE). The architecture and per-story acceptance criteria are
documented in [`docs/TDD.md`](docs/TDD.md); notable build-time decisions in
[`docs/DECISIONS.md`](docs/DECISIONS.md).
