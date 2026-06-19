# Local MCP Clients

Claude Desktop is great, but it sends your note content to Anthropic's cloud for inference. If you want fully local AI — where your notes, the search, and the AI reasoning all stay on your machine — you need a different chat client.

Three clients work well with journal: **LM Studio**, **Jan**, and **AnythingLLM**.

All three follow the same basic pattern: connect them to Ollama (for the AI model) and add journal as an MCP server.

---

## Before you start

A few things apply to all local clients:

1. **You need a tool-capable model.** Not every model knows how to call tools. Gemma 4 and Qwen3 work reliably. Older or fine-tuned models may ignore tool calls entirely.

2. **Use absolute paths.** Local GUI apps don't inherit your shell's PATH. Always use the full path to the journal binary (e.g. `/opt/homebrew/bin/journal`, not just `journal`). Find the path with `which journal`.

3. **Ollama must be running.** Start it with `brew services start ollama` (macOS) or check that the systemd service is active.

---

## LM Studio — lowest friction

[LM Studio](https://lmstudio.ai) is the closest thing to Claude Desktop for local models. It has a clean interface, MCP support since v0.3.17, and per-call confirmation dialogs.

**Note:** LM Studio runs its own inference engine (llama.cpp/MLX) — it doesn't use Ollama. You'll download a second copy of the chat model in LM Studio's model store. If you're already using Ollama for journal's synthesis, you'll have two copies of the model. That's the trade-off for LM Studio's polished UX.

### Setup

1. Install LM Studio from [lmstudio.ai](https://lmstudio.ai) (macOS/Windows/Linux).
2. Download a model in the Discover tab — Gemma 4 12B works well.
3. Go to Program sidebar → Install → Edit `mcp.json`:

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

4. Start a chat with your downloaded model and ask "What did I work on this week?"

---

## Jan — best open-source option

[Jan](https://jan.ai) is AGPL-licensed and actively maintained. It can use Ollama as its model provider, so you get one shared model runtime for both journal synthesis and the chat client.

Jan has some quirks you need to know about upfront — they're easy to fix but will trip you up silently if you skip them.

### Setup

1. Install Jan from [jan.ai](https://jan.ai).

2. **Connect it to Ollama:** Settings → Model Providers → add an OpenAI-compatible provider, base URL `http://localhost:11434/v1`. Enter any non-empty placeholder as the API key (e.g. `ollama`) — Jan requires the field but Ollama ignores it.

3. **Enable tool calling for your model.** This is the most-missed step. In the model's settings, turn on **Tools / Function Calling**. Without this, Jan never sends tool definitions to the model, and the model narrates ("I'm looking at your tools...") instead of actually calling them. The tell in Jan's trace: *"No tools are available."*

4. **Add journal as an MCP server:** Settings → MCP Servers → `+`. Enter the arguments **one per line**: `mcp` on the first line, `--repo` on the second, `/path/to/your/journal` on the third. Don't put them all on one line — Jan doesn't split on spaces.

   > **macOS gotcha:** System-level Smart Dashes can silently convert `--repo` to `—repo` (an em dash), which breaks the flag. Paste instead of typing, or disable Smart Dashes in System Settings → Keyboard → Text Input → Edit.

5. **Use a minimal assistant.** Jan's default assistant may have a web-search system prompt that competes with your journal tools. Create a simple assistant with a prompt like: *"Use the available journal tools to answer questions about my notes."*

6. **Allow `Origin: null` in Ollama.** Jan's chat requests include `Origin: null`, which Ollama rejects by default (you get "Generation failed: Forbidden"). Set `OLLAMA_ORIGINS=null` in your environment. On macOS, set it in the launchd environment (not `.zshrc`) so the Ollama menubar app picks it up:

```sh
launchctl setenv OLLAMA_ORIGINS "null,http://tauri.localhost,https://tauri.localhost"
```

Then restart Ollama. Verify it worked: `curl -s -o /dev/null -w '%{http_code}\n' -H 'Origin: null' http://localhost:11434/v1/models` should print `200`.

---

## AnythingLLM — best if you're already Ollama-centric

[AnythingLLM](https://anythingllm.com) has first-class Ollama support and stdio MCP via a config file using the same JSON shape as Claude Desktop.

One important difference from the other clients: **MCP tools only fire in `@agent` mode**, not plain chat. Start messages with `@agent` to use your journal tools.

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

---

## Enabling local-only mode

If you want to be certain nothing leaves your machine, set `local_only: true` in your journal config and `local_only_mcp: allow` to permit the MCP server:

```yaml
local_only: true
local_only_mcp: allow    # "allow" is your attestation that the MCP client is local
```

Then run `journal doctor` — the egress line should confirm the fully-local posture.
