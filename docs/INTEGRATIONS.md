# Integrations: Claude Code, Claude desktop, and Ollama

`journal` is built as a **local retrieval substrate** that a reasoning layer
(Claude) drives. The division of labor:

```mermaid
graph TB
    subgraph Reasoning["Reasoning / inference (Claude)"]
        CC[Claude Code CLI/IDE]
        DA[Claude.ai desktop app]
        SY[journal synth — cloud Claude, weekly drafts]
    end
    subgraph Tool["journal — this CLI (local, no network surface)"]
        CMD["capture · index · search<br/>recent · decisions · threads"]
    end
    subgraph Local["Local services + storage"]
        OLL["Ollama — embed + rerank<br/>localhost:11434, no auth"]
        DB[("sqlite-vec .db — derived cache")]
        MD[("markdown repo — git, source of truth")]
    end

    CC -->|"drives; cites path:line"| CMD
    DA -->|"MCP / filesystem"| CMD
    SY --> CMD
    CMD --> OLL
    CMD --> DB
    CMD --> MD
```

Key idea: **retrieval is local** (Ollama embeddings + reranking over a
`sqlite-vec` index), and **inference is Claude** (Code, desktop, or the
scheduled `synth` jobs). Markdown in git is always the source of truth.

---

## 1. Ollama (local embeddings + reranking)

`journal` talks to Ollama over plain HTTP at `ollama_base_url` (default
`http://localhost:11434`); no auth, nothing leaves the machine.

```sh
# install + start Ollama (https://ollama.com), then pull the embedding model:
ollama pull qwen3-embedding:4b     # required (config: embed_model), 2560-dim
# reranking is OPTIONAL — see below; e.g. a small generate model:
# ollama pull qwen3:4b
```

Configure in `.journal/config.yaml`:

```yaml
embed_model: qwen3-embedding:4b
reranker: ""                       # optional; a generate model enables reranking
ollama_base_url: http://localhost:11434
embed_dim: 2560                    # MUST match the model's output dimension
```

Verify the link before relying on it:

```sh
journal doctor          # Ollama reachable, embed model present, embed_dim correct, index health
```

- **Embeddings** are used by `journal index` (documents) and `journal search`
  (queries, with a retrieval-instruction prefix). **Required.**
- **Reranking** is **optional and off by default.** Ollama has no dedicated
  rerank endpoint and no official reranker model, so `journal` does
  LLM-as-reranker: set `reranker` to any small generate model and it scores the
  top vector-KNN candidates via `/api/generate` for a precision lift. Leave it
  empty and search uses vector-distance order — `qwen3-embedding:4b` is strong
  on its own. **Recommended: `qwen3:4b`** (`ollama pull qwen3:4b`); it's small,
  fast, and follows the 0–10 scoring rubric reliably. See
  [DECISIONS.md](DECISIONS.md) for latency notes and model trade-offs.
- `embed_dim` **must** equal your embed model's real output dimension (2560 for
  `qwen3-embedding:4b`). `journal doctor` probes the model and reports the exact
  value; on a mismatch, set it and run `journal index --rebuild`.

> Ollama runs the *retrieval* models locally. It is **not** used for synthesis —
> that's cloud Claude (`journal synth`, Phase 5), which reads
> `ANTHROPIC_API_KEY` from the environment only.

---

## 2. Claude Code (the primary, first-class integration)

This is the integration `journal` is designed around. Claude Code shells out to
the subcommands and consumes their stable `--json`.

**Setup:** the repo ships a skill at `skills/journal/SKILL.md` (authored in
Phase 6). Make it discoverable by Claude Code one of these ways:

- Keep your journal repo open as a workspace — Claude Code picks up
  `skills/journal/SKILL.md` from the repo.
- Or symlink/copy it into your skills library:
  `ln -s "$PWD/skills/journal" ~/.claude/skills/journal`.

Make sure the binary is on `PATH` (`journal --help` works) so the skill can call
it.

**What the skill teaches Claude Code** (full text in `skills/journal/SKILL.md`):

- Prefer `journal search --json` over reading files by hand; never scrape prose
  output when `--json` exists.
- Read the result schema: `{ "results": [ { "path", "line_start", "line_end",
  "heading", "snippet", "score", "tags", "markers" } ] }`.
- Always cite findings back as `path:line_start-line_end` (clickable, verifiable
  against the markdown source of truth).
- Choose the right command: `search` for semantic questions, `recent` for "what
  was I doing lately", `decisions` for `@decision` history, `threads --stale`
  for neglected projects.
- Treat `{ "error": "…" }` + a non-zero exit as failure, distinct from an empty
  `{ "results": [] }`.

**Example a skill-guided agent would run:**

```sh
journal search "litellm fallback when qwen ooms" --k 5 --json
journal decisions --project canton --json
journal threads --stale --days 21 --json
```

Capture works the same way — Claude Code (or you) can record a note without any
embedding round-trip:

```sh
journal capture "decided to pin ncruces v0.21.3 for sqlite-vec #journal @decision"
```

---

## 3. Claude.ai desktop app (local app)

The desktop app does its **inference in the cloud (Claude)**; you bring
`journal` as the **local retrieval tool**. Two supported patterns, depending on
how much you want wired up:

### 3a. Filesystem / project knowledge (simplest, available now)

Because markdown is the source of truth, you can point the desktop app at the
journal repo directory (via its filesystem connector or by adding the folder to
a Project). The app can then read your daily/ and projects/ notes directly. This
gives the model your raw notes but **not** semantic ranking — good for "read my
canton notes", weaker for "find the thing about fallback routing".

Keep the disposable index out of scope: only the markdown matters here
(`.journal/index/` is gitignored and irrelevant to the app).

### 3b. Built-in MCP server: `journal mcp` (recommended for retrieval)

The desktop app speaks **MCP** (Model Context Protocol), and `journal` ships a
**first-party MCP server** over stdio: `journal mcp`. It gives the app the *same
high-precision retrieval* Claude Code gets, exposing these tools (each returns
the same stable JSON as the CLI's `--json`):

| MCP tool    | Arguments                                  | Returns                         |
|-------------|--------------------------------------------|---------------------------------|
| `search`    | `query`, `k?`, `tag[]?`, `project?`, `since?`, `source?` | `{results:[…]}` (path:line cites + full `body`) |
| `show`      | `path` (date, repo-relative path, or citation) | `{path, content}` (full raw markdown) |
| `recent`    | `tag[]?`, `project?`, `since?`             | `{results:[…]}`                 |
| `decisions` | `project?`, `since?`                       | `{results:[…]}` (@decision only)|
| `threads`   | `stale?`, `days?`                          | `{threads:[…]}`                 |
| `meetings`  | `k?`, `since?`                             | `{meetings:[…]}`                |
| `todos`     | `done?`, `project?`, `since?`              | `{results:[…]}` (@todo/@done)   |
| `done`      | `ref` (citation or text fragment)          | `{completed:{…}}`               |
| `capture`   | `text`, `tags[]?`, `project?`, `marker?`   | `{"captured":"<path>"}`         |
| `journal_log_text` | `text`                              | `{path, title, landed}` (shape→assemble→land→index) |
| `journal_log_audio` | `audio_path` (server-local file)   | `{path, title, landed}` (transcribe→shape→assemble→land→index) |
| `ask`       | `query`, `k?`, `tag[]?`, `project?`, `since?` | `{answer, citations}` (grounded Q&A) |
| `stats`     | —                                          | note volume, streaks, open todos, decisions, top tags |
| `today`     | —                                          | today's note path, open todos, today's meetings |
| `synth`     | `kind?`, `days?`, `project?`, `persist?`, `date?` | `{kind, text, output_path, wrote}` (AI synthesis digest) |

`journal_log_text` and `journal_log_audio` run the same shape→assemble→land→index
core as `journal log --text` / `journal log <audio.wav>` — the mic-recording stage
is intentionally **not** exposed over MCP; an MCP server must never seize the
machine's microphone. `landed` is `false` (with empty `path`/`title`) when the
pipeline completed without error but produced no note, e.g. an empty/silent audio
transcript.

### MCP resources

In addition to tools, the server exposes read-only resources that MCP clients can pull
directly as raw Markdown — no tool call needed:

| URI | Contents |
|-------------|---------|
| `journal://today` | Today's daily note (raw Markdown). Returns a stub if no note exists yet. |
| `journal://recent` | The 50 most recent note chunks, newest first, as a Markdown listing. |
| `journal://projects/{slug}/index` | A project's `_index.md` — replace `{slug}` with the project name. Returns `ResourceNotFound` if the project has no index. |

### MCP prompts

Pre-assembled synthesis prompts that the client's model can run without hand-crafting the
request. The server assembles context from the journal index and returns it as a user-role
message — **no cloud calls are made server-side**:

| Prompt name | Arguments | Returns |
|-------------|-----------|---------|
| `weekly-reflection` | — | This week's notes assembled into a weekly-reflection prompt |
| `decisions-review` | `project` (optional) | All `@decision` notes assembled into a review prompt, optionally scoped to a project |
| `project-status` | `project` (required), `since` (optional, default `2w`) | Recent notes for a project assembled into a status prompt |

Register it in the desktop app's MCP config (`claude_desktop_config.json`).
Point `command` at the `journal` binary and use `--repo` (or the working
directory) to bind it to a specific journal repo:

```jsonc
{
  "mcpServers": {
    "journal": {
      "command": "/usr/local/bin/journal",
      "args": ["mcp", "--repo", "/Users/you/journal"]
    }
  }
}
```

That's it — restart the desktop app and the `journal` tools appear. Search still
uses the local Ollama configured in that repo; `capture` appends append-only.
Claude Desktop forwards retrieved note content to Anthropic's cloud models — for
fully local GUI alternatives (LM Studio, Jan, AnythingLLM + Ollama), see
[CLIENTS.md](CLIENTS.md); for the egress picture and the `local_only` switch,
see [DATA-FLOWS.md](DATA-FLOWS.md).
Tool errors come back as `{"error":"…"}` (e.g. if Ollama is down), so the model
can tell failure from an empty result set.

> Verified end-to-end over the real JSON-RPC stdio handshake (initialize →
> tools/list → tools/call). Built on the official `modelcontextprotocol/go-sdk`.

### Inference, locally vs. cloud

- The **desktop app and Claude Code reason with cloud Claude.** `journal`’s
  local Ollama is only for embeddings/reranking, not generation.
- If you want **fully local inference** too, that's outside journal's scope: you
  can point other local clients at the same Ollama instance, but journal's
  synthesis (`synth`) deliberately uses cloud Claude for long-context reasoning
  quality (Phase 5), reading `ANTHROPIC_API_KEY` from the env only.

---

## Workspace separation (personal vs. work)

The whole pattern clones to a second workspace by copying a repo and swapping a
config + env token (validated in Phase 6):

- Separate repo → separate gitignored `.journal/index/` (its own embeddings).
- Separate `ANTHROPIC_API_KEY` in the env that launches the tool (work Enterprise
  token vs. personal). journal never stores the key; which repo you're in and
  which env is loaded determines the workspace.
- Ollama is shared and local — no per-workspace state there.
