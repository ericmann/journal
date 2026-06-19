# AI Synthesis

`journal synth` reads your notes and generates curated summaries — weekly digests, daily rollups, decision histories, stale project alerts — using a language model of your choice. It never runs automatically; you invoke it when you want it.

## The basics

```sh
journal synth weekly         # preview: prints the prompt and output path, no API call
journal synth weekly --write # actually generates and saves the output
```

By default, synthesis is a dry run: it shows you the prompt it would send and the file it would write, but doesn't call any API. Add `--write` when you're ready.

Output files land in `reflections/` in your journal repo (e.g. `reflections/2026-W24.md`). Existing files are never overwritten — journal adds a `-2`, `-3`, etc. suffix if one already exists.

## Synthesis kinds

| Command | What it generates |
|---|---|
| `journal synth weekly` | A summary of your ISO week's notes → `reflections/YYYY-Www.md` |
| `journal synth daily` | Today's notes at a glance → `reflections/daily-YYYY-MM-DD.md` |
| `journal synth daily --date 2026-06-02` | A specific day |
| `journal synth meetings` | Digest of recent meeting transcripts (last 7 days by default) |
| `journal synth decisions --project acme` | A rollup of `@decision` notes for a project |
| `journal synth stale --days 21` | Surface threads you haven't touched in 3 weeks |

## Choosing a provider

You have three options, set in `.journal/config.yaml`:

### Cloud Claude (default)

```yaml
synth_provider: anthropic
synth_model: claude-sonnet-4-6
```

Requires an Anthropic API key in `ANTHROPIC_API_KEY`. Best quality for long-form synthesis and voice-matching. Your note excerpts are sent to Anthropic's API.

### OpenAI-compatible (OpenRouter, Groq, etc.)

```yaml
synth_provider: openai
synth_openai_base_url: https://openrouter.ai/api/v1
synth_openai_model: google/gemma-3-27b-it:free
```

Requires an API key in `OPENAI_API_KEY`. This is the middle path: capable cloud synthesis without a Claude bill. Free models are available on OpenRouter.

### Local Ollama (fully offline)

```yaml
synth_provider: ollama
synth_ollama_model: gemma4:12b
```

No API key, no data leaving your machine. For daily summaries and decision rollups, the quality is close to cloud Claude. For long-form weekly digests where voice matters a lot, cloud models have the edge — but for many workflows, local is excellent.

Pull the model first: `ollama pull gemma4:12b`

## Scheduling synthesis

Run `journal synth daily --write` on a schedule to get automatic daily digests. Add it to cron (at, say, 11:55 PM):

```sh
55 23 * * * /usr/local/bin/journal synth daily --write >> ~/.journal-synth.log 2>&1
```

Or use launchd (macOS) / systemd timers (Linux) — the same patterns as the index watcher. See [Configuration Reference](configuration.md) for details.

## Writing in your voice

If you create a file at `docs/VOICE_PROFILE.md` in your journal repo, journal reads it at synthesis time and injects it as a style reference. The model uses it to match your vocabulary, tone, and any phrases you want to avoid.

journal init creates a starter template at `docs/VOICE_PROFILE.example.md` — copy it, make it yours, and your digests will start sounding more like you over time.

The profile is plain Markdown. Evolve it whenever you notice the output drifting from your style.
