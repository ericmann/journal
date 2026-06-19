# Configuration Reference

Your journal's settings live in `.journal/config.yaml` in your journal repository. This file is committed to git — it travels with your notes. The only thing that's never in this file is API keys; those come from environment variables.

Run `journal doctor` after changing any model setting to verify your configuration.

## Full default config

```yaml
# --- Embedding & retrieval ---
embed_provider: ollama
embed_model: qwen3-embedding:4b
embed_openai_base_url: https://api.openai.com/v1
embed_openai_model: ""
embed_dim: 2560
reranker: ""
ollama_base_url: http://localhost:11434
chunk_strategy: heading
retrieval_instruction: "Represent this query for retrieving relevant developer journal notes:"
store_path: .journal/index/journal.db
excludes:
  - reflections/**
  - .journal/**
  - docs/**
  - README.md

# --- Capture ---
editor: ""

# --- Synthesis ---
synth_provider: anthropic
synth_model: claude-sonnet-4-6
synth_ollama_model: gemma4:12b
synth_openai_base_url: https://api.openai.com/v1
synth_openai_model: ""
synth_num_ctx: 32768
synth_max_tokens: 4096
voice_profile: docs/VOICE_PROFILE.md

# --- Egress kill-switch ---
local_only: false
local_only_mcp: block

# --- Git integration ---
git_autocommit: true
git_autocommit_sign: false

# --- Remote backup ---
sync_enabled: false
sync_conflict: manual

# --- Meeting transcripts / Quill ---
transcripts:
  enabled: true
  path: transcripts
  format: auto
  auto_index: true
  tag: meeting
  log_captures: false
quill:
  enabled: true
  db_path: ~/Library/Application Support/Quill/quill.db
  accept_qm_imports: true

schema_version: "2.0"
```

---

## Key reference

### Embedding & search

| Key | Default | What it does |
|---|---|---|
| `embed_provider` | `ollama` | Where embeddings come from: `ollama` (local, recommended) or `openai` (any OpenAI-compatible endpoint). |
| `embed_model` | `qwen3-embedding:4b` | The Ollama model used to embed your notes and queries. Pull it with `ollama pull qwen3-embedding:4b`. |
| `embed_dim` | `2560` | Must match your embedding model's output size. Run `journal doctor` to check — it probes the model and tells you the right value. If you change models, run `journal index --rebuild`. |
| `reranker` | `""` (off) | Optional Ollama model (e.g. `qwen3:4b`) for re-ranking search results. Off by default; vector search alone is strong. |
| `ollama_base_url` | `http://localhost:11434` | Where Ollama is running. Change this only if you've moved Ollama to a different port or host. |
| `store_path` | `.journal/index/journal.db` | Path to the search index. This file is gitignored and can be deleted and rebuilt at any time. |
| `excludes` | _(see above)_ | Files and folders the indexer skips. `reflections/` (synthesis output) and `.journal/` (the index) are excluded by default. |

### Capture

| Key | Default | What it does |
|---|---|---|
| `editor` | `""` | The editor for `journal capture` with no text. Run as a shell command, so `code --wait` works. Empty falls back to `$JOURNAL_EDITOR`, then `$VISUAL`, then `$EDITOR`, then nano. |

### Synthesis

| Key | Default | What it does |
|---|---|---|
| `synth_provider` | `anthropic` | Who runs synthesis: `anthropic` (cloud Claude), `ollama` (local), or `openai` (any OpenAI-compatible endpoint like OpenRouter or Groq). |
| `synth_model` | `claude-sonnet-4-6` | Anthropic model, used when `synth_provider: anthropic`. |
| `synth_ollama_model` | `gemma4:12b` | Local model, used when `synth_provider: ollama`. Pull it with `ollama pull gemma4:12b`. |
| `synth_openai_base_url` | `https://api.openai.com/v1` | API endpoint when `synth_provider: openai`. For OpenRouter: `https://openrouter.ai/api/v1`. |
| `synth_openai_model` | `""` | Model ID for the OpenAI-compatible provider, e.g. `google/gemma-3-27b-it:free` on OpenRouter. |
| `synth_num_ctx` | `32768` | Context window for Ollama synthesis calls. Always set explicitly — Ollama's default is 4096 and it truncates silently. |
| `voice_profile` | `docs/VOICE_PROFILE.md` | Path to a style reference that shapes how synthesis sounds like you. Optional — synthesis works without it. |

### Privacy & egress

| Key | Default | What it does |
|---|---|---|
| `local_only` | `false` | When `true`, blocks all cloud AI paths: cloud synthesis is refused, and `journal mcp` is blocked by default (see `local_only_mcp`). Use this for a fully air-gapped setup. |
| `local_only_mcp` | `block` | Under `local_only`, whether `journal mcp` can run: `block` (default) or `allow`. Set to `allow` if your MCP client runs a local model and you're confident nothing leaves your machine. |

### Git

| Key | Default | What it does |
|---|---|---|
| `git_autocommit` | `true` | Auto-commit notes after `capture` and `index`. Turn off if you prefer to manage commits yourself. |
| `git_autocommit_sign` | `false` | Sign auto-commits. Off by default so the watcher doesn't prompt for a signing passphrase. |

### Remote backup

| Key | Default | What it does |
|---|---|---|
| `sync_enabled` | `false` | Gates `journal sync`. Nothing happens until you set this to `true`. See [Backup & Sync](sync.md). |
| `sync_conflict` | `manual` | How sync handles a divergence: `manual` (aborts, lets you resolve), `prefer-upstream` (takes the remote), `prefer-local` (keeps local). |

### Meetings & transcripts

| Key | Default | What it does |
|---|---|---|
| `transcripts.enabled` | `true` | Gates the transcript feature. |
| `transcripts.path` | `transcripts` | Where rendered transcripts are written. Gitignored. |
| `transcripts.auto_index` | `true` | Embed new transcripts as the watcher detects them. |
| `transcripts.tag` | `meeting` | Tag applied to every transcript chunk. Find them with `--tag meeting` or `--source transcript`. |
| `quill.enabled` | `true` | Gates `journal quill-sync`. |
| `quill.db_path` | OS default | Path to Quill's local database. macOS: `~/Library/Application Support/Quill/quill.db`. Windows: `~/AppData/Roaming/Quill/quill.db`. |
| `quill.accept_qm_imports` | `true` | Render manually-dropped `.qm` export files in the transcripts folder. |

---

## API keys

API keys are read from environment variables only — never stored in config files or logged:

- **`ANTHROPIC_API_KEY`** — for `synth_provider: anthropic`
- **`OPENAI_API_KEY`** — for `synth_provider: openai` or `embed_provider: openai`

Set them in your shell profile (`~/.zshrc`, `~/.bashrc`) or use a tool like [direnv](https://direnv.net) to set them per-project.

---

## Running journal from any directory

Every command accepts `--journal-dir` or the `JOURNAL_DIR` environment variable:

```sh
export JOURNAL_DIR=~/notes
journal search "anything"   # always uses ~/notes, regardless of current directory
```

This is useful for aliases: `alias jc='journal capture --journal-dir ~/notes'`
