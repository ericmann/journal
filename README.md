<h1 align="center">📓 journal</h1>

<p align="center">
  <strong>A local-first developer journal with semantic search and AI synthesis.</strong><br>
  Plain-markdown notes in git. Frictionless capture, local RAG retrieval, on-demand synthesis — local Ollama, OpenAI-compatible, or cloud Claude.
</p>

<p align="center">
  <a href="https://github.com/ericmann/journal/actions/workflows/ci.yml"><img src="https://github.com/ericmann/journal/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/ericmann/journal/releases/latest"><img src="https://img.shields.io/github/v/release/ericmann/journal?sort=semver" alt="Latest release" /></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white" alt="Go 1.26+" />
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green" alt="MIT License" /></a>
  <img src="https://img.shields.io/badge/brew-ericmann%2Ftap-orange" alt="Homebrew tap" />
  <a href="https://journal.eamann.com"><img src="https://img.shields.io/badge/docs-journal.eamann.com-blue" alt="Documentation" /></a>
</p>

---

## What is journal?

`journal` turns a folder of plain-markdown notes into a searchable, AI-queryable
corpus — no server, daemon, or cloud store. Capture is frictionless and
append-only; retrieval is a fully local RAG stack (Ollama embeddings + optional
reranking, vectors in `sqlite-vec`); synthesis is on-demand — **a fully local
Ollama model** (zero egress, no API key), **any OpenAI-compatible endpoint**
(OpenRouter, Groq, …), or **cloud Claude** (the code default, requires
`ANTHROPIC_API_KEY`) — your choice.

**Markdown in git is the single source of truth** — the vector index is a
disposable, rebuildable cache and is never committed. It all ships as one static
binary.

- 📝 **Frictionless capture** — append a timestamped note inline, in your editor, or from stdin; auto-committed.
- ✅ **Todos that close the loop** — `@todo` in any note becomes a tracked item: `journal todos` lists them, `journal done` checks them off (also via MCP, so Claude can too).
- 🔎 **Local semantic search** — Ollama + `sqlite-vec`, optional LLM reranking; with synthesis configured (cloud or local), a grounded AI answer on top. Notes never leave your machine for retrieval.
- 📺 **A daily home** — `journal today` (day at a glance), `journal tui` (interactive dashboard: notes, todos, search, meetings, stats), `journal stats` (streaks & volume).
- 🤖 **AI synthesis** — daily/weekly rollups and decision digests in your own voice. Pick your `synth_provider`: a **fully local** Ollama model (zero egress, no API key), any **OpenAI-compatible** endpoint (OpenRouter, Groq, …), or cloud Claude (the code default). See [AI Synthesis](https://journal.eamann.com/synthesis.html) · [Going Fully Local](https://journal.eamann.com/local-only.html).
- 🎙️ **Meeting transcripts** — pull [Quill](https://www.quillmeetings.com) meetings into the same local index (`journal quill-sync`); or ingest any recording via `journal transcribe` (WhisperX → summarized, indexed transcript — see [Meetings & Transcripts](https://journal.eamann.com/meetings.html)). Search, list, and digest them all. *(v2.0; Quill is macOS/Windows.)*
- 🗣️ **Voice notes** — `journal log --text "..."` runs shape→assemble→land→index: the LLM cleans the text, extracts markers (`@todo`/`@decision`/`@question`), and writes a structured note to `logs/`; `--source voice` scopes search to voice chunks.
- 💾 **Backup & sync** — opt-in `journal sync` keeps a git remote in step, off-machine.
- 🔌 **Integrations** — an MCP server (`journal mcp`) exposes 13 tools (`search`, `capture`, `todos`, `synth`, and more), read-only resources (`journal://today`, `journal://recent`, …), and pre-built prompts to Claude Desktop and Claude Code over MCP.

---

## Requirements

- **git** — notes live in a git repo; capture/index auto-commit.
- **[Ollama](https://ollama.com) + an embedding model** — for indexing and search
  (all local). Default: `ollama pull qwen3-embedding:4b`. Verify with `journal doctor`.
  Optional reranker: `ollama pull qwen3:4b` + `reranker: qwen3:4b` in config — precision boost for search, off by default.
- **A synthesis provider** *(only for `journal synth` + grounded search answers)* —
  **cloud Claude** (default; set `ANTHROPIC_API_KEY`), an **OpenAI-compatible**
  endpoint (`synth_provider: openai` + `OPENAI_API_KEY` — OpenRouter, Groq, …), or a
  **fully local** Ollama model (`synth_provider: ollama` — no key, nothing leaves
  your machine). Keys are read from the env, never stored in config. Skip all of
  them if you don't synthesize.
- **[Quill](https://www.quillmeetings.com)** *(optional, macOS/Windows)* — only for
  `journal quill-sync` to pull meeting transcripts. Everything else works without it.

Capture, the watcher, and backup work without Ollama; only the embedding-backed
features (index, search, synth) need it.

---

## Install

**Homebrew** (macOS) — the cask clears the quarantine flag, so it runs without an
"Allow Anyway" prompt:

```sh
brew install ericmann/tap/journal
```

**Install script** (Linux/macOS) — downloads the latest release, **verifies its
SHA-256**, installs on your `PATH`:

```sh
curl -fsSL https://raw.githubusercontent.com/ericmann/journal/main/install.sh | sh
```

<details>
<summary>More options — Linux packages, signed archives, build from source</summary>

**Linux packages** — each release attaches `.deb`/`.rpm`/`.apk` (amd64/arm64),
with shell completions bundled in:

```sh
sudo dpkg -i journal_*_linux_amd64.deb     # Debian/Ubuntu
sudo rpm  -i journal_*_linux_amd64.rpm     # Fedora/RHEL
sudo apk add --allow-untrusted journal_*_linux_amd64.apk
```

**Direct archive (any platform)** — grab the `tar.gz` from the
[releases page](https://github.com/ericmann/journal/releases) and verify it
against `checksums.txt`, which is [cosign](https://docs.sigstore.dev/)-signed
(keyless):

```sh
cosign verify-blob --signature checksums.txt.sig --certificate checksums.txt.pem \
  --certificate-identity-regexp 'https://github.com/ericmann/journal' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
```

**From source** (Go 1.26+):

```sh
git clone https://github.com/ericmann/journal.git && cd journal
make install     # build + put a version-stamped static binary on PATH (PREFIX=/usr/local)
```

The install script takes `JOURNAL_VERSION=` to pin and `PREFIX=` to relocate.
Release artifacts are produced by [GoReleaser](https://goreleaser.com) on each
tag; build them locally with `make snapshot`.
</details>

Shell completions: package installs wire them up automatically; otherwise
`journal completion bash|zsh|fish` — see [Capturing Notes](https://journal.eamann.com/capturing-notes.html).

---

## Quick start

```sh
journal init                                          # scaffold a repo in the cwd
ollama pull qwen3-embedding:4b                         # one-time: the embedding model
journal capture "fallback isn't triggering on OOM #cabot #litellm"
journal capture "declare the dev-fund payment as income" --project canton --marker decision
journal index                                          # embed new notes
journal search "how did we handle the OOM fallback"    # ask
```

`journal init` scaffolds:

```
.journal/config.yaml   # committed: models, excludes (no secrets)
.journal/index/        # gitignored: the disposable sqlite-vec database
daily/                 # the firehose: daily/YYYY/MM/YYYY-MM-DD.md
projects/              # long-lived threads: projects/<slug>/
reflections/           # synthesis output (drafts)
docs/                  # your voice profile + the cron/launchd guide
```

Never commit `.journal/index/` (it's a rebuildable binary blob; `init` gitignores
it). Run `journal doctor` anytime to check Ollama, models, and the index.

---

## Commands

| Command | What it does |
| --- | --- |
| `journal init [path]` | Scaffold (or upgrade) a journal repo |
| `journal capture [text]` | Append a timestamped note (inline / editor / stdin) |
| `journal index [--watch]` | Embed changed notes; `--watch` runs continuously |
| `journal log --text "..."` | Capture a voice note: shape (LLM) → assemble → land to `logs/` → index as `source=voice` |
| `journal search <query>` | Semantic search with citations (+ a grounded AI answer when synthesis is configured); `--source notes\|transcript\|voice\|all` |
| `journal recent` · `decisions` · `threads` · `meetings` | Metadata views (newest-first, `@decision`, project activity, transcripts) |
| `journal todos` · `journal done <ref>` | List open `@todo` items; check one off (rewrites it to `@done <date>`) |
| `journal today` · `show` · `edit` | Day at a glance; render any note; open a daily file in your editor |
| `journal tui` | Interactive dashboard: today, todos, semantic search, recent, meetings, stats |
| `journal stats` | Capture volume, streaks, marker counts, top tags |
| `journal tags` | List `#tags` with usage counts; `tags rename <old> <new>` renames across all notes |
| `journal quill-sync` | Pull Quill meeting transcripts into `transcripts/` ([Meetings & Transcripts](https://journal.eamann.com/meetings.html)) |
| `journal transcribe <whisperx.json>` | Ingest a non-Quill recording: render + summarize + index ([Meetings & Transcripts](https://journal.eamann.com/meetings.html)) |
| `journal synth weekly\|daily\|meetings\|decisions\|stale` | AI synthesis — cloud Claude, OpenAI-compatible (OpenRouter/…), or local Ollama (`synth_provider`) |
| `journal sync` | Back up to / pull from a git remote (opt-in) |
| `journal doctor` | Health-check Ollama, models, the index |
| `journal mcp` | MCP server for Claude Desktop / Claude Code |

Every command takes a global **`--journal-dir`** (or `JOURNAL_DIR` env) to operate
on a journal from any directory — handy for an alias like
`alias jc='journal capture --journal-dir ~/Projects/devnotes'`.

Full flags, the note format, and search internals are in the [**user documentation**](https://journal.eamann.com).

---

## Documentation

📖 **[Full user documentation → journal.eamann.com](https://journal.eamann.com)**

**Getting Started**

| Guide | Contents |
| --- | --- |
| [What is journal?](https://journal.eamann.com/what-is-journal.html) | Local-first design, synthesis options, the one-binary model |
| [Installation](https://journal.eamann.com/installation.html) | Homebrew, install script, Linux packages, build from source |
| [Your First Day](https://journal.eamann.com/first-day.html) | Init, capture, index, search — the 5-minute intro |

**Using Journal**

| Guide | Contents |
| --- | --- |
| [Capturing Notes](https://journal.eamann.com/capturing-notes.html) | Capture conventions, the watcher, auto-commit, note format |
| [Tracking Todos](https://journal.eamann.com/todos.html) | `@todo` / `@done`, listing, checking off |
| [Searching Your Journal](https://journal.eamann.com/searching.html) | Semantic search, filters, grounded AI answers |
| [AI Synthesis](https://journal.eamann.com/synthesis.html) | `journal synth`, choosing a provider, writing in your voice |
| [Meetings & Transcripts](https://journal.eamann.com/meetings.html) | Quill sync, `journal transcribe`, searching meetings |

**Configuration**

| Guide | Contents |
| --- | --- |
| [Configuration Reference](https://journal.eamann.com/configuration.html) | Every `config.yaml` key, defaults, and secrets |
| [Going Fully Local](https://journal.eamann.com/local-only.html) | Zero-egress stack: `local_only`, Ollama synthesis, local MCP clients |
| [Backup & Sync](https://journal.eamann.com/sync.html) | `journal sync`: enabling, conflict modes, cron/launchd/systemd |

**Integrations**

| Guide | Contents |
| --- | --- |
| [Claude Code](https://journal.eamann.com/integrations/claude-code.html) | MCP server setup for Claude Code |
| [Claude Desktop](https://journal.eamann.com/integrations/claude-desktop.html) | MCP server setup for Claude Desktop |
| [Local MCP Clients](https://journal.eamann.com/integrations/local-clients.html) | Jan, LM Studio, and other local chat clients |

**Contributor / internal reference** (repo-relative, not end-user docs):
[`docs/DECISIONS.md`](docs/DECISIONS.md) — ADRs and design rationale ·
[`docs/LEARNINGS.md`](docs/LEARNINGS.md) — agent failure modes and lessons learned

---

## Development

```sh
make build    # go build -o journal .
make test     # unit + integration tests (no network; fakes for Ollama/Anthropic)
make lint     # gofmt check + go vet (+ golangci-lint if installed)
make snapshot # build the full release artifact set locally (no publish)
```

CI runs build, `gofmt`, `go vet`, tests (serial, `-p 1` — see [`docs/DECISIONS.md`](docs/DECISIONS.md)
for why), race-enabled tests on `internal/embed` only (see [`.github/workflows/ci.yml`](.github/workflows/ci.yml)),
and `golangci-lint` on every push and PR. Tests never touch the network — the Ollama and
Anthropic clients sit behind interfaces with deterministic fakes, and integration tests use
a temp-file `sqlite-vec` database.

---

## License

Changes are tracked in [`CHANGELOG.md`](CHANGELOG.md).

MIT © Displace Technologies, LLC ([displace.tech](https://displace.tech)). See
[`LICENSE`](LICENSE). The original technical design and build prompt live in
[`docs/_internal/`](docs/_internal).
