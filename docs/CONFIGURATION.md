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

# --- Voice note capture (journal log --text) ---
log:
  shaping:
    enabled: true                      # LLM shaping: clean, title, summarize, extract markers
    keep_raw_transcript: true          # include collapsed <details> block with raw text
  landing:
    dir: logs                          # repo-relative landing zone for voice notes
    backlink_daily: false              # append one-line breadcrumb to today's daily note
  # Audio/transcriber keys — recording toggle (Phase 3) + transcriber:
  audio:
    device: default
    sample_rate: 16000
    channels: 1
    tmp_dir: ""                 # scratch recording dir; "" -> <os temp dir>/journal-log
    max_duration: 900           # seconds; self-finalizes + processes at the cap (0 = no cap)
    silence_autostop: false     # safety-net stop after sustained silence (not the primary stop)
    keep_wav: false             # retain the wav after landing + record it in `audio:` frontmatter
  transcriber:
    backend: whisper.cpp        # transcription engine ("whisper.cpp" is the default)
    model: base.en              # model name without .bin (e.g. base.en, small.en)
    model_dir: ~/.cache/journal/models  # where model files are stored; ~ is expanded

schema_version: "2.0"                  # config schema; `journal init` upgrades older repos

# --- Voice transcription model (see `journal models pull`) ---
transcriber:
  model_id: Systran/faster-whisper-base.en   # HuggingFace model id (ungated)
  revision: main                              # branch/tag/commit to pin
  filename: ""                                # remote/on-disk file name; "" defaults to "model.bin"
  checksum: ""                               # SHA-256 hex; populated after first pull
  model_dir: ~/.cache/journal/models          # global model store; not repo-specific
  gated: false                                # set true for gated repos (e.g. pyannote diarization)
  accept_url: ""                              # HuggingFace terms-acceptance page; required when gated: true

# --- Meeting diarization model (optional; see `journal models pull` and docs/TRANSCRIBE.md) ---
# Empty model_id (the default) means disabled: `journal models pull` skips it
# entirely, no network call. Fill in model_id (and gated/accept_url) to
# provision pyannote as a credential preflight before running WhisperX
# diarization — recommended values:
diarization:
  model_id: ""      # e.g. pyannote/speaker-diarization-community-1
  revision: main
  filename: ""      # e.g. config.yaml — pyannote's repo has no model.bin, pull its manifest instead
  checksum: ""
  model_dir: ~/.cache/journal/models   # shares the transcriber model store
  gated: false       # true for pyannote — set alongside accept_url
  accept_url: ""     # e.g. https://huggingface.co/pyannote/speaker-diarization-community-1
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
| `log.shaping.enabled` | `true` | Run the LLM shaping step for `journal log` (clean, title, summarize, extract markers). When `false` (or when no provider is available), the raw text lands unchanged. |
| `log.shaping.keep_raw_transcript` | `true` | Include a collapsed `<details>` block with the original raw text in the landed note. |
| `log.landing.dir` | `logs` | Repo-relative directory for landed voice notes (`YYYY-MM-DD-HHMM-<slug>.md`). |
| `log.landing.backlink_daily` | `false` | Append a one-line breadcrumb to today's daily note after landing. |
| `log.audio.tmp_dir` | `""` (→ `<os temp dir>/journal-log`) | Directory for scratch recording WAVs made by the `journal log` toggle. `~` is expanded; created on demand. |
| `log.audio.max_duration` | `900` | Caps a single recording in seconds; the recorder self-finalizes and hands off to the pipeline at the limit. `0` disables the cap. |
| `log.audio.silence_autostop` | `false` | Optional safety-net stop after a sustained silence interval — not the primary stopping mechanism (that's the toggle/`--stop`). |
| `log.audio.keep_wav` | `false` | Retain the recorded WAV after a successful pipeline run and record its path in the landed note's `audio:` frontmatter. Default: delete the scratch WAV once the note lands. A WAV passed directly (`journal log <file>.wav`) is never auto-deleted regardless of this setting. |
| `log.transcriber.backend` | `whisper.cpp` | Transcription engine used by `journal log <audio.wav>`. Only `whisper.cpp` is built in. |
| `log.transcriber.model` | `base.en` | Model name (without `.bin`) for the log transcriber. Small English models (`base.en`, `small.en`) give fast desk-dictation results. |
| `log.transcriber.model_dir` | `~/.cache/journal/models` | Directory where model files are stored. Defaults to the same path as `transcriber.model_dir` so both paths share one model store. `~` is expanded. |
| `schema_version` | `2.0` | Config schema version; `journal init` upgrades older repos in place. |
| `transcriber.model_id` | `Systran/faster-whisper-base.en` | HuggingFace model id for the whisper model used by `journal transcribe`. Ungated (no HF token needed for the `base.en`/`small.en` class). |
| `transcriber.revision` | `main` | Branch, tag, or commit SHA to pin for the model download. |
| `transcriber.filename` | `""` | Remote/on-disk file name to pull. Empty defaults to `model.bin` (every whisper model). Set it (e.g. `config.yaml`) for a repo whose primary artifact isn't named `model.bin` — see `diarization.filename` below. |
| `transcriber.checksum` | `""` | Expected SHA-256 hex digest of the downloaded model file. Empty = no verification (useful before the first pull). Set it from the checksum printed by `journal models pull` to lock the version. |
| `transcriber.model_dir` | `~/.cache/journal/models` | Directory where model files are stored. Not repo-specific — shared across all journal repos on the machine. `~` is expanded. |
| `transcriber.gated` | `false` | Marks `model_id` as a gated HuggingFace repo (e.g. `pyannote/speaker-diarization-3.1`) that requires accepting terms on huggingface.co and an `HF_TOKEN`. `journal models pull` fails with an explicit "accept terms at `accept_url`, set `HF_TOKEN`" message instead of a raw 401 when this is `true` and the token is missing/invalid. |
| `transcriber.accept_url` | `""` | The HuggingFace terms-acceptance page for `model_id`. Shown in the pull failure message and recorded in `MODELS.md`. Ignored when `gated` is `false`. |
| `diarization.model_id` | `""` (disabled) | HuggingFace model id for the meeting pipeline's optional speaker-diarization model (e.g. `pyannote/speaker-diarization-community-1`). Empty means disabled — `journal models pull` skips it entirely, no network call. Same shape and keys as `transcriber.*`; see [TRANSCRIBE.md](TRANSCRIBE.md) for the credential-preflight workflow. |
| `diarization.filename` | `""` | Same meaning as `transcriber.filename`. pyannote's repo has no `model.bin`; set this to `config.yaml` to pull its primary manifest as a preflight. |
| `diarization.model_dir` | `~/.cache/journal/models` | Same default as `transcriber.model_dir` — shares the same model store. |
| `diarization.gated` / `diarization.accept_url` | `false` / `""` | Same meaning as `transcriber.gated`/`transcriber.accept_url`. Set both for pyannote (gated). |

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
- **`HF_TOKEN`** — only needed when `transcriber.gated: true` or
  `diarization.gated: true` (e.g. pyannote diarization models). Accept the
  model's terms at the corresponding `accept_url` on huggingface.co first,
  then export the token — `journal models pull` fails with that exact "accept
  terms" message instead of an opaque 401 if either step is missing. Ungated
  models (the default) never need this. `diarization.model_id` is empty (and
  thus skipped by `journal models pull`) until you configure it.

To keep personal and work journals separate, clone the repo into different
directories and export different keys in each.
