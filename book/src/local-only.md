# Going Fully Local

journal works entirely on your machine by default — your notes are never uploaded for indexing or search. But if you also want synthesis (AI summaries) to run locally, with zero data leaving your computer at all, you can set that up too.

This guide walks through a complete zero-egress setup using Ollama for both embeddings and synthesis.

## What "fully local" means

By default, `journal synth` sends your note excerpts to Anthropic's cloud API. The `local_only` setup replaces that with a local Ollama model, so:

- Indexing: local (already the default)
- Search: local (already the default)
- Synthesis: local (this is what we're adding)
- MCP tools: optionally local, if your chat client runs locally too

Nothing leaves your machine.

## Hardware requirements

| Machine RAM | Embedding model | Synthesis model |
|---|---|---|
| 16 GB | `qwen3-embedding:4b` (~2.5 GB) | `llama3.1:8b` (~5 GB) |
| 32–48 GB | same | `gemma4:12b` (~8–10 GB) — recommended |
| 64 GB+ | same | `gemma4:26b` (~20–24 GB peak) |

Ollama loads models on demand and unloads them after about 5 minutes of inactivity, so these are transient peaks, not standing costs.

## Step 1: Install and start Ollama

**macOS:**
```sh
brew install ollama
brew services start ollama
```

**Linux:**
```sh
curl -fsSL https://ollama.com/install.sh | sh
# Installed as a systemd service, starts automatically
```

Verify it's running: `ollama --version`

## Step 2: Pull both models

```sh
ollama pull qwen3-embedding:4b   # for indexing and search (~2.5 GB)
ollama pull gemma4:12b           # for synthesis (~8 GB, or pick a smaller model)
```

## Step 3: Update your config

In your journal repo's `.journal/config.yaml`:

```yaml
synth_provider: ollama
synth_ollama_model: gemma4:12b

local_only: true          # block all cloud AI paths
local_only_mcp: allow     # if you want to use journal with a local MCP chat client
```

The `local_only: true` flag is a hard kill-switch — it refuses cloud synthesis and requires Ollama to be on loopback. It doesn't affect `journal sync`, which backs up to your own git remote (not cloud AI).

`local_only_mcp: allow` is only needed if you plan to use `journal mcp` with a local chat client like Jan or LM Studio. Leave it as `block` (the default) until you've set up the client.

## Step 4: Verify

```sh
journal doctor
```

The egress line should say something like:
```
local_only: no cloud-AI egress (synth local: gemma4:12b); mcp blocked by policy
```

Try it:
```sh
journal synth daily              # dry run: shows the prompt, no network call
journal synth daily --write      # runs locally, writes reflections/daily-YYYY-MM-DD.md
journal search "what did I work on"   # semantic search, fully local
```

---

## Adding a local MCP chat client (optional)

If you want to chat with your notes using a local AI model (no Claude), you can pair journal's MCP server with [Jan](https://jan.ai) or [LM Studio](https://lmstudio.ai). Both can connect to Ollama and call journal tools.

See [Local MCP Clients](integrations/local-clients.md) for setup instructions for each client.

---

## Quality expectations

For summarization tasks — daily digests, weekly rollups, decision rollups — local models like `gemma4:12b` perform very close to cloud Claude. The gap shows most on long-form stylistic writing where voice profile matching matters a lot.

A practical approach: run daily synthesis and search answers locally, and keep weekly digests on `synth_provider: anthropic` if you care about the writing quality. You can switch providers with a single line in config.
