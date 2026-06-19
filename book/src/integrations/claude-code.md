# Claude Code

Claude Code is the primary way to use journal with an AI assistant. You can ask Claude natural language questions about your notes, and it will search, retrieve, and reason over them — all locally.

## How it works

Claude Code calls journal's CLI commands (`journal search --json`, `journal decisions --json`, etc.) and reads their JSON output. Your notes stay on your machine; Claude Code just gets the search results back.

The integration uses journal's stable `--json` API, so the results are structured and consistent. Claude always knows exactly which file and lines a finding came from.

## Setting up the skill

journal ships a Claude Code skill at `skills/journal/SKILL.md`. This teaches Claude how to use journal effectively — which commands to run, how to read the output, how to cite findings.

Make it discoverable in one of two ways:

**Option 1:** Keep your journal repo open as a workspace in Claude Code. Claude picks up the skill file automatically.

**Option 2:** Symlink it into your skills library:
```sh
ln -s "$PWD/skills/journal" ~/.claude/skills/journal
```

Make sure `journal` is on your `PATH` (`journal --help` works in your terminal).

## What Claude can do

Once the skill is active, Claude can:

**Search semantically:**
```
"What did I decide about the authentication approach?"
→ runs: journal search "authentication approach" --json
```

**Find decisions:**
```
"What decisions did I make about the Canton project last month?"
→ runs: journal decisions --project canton --since 4w --json
```

**Surface stale threads:**
```
"What projects haven't I touched in two weeks?"
→ runs: journal threads --stale --days 14 --json
```

**Capture notes:**
```
"Note that we decided to use Redis Cluster @decision #redis"
→ runs: journal capture "decided to use Redis Cluster @decision #redis"
```

**Check and complete todos:**
```
"What are my open todos? Mark the Acme follow-up as done."
→ runs: journal todos --json, then: journal done "Acme follow-up"
```

## Citations

Every result includes a `path:line_start-line_end` reference. Claude cites these in its responses, so you can open the exact section of your notes to verify. In most terminals and editors, these are clickable.

## Using the MCP server (alternative)

If you prefer MCP over the CLI skill, you can register `journal mcp` as an MCP server for Claude Code. This gives Claude Code access to the same 13 tools available to Claude Desktop — no need to shell out to `journal` CLI commands manually.

Add to your Claude Code MCP config:

```json
{
  "mcpServers": {
    "journal": {
      "command": "/usr/local/bin/journal",
      "args": ["mcp", "--repo", "/path/to/your/journal"]
    }
  }
}
```

See [Claude Desktop](claude-desktop.md) for the full tool, resource, and prompt reference — both integrations share the same MCP server.

## The JSON schema

If you want to use journal with your own scripts or agents, the search output looks like:

```json
{
  "results": [
    {
      "path": "daily/2026/06/2026-06-01.md",
      "line_start": 3,
      "line_end": 7,
      "heading": "09:14 #cabot #litellm",
      "snippet": "Routing fallback isn't triggering when Qwen OOMs...",
      "score": 0.91,
      "tags": ["cabot", "litellm"],
      "markers": ["decision"]
    }
  ]
}
```

An empty result set is `{"results": []}` — distinct from an error (`{"error": "..."}` with a non-zero exit code).
