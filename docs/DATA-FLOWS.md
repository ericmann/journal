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
| Cloud synthesis | `journal synth --write` or the answer above `journal search` results, with `synth_provider: anthropic` | Assembled prompt (note excerpts) → Anthropic API | `synth_provider: ollama` or `local_only: true` |
| Git backup | `journal sync` (opt-in, `sync_enabled: true`) | Your notes → *your* git remote | `sync_enabled: false` (default) or `local_only: true` |
| MCP client | `journal mcp` serving an MCP client | The server itself is local stdio — but the **client** (e.g. Claude Desktop) typically forwards retrieved note content to a cloud model | `local_only: true`; or use a local-model MCP client ([CLIENTS.md](CLIENTS.md)) |
| Networked Ollama | `ollama_base_url` pointed at another host | Note content → that host | Loopback URL (default); enforced under `local_only` |

There is no telemetry, no crash reporting, no update check — the binary makes
no network connection outside the table above.

## `local_only: true`

One config line hard-disables every egress path:

- **Cloud synthesis is refused** — `synth --write` and `search` answers error
  unless `synth_provider: ollama` (see [SYNTHESIS.md](SYNTHESIS.md)).
- **`journal sync` is disabled**, regardless of `sync_enabled`.
- **`journal mcp` is disabled** — the conservative default, because the typical
  MCP client sends retrieved content to a cloud model. If you run a vetted
  local-model client ([CLIENTS.md](CLIENTS.md)), you can keep `local_only` off
  and simply not configure cloud paths; revisiting this gate is on the roadmap.
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
