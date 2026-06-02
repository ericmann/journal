# Build prompt for Claude Code — `journal`

Paste this into Claude Code at the root of a fresh, empty git repo on the MacBook. The companion design doc is `docs/_internal/TDD.md` — read it first; it is the source of truth for behavior, schema, command surface, and acceptance criteria.

---

## Your task

Build `journal`, a single-binary Go CLI for a local-first developer journal with semantic retrieval and AI-assisted synthesis, exactly as specified in `docs/_internal/TDD.md`. Implement it phase by phase. Do not skip ahead. After each phase: everything builds, `make test` passes, and you stop and summarize what shipped before starting the next phase.

## Ground rules

1. **Read the TDD fully before writing any code.** Sections 5–7 define the repo layout, command surface, storage schema, component breakdown, and per-story acceptance criteria. Treat the acceptance criteria as the definition of done for each story.
2. **TDD-as-in-test-driven, here.** For each story, write the tests first from the acceptance criteria, then implement until green. Target 80%+ coverage on pure logic (chunking, hashing, citation formatting, SQL building, config/note parsing).
3. **No network in unit tests.** The Ollama and Anthropic clients sit behind interfaces. Provide a deterministic `fakeEmbedder` and a fake synth client. Integration tests use a temp-file sqlite-vec DB and the fake embedder.
4. **Markdown is the source of truth; the `.db` is disposable.** Never mutate user note bodies. Capture is append-only. Synthesis writes only to `reflections/` and to clearly-marked rollup blocks in project `_index.md`. The `.db` lives at `.journal/index/journal.db` and MUST be gitignored.
5. **Secrets from env only.** Read `ANTHROPIC_API_KEY` from the environment. Never write it to config, never log it. Non-secret settings live in `.journal/config.yaml`.
6. **Every read command supports `--json`** with the stable schema documented in the TDD (Software Architecture). Errors in `--json` mode emit `{"error": "..."}` and a non-zero exit, so empty results are distinguishable from failure.
7. **`capture` never embeds inline** — it appends and returns (<2s). Embedding happens in `index` / `index --watch`.
8. Use **Cobra** for the command tree and **sqlite-vec** for the store. Prefer `modernc.org/sqlite` (pure Go, no cgo) if sqlite-vec can be loaded that way in this environment; otherwise use `mattn/go-sqlite3` and note the cgo requirement in the README. Verify the sqlite-vec extension actually loads before building on it — if it doesn't load cleanly, stop and report rather than working around it.

## Phase order (build in this sequence)

- **Phase 1 — Story 1:** repo skeleton, `internal/config`, `internal/note`, `journal capture`.
- **Phase 2 — Story 2:** `internal/store` (sqlite-vec schema + migrations + CRUD/KNN).
- **Phase 3 — Stories 3, 4, 5:** chunking + hashing + embed client + fake; `index`/`--rebuild`/incremental; `search` + rerank + `recent`/`decisions`/`threads` + `--json`. **This is the MVP — stop here and let me dogfood before continuing.**
- **Phase 4 — Story 6:** `index --watch` (debounced) + `doctor`.
- **Phase 5 — Story 7:** `internal/synth`, Anthropic client, prompt assembly (golden-file tested), `synth weekly|decisions|stale`, `--dry-run`/`--write`.
- **Phase 6 — Story 8:** `skills/journal/SKILL.md`; validate clone-to-second-workspace isolation.

## Deliverables

- A buildable Go module: `make build` → `./journal`, `make test`, `make lint`.
- A `README.md` covering: install (single binary on PATH), `ollama pull qwen3-embedding:4b` and `qwen3-reranker`, `journal doctor`, the capture conventions (`#tags`, `@decision/@question/@todo`), and how to run the watcher (tmux pane vs launchd/systemd user service — document both, default to whichever I confirm).
- A `.gitignore` that ignores `.journal/index/`.
- `skills/journal/SKILL.md` (Phase 6) teaching: prefer `journal search --json`; how to read the JSON result schema; always cite results back as `path:line_start-line_end`; when to use `search` vs `recent` vs `decisions` vs `threads`; never scrape prose output when `--json` exists.

## Config defaults to scaffold

Use `qwen3-embedding:4b` as the default embed model and `qwen3-reranker` as the reranker in `.journal/config.yaml`, with `OllamaBaseURL` of `http://localhost:11434`, chunk strategy `heading`, and an exclude list covering `reflections/**` and `.journal/**`. Make the embedding dimension configurable (the schema's `float[<dim>]` must match the model's output dimension — read it from config and validate at index time).

## Constraints / watch-outs

- Chunk identity is a stable hash of (path + heading anchor + normalized body). Re-indexing an unchanged repo must make **zero** embed calls and complete in <2s. Editing one block re-embeds only that block. Deleting a block removes its row.
- KNN is brute-force over sqlite-vec at this scale — do not add an ANN index.
- `synth --dry-run` must print the assembled prompt and intended output path and make **no** network call and **no** file write. Verify this in a test.
- Keep the daily-note format exactly as in the TDD (`# YYYY-MM-DD` H1, `## HH:MM #tags` blocks). The chunker and the writer must agree on this format — share one definition.

## When you finish a phase

Stop. Print: what shipped, test results, coverage on the logic packages, and anything in the TDD's Open Questions you hit that needs my decision. Wait for me before the next phase.

Start by reading `docs/_internal/TDD.md`, then confirm your Phase 1 plan back to me in a few bullets before writing code.