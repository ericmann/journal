# What is journal?

journal is a tool for capturing, searching, and making sense of your notes — all on your own computer, without a cloud service in the middle.

## The basic idea

Your notes live as plain Markdown files in a git repository. When you run `journal capture`, it adds a timestamped entry to today's file and commits it. Your history is always safe in git.

The clever part is retrieval. journal uses a technique called **semantic search** (powered by a local AI model via [Ollama](https://ollama.com)) to understand what your notes *mean*, not just the exact words they contain. When you search for "the auth issue from last month," you'll find the right entry even if you used different words when you wrote it.

## Local-first means your data stays yours

journal is designed around a simple principle: your notes are yours. The search index is built entirely on your machine. No notes are uploaded to build it, and no account is required to use it.

The vector index (the database that powers semantic search) is a disposable cache — you can delete and rebuild it at any time from your Markdown files. The Markdown files are the source of truth.

## Optional AI synthesis

If you want more than search, journal can generate summaries and digests using a language model. You have three options:

- **Cloud Claude** (the default): great quality, requires an Anthropic API key
- **OpenAI-compatible endpoint** (OpenRouter, Groq, etc.): flexible, often cheaper
- **Local Ollama model**: zero egress, fully offline, no API key needed

You can switch providers at any time in the config file. If you skip synthesis entirely, journal still works great — you just get search without AI-generated summaries.

## Just a binary

journal ships as a single static binary. No runtime, no daemon, no Docker. Install it, point it at a folder, run `journal init`, and you're ready.

It works from the command line — pair it with any text editor you like, or use `journal tui` for a built-in interactive dashboard.

## In a sentence

journal turns a folder of Markdown notes into a searchable knowledge base with optional AI synthesis, all running locally on your machine.
