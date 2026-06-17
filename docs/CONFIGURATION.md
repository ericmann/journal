# Configuration

All non-secret settings live in `.journal/config.yaml`, which `journal init`
creates with the defaults below. The file is committed with your notes. The one
secret — the Anthropic API key for synthesis — is **never** stored here; it is
read from the `ANTHROPIC_API_KEY` environment variable only.

Run `journal doctor` after changing models or dimensions; it validates the
config against your live Ollama and tells you exactly what to fix.

## Full default `config.yaml`

```yaml
# --- Embedding & retrieval ---
embed_provider: ollama                 # ollama (local, default) | openai (OpenAI-compatible)
embed_model: qwen3-embedding:4b        # Ollama embedding model (embed_provider: ollama)
embed_openai_base_url: https://api.openai.com/v1  # embed_provider: openai (override for Together/etc.)
embed_openai_model: ""                 # embed_provider: openai (e.g. text-embedding-3-small → embed_dim 1536)
embed_dim: 2560                        # vector dimension; MUST match the active embed model
reranker: ""                           # optional Ollama generate model for LLM-as-reranker (ollama embed only)
ollama_base_url: http://localhost:11434
chunk_strategy: heading                # only "heading" is supported
retrieval_instruction: "Represent this query for retrieving relevant developer journal notes:"
store_path: .journal/index/journal.db  # disposable, gitignored vector index
excludes:                              # repo-relative globs skipped by the indexer
  - reflections/**
  - .journal/**
  - docs/**
  - README.md

# --- Capture ---
editor: ""                             # editor for `journal capture` with no text

# --- Synthesis (cloud Claude, OpenAI-compatible, or local Ollama) ---
synth_provider: anthropic              # anthropic (cloud) | ollama (local) | openai (OpenAI-compatible)
synth_model: claude-sonnet-4-6         # model when provider is anthropic
synth_ollama_model: gemma4:12b         # model when provider is ollama
synth_openai_base_url: https://api.openai.com/v1  # provider: openai (e.g. https://openrouter.ai/api/v1)
synth_openai_model: ""                 # provider: openai (e.g. google/gemma-3-27b-it:free on OpenRouter)
synth_num_ctx: 32768                   # Ollama context window (its 4096 default truncates silently)
synth_max_tokens: 4096
voice_profile: docs/VOICE_PROFILE.md   # optional style reference for synth

# --- Egress kill-switch (see docs/DATA-FLOWS.md) ---
local_only: false                      # true = block cloud-AI egress (refuse cloud synth, block mcp, loopback Ollama)
local_only_mcp: block                  # block | allow — allow = "my MCP client is local" (docs/CLIENTS.md)

# --- Git integration ---
git_autocommit: true                   # auto-commit notes during capture/index/watch
git_autocommit_sign: false             # sign those commits

# --- Remote backup (opt-in; see docs/SYNC.md) ---
sync_enabled: false                    # `journal sync` does nothing until true
sync_conflict: manual                  # manual | prefer-upstream | prefer-local

# --- Meeting transcripts / Quill (see docs/QUILL.md) ---
transcripts:
  enabled: true                        # gate the whole transcript feature
  path: transcripts                    # gitignored landing zone (repo-relative)
  format: auto                         # auto | markdown | txt
  auto_index: true                     # embed new transcripts as the watcher sees them
  tag: meeting                         # tag applied to every transcript chunk
  log_captures: false                  # daily breadcrumb when a transcript is indexed
quill:
  enabled: true                        # gate `journal quill-sync`
  db_path: ~/Library/Application Support/Quill/quill.db  # macOS; Windows: ~/AppData/Roaming/Quill/quill.db
  accept_qm_imports: true              # render dropped-in .qm files

schema_version: "2.0"                  # config schema; `journal init` upgrades older repos
```

## Key reference

| Key | Default | What it does |
| --- | --- | --- |
| `embed_provider` | `ollama` | Embedding backend: `ollama` (local) or `openai` (any OpenAI-compatible `/embeddings` endpoint; needs `OPENAI_API_KEY`). Remote embeddings can't use the Ollama reranker and require `embed_dim` to match the model. |
| `embed_model` | `qwen3-embedding:4b` | Ollama model used to embed notes and queries when `embed_provider: ollama`. **Required** for that provider — pull it with `ollama pull`. |
| `embed_openai_base_url` | `https://api.openai.com/v1` | OpenAI-compatible embeddings base when `embed_provider: openai`. |
| `embed_openai_model` | `""` | Embedding model id when `embed_provider: openai` (e.g. `text-embedding-3-small` → set `embed_dim: 1536`). |
| `embed_dim` | `2560` | Embedding vector dimension. **Must match the active embed model's output** (the vec table is declared `float[embed_dim]`). With `ollama`, `journal doctor` probes and prints the right value; after changing it, run `journal index --rebuild`. |
| `reranker` | `""` (off) | Optional generate model (e.g. `qwen3:4b`) for LLM-as-reranker precision. Empty = vector-KNN order, which is strong on its own. |
| `ollama_base_url` | `http://localhost:11434` | Where Ollama is reached. Change it if Ollama runs on another host/port. |
| `chunk_strategy` | `heading` | How notes split into chunks. Only `heading` is supported. |
| `retrieval_instruction` | _(see above)_ | Prefix added to queries when embedding for search. |
| `store_path` | `.journal/index/journal.db` | Path to the sqlite-vec index. Disposable and gitignored — rebuild any time with `journal index --rebuild`. |
| `excludes` | `reflections/**`, `.journal/**`, `docs/**`, `README.md` | Repo-relative globs the indexer skips. `reflections/` holds synthesis output; `docs/` holds meta like the voice profile; `README.md` is the generated guide. |
| `editor` | `""` | Command for `journal capture` with no text. Run as a shell string (so `code --wait` works). Empty falls back to `$JOURNAL_EDITOR`, `$VISUAL`, `$EDITOR`, then `nano`. |
| `synth_provider` | `anthropic` | Who runs `journal synth` and search answers: `anthropic` (cloud Claude, needs `ANTHROPIC_API_KEY`), `ollama` (fully local), or `openai` (any OpenAI-compatible endpoint — OpenRouter/Groq/Together/…, needs `OPENAI_API_KEY`). See [SYNTHESIS.md](SYNTHESIS.md). |
| `synth_model` | `claude-sonnet-4-6` | Anthropic model, used when `synth_provider: anthropic`. |
| `synth_ollama_model` | `gemma4:12b` | Ollama model, used when `synth_provider: ollama`. Pull it with `ollama pull`. 64GB machines can step up to `gemma4:26b`. |
| `synth_openai_base_url` | `https://api.openai.com/v1` | OpenAI-compatible Chat Completions base when `synth_provider: openai` (e.g. `https://openrouter.ai/api/v1`). |
| `synth_openai_model` | `""` | Model id when `synth_provider: openai` (e.g. `google/gemma-3-27b-it:free` on OpenRouter, `gpt-4o-mini` on OpenAI). |
| `synth_num_ctx` | `32768` | Context window requested per Ollama synthesis call. Always sent explicitly — Ollama's server default is 4096 and it **truncates silently**. |
| `synth_max_tokens` | `4096` | Cap on synthesis response length. |
| `local_only` | `false` | **Cloud-AI egress kill-switch.** When `true`: cloud synthesis is refused (`synth_provider` must be `ollama`), `journal mcp` is blocked (see `local_only_mcp`), and `ollama_base_url` must be loopback. Does **not** touch `journal sync` — that backs up to your own remote and stays governed by `sync_enabled` (keep it `false` for a fully sealed posture). See [DATA-FLOWS.md](DATA-FLOWS.md). |
| `local_only_mcp` | `block` | Under `local_only`, whether `journal mcp` runs: `block` (default) or `allow`. `allow` is an **attestation** — the server can't verify its client, so it means "my MCP client runs a local model" (e.g. LM Studio/Jan, see [CLIENTS.md](CLIENTS.md)). Ignored when `local_only` is `false`. |
| `voice_profile` | `docs/VOICE_PROFILE.md` | Optional markdown style reference injected into synth prompts. |
| `git_autocommit` | `true` | Auto-commit note changes during `capture`/`index`/`index --watch` when the repo root is a git work tree. No-op outside git. |
| `git_autocommit_sign` | `false` | Sign auto-commits. Off avoids signing prompts in an unattended watcher. |
| `sync_enabled` | `false` | Gates `journal sync`. **Opt-in** — see [SYNC.md](SYNC.md). |
| `sync_conflict` | `manual` | How `sync` resolves a divergence: `manual` aborts and asks you to resolve (never discards work), `prefer-upstream` takes the remote on conflict, `prefer-local` keeps local. See [SYNC.md](SYNC.md). |
| `transcripts.enabled` | `true` | Gates the meeting-transcript feature (a no-op until a transcript exists). |
| `transcripts.path` | `transcripts` | Repo-relative, gitignored landing zone for rendered transcripts. |
| `transcripts.format` | `auto` | Transcript parse hint: `auto`/`markdown`/`txt`. |
| `transcripts.auto_index` | `true` | Embed new/modified transcripts as the watcher detects them. |
| `transcripts.tag` | `meeting` | Tag applied to every transcript chunk (find them via `--tag`/`--source`). |
| `transcripts.log_captures` | `false` | Append a daily breadcrumb note when a transcript is indexed. |
| `quill.enabled` | `true` | Gates `journal quill-sync`. |
| `quill.db_path` | OS default | Quill SQLite DB (read-only). `~` expands. macOS/Windows only — see [QUILL.md](QUILL.md). |
| `quill.accept_qm_imports` | `true` | Render manually-dropped `.qm` files in the landing zone. |
| `schema_version` | `2.0` | Config schema version; `journal init` upgrades older repos in place. |

## Secrets

API keys are read from the environment only — never written to config or logged:

- **`ANTHROPIC_API_KEY`** — for `synth_provider: anthropic` (`journal synth
  --write` and grounded search answers).
- **`OPENAI_API_KEY`** — for the `openai` provider on either side
  (`synth_provider: openai` and/or `embed_provider: openai`). It holds whatever
  the endpoint expects — e.g. an **OpenRouter** key when `*_base_url` points at
  OpenRouter. The same variable serves both openai-provider paths, so mixing two
  different OpenAI-compatible services (one for synth, one for embeddings) isn't
  supported yet.

To keep personal and work journals separate, clone the repo into different
directories and export different keys in each.
