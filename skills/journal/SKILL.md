---
name: journal
description: >-
  Search and query the user's local developer journal (a git repo of markdown
  notes indexed for semantic retrieval). Use when the user asks what they did,
  decided, or noted in the past; to find prior context on a topic, project, or
  bug; to surface neglected project threads; or to capture a new note. Shells
  out to the `journal` CLI and reads its stable `--json`.
---

# journal

`journal` is a local CLI over the user's markdown notes with semantic search
(Ollama embeddings + reranking, `sqlite-vec` index) and structured queries. The
markdown in git is the source of truth; the index is a disposable local cache.

**Run `journal` from inside the journal repo** (it locates the repo root by
walking up for a `.journal/` directory). If a command reports "not inside a
journal repo", `cd` into the repo first.

## Core rules

1. **Always prefer `--json`.** Every read command supports it and emits a stable
   schema. **Never scrape the human-readable prose output** — parse JSON.
2. **Always cite results back to the user as `path:line_start-line_end`** (e.g.
   `daily/2026/06/2026-06-01.md:3-5`). These are verifiable against the markdown
   and clickable. Build the citation from the JSON fields, don't invent it.
3. **Distinguish error from empty.** On failure a command prints
   `{"error": "..."}` and exits non-zero. An empty result set is
   `{"results": []}` with exit 0. Treat them differently — if you get an error
   (e.g. Ollama down), say so; don't report "nothing found".
4. **Don't run `index` or `synth --write` unless asked.** Searching is safe and
   read-only. Indexing embeds via Ollama; `synth --write` calls the cloud API
   and writes files.

## Which command to use

- **`journal search "<query>" --json`** — semantic question: "find where I
  worked on X", "what did I note about the litellm fallback", "anything on
  Canton taxes". This is the default for "find / recall / what did I say about".
- **`journal recent --json`** — time-ordered recap: "what have I been working on
  lately". Newest first. Filter with `--tag`, `--project`, `--since`.
- **`journal decisions --json`** — the decision log (`@decision` notes only):
  "what did I decide about X". Scope with `--project`, `--since`.
- **`journal threads --json`** — project-level activity. Add `--stale` (and
  optionally `--days N`) for "what have I been neglecting / what's gone quiet".
- **`journal capture "<text>"`** — record a new note (append-only, instant). Use
  `--project <slug>`, `--tags a,b`, `--marker decision|question|todo`. Inline
  `#tags` and `@decision/@question/@todo` in the text are detected too.

Prefer `search` for meaning, `recent`/`decisions`/`threads` for structured
facets. Combine: `search "..." --project canton --since 4w --json`.

## Reading the JSON

`search` / `recent` / `decisions` return:

```json
{
  "results": [
    {
      "path": "daily/2026/06/2026-06-01.md",
      "line_start": 3,
      "line_end": 5,
      "heading": "09:14 #cabot #litellm",
      "snippet": "Routing fallback isn't triggering when Qwen OOMs…",
      "score": 0.87,
      "tags": ["cabot", "litellm"],
      "markers": []
    }
  ]
}
```

- Results are ordered best-first (`search` by rerank `score`; `recent`/
  `decisions` newest-first). Lead with the top hits.
- Cite each as `path:line_start-line_end`. Use `snippet` for context; read the
  file at that range if you need the full note.
- `score` is a relevance signal for `search` (0–1); it's `0` for `recent`/
  `decisions`.

`threads` returns a different shape:

```json
{
  "threads": [
    { "project": "canton", "last_activity": "2026-06-01T09:14:00Z",
      "chunks": 12, "open_questions": 2, "stale": false, "days_since": 3 }
  ]
}
```

Error shape (any read command):

```json
{ "error": "ollama unreachable at http://localhost:11434: ..." }
```

## Examples

```sh
# Recall context on a topic, best matches first:
journal search "litellm fallback when qwen ooms" --k 5 --json

# Decisions for one project in the last month:
journal decisions --project canton --since 4w --json

# What have I let go stale for 3+ weeks?
journal threads --stale --days 21 --json

# Capture a decision (you may also write @decision inline):
journal capture "pin ncruces v0.21.3 for sqlite-vec #journal" --marker decision
```

## Health

If searches error, run **`journal doctor --json`** — it checks Ollama
reachability, model presence, and index health, and returns
`{"ok": false, "checks": [...]}` with actionable detail. A missing model means
the user needs `ollama pull <model>`; an empty index means run `journal index`.

## Don'ts

- Don't parse the text output — use `--json`.
- Don't fabricate `path:line` citations — only cite ranges present in results.
- Don't run `index`, `index --watch`, or `synth --write` unprompted.
- Don't read the `.journal/index/` database — it's a disposable binary cache;
  the markdown files are the source of truth.
