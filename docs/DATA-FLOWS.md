# Data flows & the `local_only` egress switch

This page is the one-stop answer to *"what does journal store, and what — if
anything — leaves my machine?"* It exists for security reviews (e.g. users in
HIPAA/HITRUST-constrained environments) and for anyone who wants the local-only
guarantee spelled out.

## What is stored, and where

| Data | Location | Notes |
| --- | --- | --- |
| Notes (source of truth) | Markdown files in your journal repo | Plain files you own; never transformed in place by retrieval. |
| Vector index | `.journal/index/journal.db` (sqlite-vec) | A **disposable cache** of embeddings + chunk text, rebuildable any time with `journal index --rebuild`. Gitignored. |
| Meeting transcripts | `transcripts/` (gitignored) | Rendered from the local Quill database (read-only; a copy is made first). Quill data is already on your machine — `quill-sync` makes no network calls. |
| Synthesis output | `reflections/` | Drafts written by `journal synth --write`. |
| Config | `.journal/config.yaml` | Non-secret settings only. The Anthropic API key is read from the environment and is **never** written to disk or logged. |

Encryption at rest is the platform's job, by design: run FileVault (macOS) /
BitLocker (Windows) / LUKS (Linux) full-disk encryption, which is how workstation
controls expect it to be handled. journal adds no encryption layer of its own.

## What can leave the machine, and when

Embedding, vector search, and reranking are **always local** (Ollama on
loopback). The complete list of potential egress paths:

| Path | Trigger | Where it goes | Closed by |
| --- | --- | --- | --- |
| Cloud synthesis | `journal synth --write`, the answer above `journal search`, or voice-note shaping in `journal log --text`, with `synth_provider: anthropic` *or* `openai` | Assembled prompt (note excerpts / raw voice text) → Anthropic, or your OpenAI-compatible endpoint (OpenRouter/…) | `synth_provider: ollama` or `local_only: true` |
| Remote embeddings | indexing/search with `embed_provider: openai` | Note text → your OpenAI-compatible `/embeddings` endpoint | `embed_provider: ollama` (default) or `local_only: true` |
| Git backup | `journal sync` (opt-in, `sync_enabled: true`) | Your notes → *your* git remote | `sync_enabled: false` (default) — not affected by `local_only`, since it's your own remote, not a cloud-AI service |
| MCP client | `journal mcp` serving an MCP client | The server itself is local stdio — but the **client** (e.g. Claude Desktop) typically forwards retrieved note content to a cloud model | `local_only: true`; or use a local-model MCP client ([CLIENTS.md](CLIENTS.md)) |
| Networked Ollama | `ollama_base_url` pointed at another host | Note content → that host | Loopback URL (default); enforced under `local_only` |

There is no telemetry, no crash reporting, no update check — the binary makes
no network connection outside the table above.

## Voice note write path (`journal log --text`)

`journal log --text "..."` runs a four-stage pipeline — all local except the
optional LLM shaping step:

```
raw text ──► Shape ──► Assemble ──► Land ──► Index
              (LLM)     (pure)      (disk)   (Ollama + sqlite-vec)
```

1. **Shape** (optional) — raw text is sent to the configured `synth_provider`
   for cleaning, title generation, summarization, tag extraction, and marker
   surfacing. Skipped when `log.shaping.enabled: false`, when `local_only: true`
   with a cloud provider, or when no synthesis key is available — the raw text
   proceeds to Assemble unchanged.
2. **Assemble** — pure in-process rendering: YAML frontmatter
   (`source: voice`, `duration_sec`, `transcriber`, tags, marker counts) plus
   `## Summary`, `## Notes`, and an optional collapsed `## Raw transcript` block.
3. **Land** — `os.WriteFile` to `logs/YYYY-MM-DD-HHMM-<slug>.md` under
   `log.landing.dir`. A note is **always written** — shaping failure is
   non-fatal. Optional: one-line daily breadcrumb when `log.landing.backlink_daily: true`.
4. **Index** — `Indexer.IndexVoice` chunks the landed note (reusing the
   transcript line-windowing strategy) and upserts chunks with
   `source = "voice"`. Failure is non-fatal — the note is on disk and
   re-indexable with `journal index`.

Voice chunks are scoped separately from notes and transcripts; use
`journal search --source voice` (aliases `log`/`logs`) to scope results.

## `local_only: true`

One config line blocks **cloud-AI egress** — note content reaching a
third-party inference service:

- **Cloud synthesis is refused** — `synth --write` and `search` answers error
  unless `synth_provider: ollama` (a cloud `anthropic`/`openai` provider is
  rejected; see [SYNTHESIS.md](SYNTHESIS.md)).
- **Remote embeddings are refused** — `embed_provider` must be `ollama`
  (`openai` ships note text off-machine).
- **`ollama_base_url` must be loopback** — a networked Ollama host is egress.
- **`journal mcp` is blocked by default** — the typical MCP client sends
  retrieved content to a cloud model. If you run a local-model client
  ([CLIENTS.md](CLIENTS.md)), set `local_only_mcp: allow`. That setting is an
  **attestation, not a verification**: stdio MCP gives the server no
  trustworthy client identity, so the binary cannot tell LM Studio from Claude
  Desktop — `allow` shifts responsibility for the MCP path's egress to you.
  Every other `local_only` guarantee remains enforced.

**`local_only` does *not* disable `journal sync`.** Sync backs your notes up to
*your own* git remote — that's data you control, not a third-party AI service,
so it stays governed solely by `sync_enabled`. This means local AI **and** git
backup coexist: `local_only: true` + `sync_enabled: true`. For a fully sealed
"nothing leaves this machine" posture, keep `sync_enabled: false` (the default);
`journal doctor`'s `egress` check shows which of these you're in. Note that for
strict HIPAA, pushing notes to a hosted git remote is itself disclosure to that
host (e.g. GitHub) and would need its own BAA — which is exactly why sync is
off by default and a deliberate opt-in.
- **`ollama_base_url` must be loopback** (localhost / 127.0.0.0/8 / ::1) —
  validated at config load, since a networked Ollama host is egress.

`journal doctor` reports the current posture in one line (the `egress` check),
so "does anything leave this machine?" is verifiable at a glance.

## Compliance posture (HIPAA), in brief

There is no such thing as HIPAA-certified software — compliance attaches to
organizations, not products ([HHS FAQ](https://www.hhs.gov/hipaa/for-professionals/faq/2003/are-we-required-to-certify-our-organizations-compliance-with-the-standards/index.html)).
For a fully local deployment (`local_only: true`), the journal vendor never
creates, receives, maintains, or transmits your data — which is the test for
Business Associate status. Per
[HHS FAQ 256](https://www.hhs.gov/hipaa/for-professionals/faq/256/is-software-vendor-business-associate/index.html),
a software vendor without access to protected health information is **not** a
business associate, and no BAA applies; the workstation controls (disk
encryption, device policy) are your organization's, as with any local editor.
If you enable a cloud path, that analysis changes — Anthropic offers BAAs for
its API, and Claude is HIPAA-eligible via AWS Bedrock — but that is between
your organization and the cloud provider.
