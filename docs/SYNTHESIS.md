# Synthesis (cloud Claude or local Ollama)

`journal synth` turns the indexed firehose into curated drafts using the
configured provider — the Anthropic API (default) or a local Ollama model. It
runs scheduled or on demand — never in the capture/search hot path. See also
[Usage](USAGE.md) · [Configuration](CONFIGURATION.md).

```sh
journal synth weekly                 # dry-run by default: prints prompt + target path
journal synth weekly --write         # calls Claude, writes reflections/YYYY-Www.md (DRAFT)
journal synth daily --write          # summarize today → reflections/daily-YYYY-MM-DD.md
journal synth daily --date 2026-06-02 --write      # summarize a specific day
journal synth decisions --project canton --write   # appends a marked rollup to its _index.md
journal synth stale --days 21        # surface threads idle > 21 days
```

Kinds: **`weekly`** (ISO week → `reflections/YYYY-Www.md`), **`daily`** (one day,
`--date` or today → `reflections/daily-YYYY-MM-DD.md`), **`meetings`** (meeting
transcripts over the last `--days`, default **7** → `reflections/meetings-YYYY-MM-DD.md`;
see [QUILL.md](QUILL.md)), **`decisions`** (a `@decision` rollup; `--project` appends
to that project's `_index.md`), and **`stale`** (threads idle beyond `--days`). A daily cron at, say, 23:55 can summarize the day with
`journal synth daily --write` (or yesterday at 00:05 via
`--date "$(date -v-1d +%F)"` on macOS / `--date "$(date -d yesterday +%F)"` on Linux).

- **`--dry-run` is the default** (and is explicit-safe): it assembles and prints
  the prompt and the intended output path, and makes **no** network call and
  **no** file write — useful for cost control and verifying token boundaries.
- **`--write`** calls the API and writes output. `weekly`/`stale` write a new file
  under `reflections/` (refusing to clobber an existing one, since you edit
  those). `decisions --project <slug>` appends a **clearly-marked rollup block** to
  `projects/<slug>/_index.md` — it never mutates your note bodies.
- With the default `synth_provider: anthropic`, `--write` requires
  **`ANTHROPIC_API_KEY` in the environment**; it's never written to config or
  logged. The model is `synth_model` in config.

## Local synthesis (Ollama)

Set `synth_provider: ollama` in `.journal/config.yaml` and synthesis (plus the
grounded answer above `journal search` results) runs against your local Ollama —
no API key, and **note content never leaves the machine**. This is the provider
`local_only` mode requires (see [DATA-FLOWS.md](DATA-FLOWS.md)).

```yaml
synth_provider: ollama
synth_ollama_model: gemma4:12b   # pull first: ollama pull gemma4:12b
```

Model guidance (Apple Silicon, Q4 quantization):

| Model | Memory while loaded | Fit |
| --- | --- | --- |
| `gemma4:12b` (default) | ~8-10 GB | Comfortable on 32-48GB machines; strong faithful summarization. |
| `gemma4:26b` (MoE, 3.8B active) | ~20-24 GB peak | Near-frontier prose at ~50 tok/s; wants 48-64GB with headroom. |
| `llama3.1:8b` | ~5-6 GB | Budget pick; clean, low-hallucination summaries. |

Ollama loads the model on first call and unloads it after ~5 minutes idle, so
the memory cost is transient — synthesis runs don't permanently occupy RAM.
Every request sends `synth_num_ctx` (default 32768) explicitly because Ollama's
server default is 4,096 tokens and it **truncates the prompt silently** — a
weekly synthesis prompt would quietly lose most of its input.

Quality calibration: for faithful summarization (daily/weekly digests, decision
rollups) the local tier is close to cloud Sonnet. The gap shows in long-form
stylistic writing — if you use a voice profile and care about register, you may
prefer keeping `weekly` on the anthropic provider and running `daily`/answers
locally; the provider is one config line to flip either way.

## Writing in your voice

If a voice-profile file exists (path set by `voice_profile`, default
`docs/VOICE_PROFILE.md`), `journal synth` reads it **from your journal repo at
generation time** and injects it into the prompt as a **style reference** so
drafts sound like you — matching your language patterns and honoring any anti-AI
word/phrase guardrails it lists. It's read from disk, not baked into the binary;
each repo uses its own.

The profile is treated as style only: the prompt explicitly tells the model to
ignore meta-instructions in it (e.g. "ask which platform") since the destination
is fixed. Evolve it over time; it's plain markdown. Omit the file and synthesis
still works, just without the voice section.

`journal init` drops a starter template at `docs/VOICE_PROFILE.example.md` in your
repo ([source](../cmd/templates/VOICE_PROFILE.example.md)) — copy it to
`docs/VOICE_PROFILE.md` and make it yours. (In this source repo,
`docs/VOICE_PROFILE.md` is gitignored so a personal profile is never committed
here, and `docs/**` is excluded from indexing so it never pollutes search.)
