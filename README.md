<!-- Drop a logo at docs/journal-logo.png and swap the <h1> below for:
<p align="center"><img src="docs/journal-logo.png" alt="journal" width="180" /></p> -->

<h1 align="center">📓 journal</h1>

<p align="center">
  <strong>A local-first developer journal with semantic search and AI synthesis.</strong><br>
  Plain-markdown notes in git. Frictionless capture, local RAG retrieval, synthesis via cloud Claude <em>or</em> a fully local model.
</p>

<p align="center">
  <a href="https://github.com/ericmann/journal/actions/workflows/ci.yml"><img src="https://github.com/ericmann/journal/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/ericmann/journal/releases/latest"><img src="https://img.shields.io/github/v/release/ericmann/journal?sort=semver" alt="Latest release" /></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white" alt="Go 1.26+" />
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green" alt="MIT License" /></a>
  <img src="https://img.shields.io/badge/brew-ericmann%2Ftap-orange" alt="Homebrew tap" />
</p>

---

## What is journal?

`journal` turns a folder of plain-markdown notes into a searchable, AI-queryable
corpus — no server, daemon, or cloud store. Capture is frictionless and
append-only; retrieval is a fully local RAG stack (Ollama embeddings + optional
reranking, vectors in `sqlite-vec`); synthesis is on-demand — **cloud Claude
(Sonnet by default), any OpenAI-compatible endpoint (OpenRouter, Groq, …), or a
fully local Ollama model**, your choice.

**Markdown in git is the single source of truth** — the vector index is a
disposable, rebuildable cache and is never committed. It all ships as one static
binary.

- 📝 **Frictionless capture** — append a timestamped note inline, in your editor, or from stdin; auto-committed.
- ✅ **Todos that close the loop** — `@todo` in any note becomes a tracked item: `journal todos` lists them, `journal done` checks them off (also via MCP, so Claude can too).
- 🔎 **Local semantic search** — Ollama + `sqlite-vec`, optional LLM reranking; with synthesis configured (cloud or local), a grounded AI answer on top. Notes never leave your machine for retrieval.
- 📺 **A daily home** — `journal today` (day at a glance), `journal tui` (interactive dashboard: notes, todos, search, meetings, stats), `journal stats` (streaks & volume).
- 🤖 **AI synthesis** — daily/weekly rollups and decision digests in your own voice. Pick your `synth_provider`: cloud Claude (default), any **OpenAI-compatible** endpoint (OpenRouter's free Gemma, Groq, …), or a **fully local** Ollama model (zero egress). See [SYNTHESIS.md](docs/SYNTHESIS.md) · [LOCAL-SETUP.md](docs/LOCAL-SETUP.md).
- 🎙️ **Meeting transcripts** — pull [Quill](https://www.quillmeetings.com) meetings into the same local index (`journal quill-sync`); or ingest any recording via `journal transcribe` (WhisperX → summarized, indexed transcript — see [TRANSCRIBE.md](docs/TRANSCRIBE.md)). Search, list, and digest them all. *(v2.0; Quill is macOS/Windows.)*
- 💾 **Backup & sync** — opt-in `journal sync` keeps a git remote in step, off-machine.
- 🔌 **Integrations** — an MCP server exposes search/recent/decisions/meetings to Claude Desktop and Claude Code.

---

## Requirements

- **git** — notes live in a git repo; capture/index auto-commit.
- **[Ollama](https://ollama.com) + an embedding model** — for indexing and search
  (all local). Default: `ollama pull qwen3-embedding:4b`. Verify with `journal doctor`.
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
`journal completion bash|zsh|fish` — see [Usage](docs/USAGE.md).

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
| `journal search <query>` | Semantic search with citations (+ a grounded AI answer when synthesis is configured); `--source notes\|transcript\|all` |
| `journal recent` · `decisions` · `threads` · `meetings` | Metadata views (newest-first, `@decision`, project activity, transcripts) |
| `journal todos` · `journal done <ref>` | List open `@todo` items; check one off (rewrites it to `@done <date>`) |
| `journal today` · `show` · `edit` | Day at a glance; render any note; open a daily file in your editor |
| `journal tui` | Interactive dashboard: today, todos, semantic search, recent, meetings, stats |
| `journal stats` | Capture volume, streaks, marker counts, top tags |
| `journal quill-sync` | Pull Quill meeting transcripts into `transcripts/` ([Quill](docs/QUILL.md)) |
| `journal transcribe <whisperx.json>` | Ingest a non-Quill recording: render + summarize + index ([Transcribe](docs/TRANSCRIBE.md)) |
| `journal synth weekly\|daily\|meetings\|decisions\|stale` | AI synthesis — cloud Claude, OpenAI-compatible (OpenRouter/…), or local Ollama (`synth_provider`) |
| `journal sync` | Back up to / pull from a git remote (opt-in) |
| `journal doctor` | Health-check Ollama, models, the index |
| `journal mcp` | MCP server for Claude Desktop / Claude Code |

Every command takes a global **`--journal-dir`** (or `JOURNAL_DIR` env) to operate
on a journal from any directory — handy for an alias like
`alias jc='journal capture --journal-dir ~/Projects/devnotes'`.

Full flags, the note format, and search internals are in [**Usage**](docs/USAGE.md).

---

## Documentation

| Guide | Contents |
| --- | --- |
| [Usage](docs/USAGE.md) | Capture conventions, command surface, retrieval, the watcher, auto-commit |
| [Meeting transcripts (Quill)](docs/QUILL.md) | Pulling Quill meetings into the index — the v2.0 feature (macOS/Windows) |
| [Transcribing recordings](docs/TRANSCRIBE.md) | Non-Quill audio/video → WhisperX → summarized, indexed transcript (`journal transcribe`) |
| [Configuration](docs/CONFIGURATION.md) | Every `config.yaml` key, defaults, and secrets |
| [Synthesis](docs/SYNTHESIS.md) | `journal synth` and writing in your voice |
| [Remote backup](docs/SYNC.md) | `journal sync`: enabling, conflict modes, cron/launchd/systemd |
| [Workspaces](docs/WORKSPACES.md) | Multiple isolated journals (e.g. personal vs. work) |
| [Integrations](docs/INTEGRATIONS.md) | Claude Desktop, Claude Code, Ollama wiring |
| [Fully local setup](docs/LOCAL-SETUP.md) | Zero-egress stack: Ollama models, `local_only`, a local MCP chat client |
| [Design decisions](docs/DECISIONS.md) | Why the tool is built the way it is |

---

## Development

```sh
make build    # go build -o journal .
make test     # unit + integration tests (no network; fakes for Ollama/Anthropic)
make lint     # gofmt check + go vet (+ golangci-lint if installed)
make snapshot # build the full release artifact set locally (no publish)
```

CI runs build, `gofmt`, `go vet`, race-enabled tests, and `golangci-lint` on every
push and PR. Tests never touch the network — the Ollama and Anthropic clients sit
behind interfaces with deterministic fakes, and integration tests use a temp-file
`sqlite-vec` database.

---

## License

Changes are tracked in [`CHANGELOG.md`](CHANGELOG.md).

MIT © Displace Technologies, LLC ([displace.tech](https://displace.tech)). See
[`LICENSE`](LICENSE). The original technical design and build prompt live in
[`docs/_internal/`](docs/_internal).
