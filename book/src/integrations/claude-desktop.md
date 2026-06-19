# Claude Desktop

You can connect Claude Desktop to your journal using the MCP (Model Context Protocol). Once connected, Claude can search your notes, read specific entries, check your todos, and capture new notes — all from the Claude chat interface.

## What is MCP?

MCP is a standard protocol that lets Claude Desktop connect to local tools and services. journal ships a built-in MCP server (`journal mcp`) that exposes your journal to Claude.

The inference (the AI reasoning) happens in Claude's cloud. The retrieval (the semantic search over your notes) happens locally on your machine. Your notes are shared with Claude as search results — not uploaded in bulk.

## Setup

Add journal to Claude Desktop's MCP config file. Find the config at:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

Add this block (create the file if it doesn't exist):

```json
{
  "mcpServers": {
    "journal": {
      "command": "/usr/local/bin/journal",
      "args": ["mcp", "--repo", "/Users/you/journal"]
    }
  }
}
```

Replace `/usr/local/bin/journal` with the actual path to your journal binary (find it with `which journal`), and `/Users/you/journal` with the path to your journal repository.

> **Use an absolute path for the command.** Claude Desktop doesn't inherit your shell's PATH, so a relative path like `journal` won't work. Always use the full path.

Restart Claude Desktop, and the journal tools appear.

## What Claude can do

Once connected, Claude has access to these tools:

| Tool | What it does |
|---|---|
| `search` | Semantic search over your notes |
| `show` | Read the full content of a specific note |
| `recent` | See your most recent notes |
| `decisions` | Find notes marked `@decision` |
| `threads` | See project activity and stale threads |
| `meetings` | List recent meeting transcripts |
| `todos` | List open `@todo` items |
| `done` | Complete a todo |
| `capture` | Add a new note |

Try asking:
- "What did I work on this week?"
- "What decisions did I make about the Acme project?"
- "Add a note that we decided to go with Redis Cluster @decision"
- "What are my open todos? Mark the pricing one as done."

## Privacy

The MCP server runs locally on your machine. Claude Desktop sends tool calls (like "search for X") to the local server, and the server runs the search and returns results. Your note content travels from your machine to Anthropic's cloud as part of Claude's context — the same as pasting text into a chat window.

If you want a fully local setup (including local AI inference), see [Local MCP Clients](local-clients.md).

## Troubleshooting

**Tools don't appear after restart:** Check that the JSON is valid and the path to the binary is correct and absolute.

**"Connection failed":** Make sure `journal --help` works in your terminal and the path in the config matches.

**Ollama needs to be running:** The search tools require Ollama for embedding queries. Start Ollama before asking Claude to search your notes.
