# MCP clients: Claude Desktop and local alternatives

`journal mcp` is a standard stdio MCP server — any MCP-capable client can drive
it. Claude Desktop is the reference setup ([INTEGRATIONS.md](INTEGRATIONS.md)),
but it forwards retrieved note content to Anthropic's cloud models. This page
covers **fully local** alternatives: a desktop GUI + a local model (Ollama or
built-in) + journal's MCP tools, with zero egress. See
[DATA-FLOWS.md](DATA-FLOWS.md) for why that matters.

> **Verified June 2026.** MCP client support moves fast — check each project's
> docs if you're reading this much later.

## Ground rules (read first)

1. **Ollama is not an MCP client.** It only serves the model; the GUI client
   does the MCP bridging. "Local MCP" always means *client + model*, two pieces.
2. **The model matters more than the client.** Tool calling needs a model
   trained for it: **Gemma 4** and **Qwen3-family** models are the reliable
   choices; older or non-function-calling fine-tunes will simply never invoke
   the journal tools.
3. **Use an absolute path to the binary.** GUI apps inherit a minimal `PATH`
   (the same gotcha as Claude Desktop): `command` must be e.g.
   `/opt/homebrew/bin/journal`.

## Recommended clients

### LM Studio — lowest friction, closest to Claude Desktop

- stdio MCP since v0.3.17; per-call confirmation dialog with editable
  arguments. Runs models with its own llama.cpp/MLX engine (no Ollama needed).
- macOS/Windows/Linux; free, app closed-source.
- Configure via Program sidebar → Install → Edit `mcp.json` — the config shape
  is the Claude Desktop / Cursor notation, so the journal block is identical:

```json
{
  "mcpServers": {
    "journal": {
      "command": "/opt/homebrew/bin/journal",
      "args": ["mcp", "--repo", "/Users/you/journal"]
    }
  }
}
```

Docs: https://lmstudio.ai/docs/app/mcp

### Jan — best open-source option

- stdio MCP configured in the UI (Settings → MCP Servers → `+`: name, command,
  arguments, env), with inline tool-approval cards. AGPL-3.0, very active.
- Runs models via its built-in llama.cpp engine, or against Ollama as an
  OpenAI-compatible provider (`http://localhost:11434/v1`).
- Gotcha: tool calling is a **per-model capability toggle** in Jan's model
  settings — easy to miss; nothing works until it's on.

Docs: https://www.jan.ai/docs/desktop/mcp

### AnythingLLM — best if you're already Ollama-centric

- First-class Ollama support plus stdio MCP via
  `anythingllm_mcp_servers.json` (same `mcpServers` JSON shape as above). MIT.
- Gotcha: MCP tools fire **only in `@agent` mode**, not plain chat — a real UX
  difference from Claude Desktop.

Docs: https://docs.anythingllm.com/mcp-compatibility/overview

## Also viable / watch list

| Client | Notes |
| --- | --- |
| Cherry Studio | stdio MCP + Ollama provider, but open reports of Ollama models not triggering MCP tools — verify with your model first. |
| 5ire | Lightweight open-source client; Ollama provider; per-model tool-compatibility badge. |
| Witsy | "Universal MCP client", Ollama support, AGPL. |
| Msty Studio | stdio MCP in the Toolbox; closed-source, some features paid. |
| llama.cpp web UI | Native MCP client landed in llama-server's UI (March 2026), stdio via `--webui-mcp-proxy` — no extra app at all, but young and rough. |

**Poor fits:** Open WebUI (no stdio MCP — needs the `mcpo` HTTP proxy) and
LibreChat (Docker/Node web stack; heavier than a desktop app warrants).

## A fully local stack, end to end

```sh
ollama pull gemma4:12b          # tool-calling capable model for the client
ollama pull qwen3-embedding:4b  # journal's embedding model (if not already)
```

1. Configure one client above with the journal `mcpServers` block.
2. Pick Gemma 4 / Qwen3 as the chat model (in Jan: enable its tool-calling
   capability).
3. Optionally set `synth_provider: ollama` so `journal synth` is local too
   ([SYNTHESIS.md](SYNTHESIS.md)).

4. If you run with `local_only: true`, also set `local_only_mcp: allow` — the
   blanket default blocks `journal mcp` because the server cannot verify its
   client; `allow` is your attestation that the client above keeps everything
   local ([DATA-FLOWS.md](DATA-FLOWS.md)).

Result: capture, index, search, MCP chat, and synthesis with no note content
leaving the machine.
