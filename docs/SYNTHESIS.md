# Synthesis (cloud Claude)

`journal synth` turns the indexed firehose into curated drafts using the
Anthropic API. It runs scheduled or on demand — never in the capture/search hot
path. See also [Usage](USAGE.md) · [Configuration](CONFIGURATION.md).

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
- Requires **`ANTHROPIC_API_KEY` in the environment** (only for `--write`); it's
  never written to config or logged. The model is `synth_model` in config.

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
