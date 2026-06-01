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
`gofmt` check, `go vet`, `go build`, race-enabled tests with coverage, and
`golangci-lint` (pinned to v1.64.8 to match the locally verified version).

### sqlite-vec loading (to be verified in Phase 2)
Ground rule: verify the `sqlite-vec` extension actually loads — preferring pure
-Go `modernc.org/sqlite`, else cgo `mattn/go-sqlite3` — before building the
store on it. If it doesn't load cleanly, stop and report. _(Pending Phase 2.)_
