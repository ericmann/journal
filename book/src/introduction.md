# Welcome to journal

You write notes every day — decisions you made, bugs you chased, ideas you want to revisit. But finding them again when you need them? That's the hard part.

**journal** is a command-line tool that turns a folder of plain Markdown files into a searchable, AI-queryable personal knowledge base. You write naturally. journal handles the rest: it keeps your notes in git, builds a local search index, and lets you ask questions in plain English — without sending your notes to any cloud service.

## What you can do with it

- **Capture a thought in seconds.** One command, and your note is timestamped, saved, and committed to git.
- **Find anything, instantly.** Semantic search understands what you meant, not just what you typed. "How did we handle the auth issue last month?" actually finds the right note.
- **Get AI summaries.** Ask journal to summarize your week, surface your open questions, or digest your recent meetings — using Claude, a free cloud model, or a local model running entirely on your machine.
- **Bring your meetings in.** Pull Quill transcripts or any recording into the same search index. Your notes and your meetings, searchable together.
- **Stay on top of your todos.** Drop `@todo` anywhere in a note. journal tracks them, lets you check them off, and even exposes them to Claude so your AI assistant can help.
- **Keep your data yours.** Everything runs locally by default. No account, no server, no subscription. Your notes stay in plain Markdown files in a git repository you own.

## Who this is for

journal is built for developers, but you don't need to be technical to use it once it's set up. If you're comfortable with a terminal and want a better way to capture and retrieve your thinking, journal will feel immediately useful.

The [Installation guide](installation.md) gets you up and running in about ten minutes.

---

> **A note on privacy:** journal indexes your notes locally using [Ollama](https://ollama.com), an open-source tool that runs AI models on your own computer. Your notes never leave your machine for search or indexing. If you use the AI synthesis features, you can choose between cloud providers or keep everything local too. You're always in control.

---

*Looking for contributor or developer documentation? The technical reference lives in the [docs/ folder on GitHub](https://github.com/ericmann/journal/tree/main/docs).*
