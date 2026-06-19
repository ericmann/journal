# Your First Day

You've installed journal and Ollama. Let's get your first journal set up and capture something in it.

## Initialize a journal

Navigate to the folder where you want to keep your notes, then run:

```sh
journal init
```

This scaffolds a few directories and a config file:

```
.journal/config.yaml    your settings (committed to git)
.journal/index/         the search index (gitignored — rebuilt from your notes anytime)
daily/                  your daily notes land here
projects/               long-running threads go here
reflections/            AI synthesis output
```

journal also creates a `.gitignore` that excludes the search index — it's a cache, not source of truth.

> If you already have a git repository in this folder, that's fine. `journal init` works in existing repos too.

## Capture your first note

```sh
journal capture "Set up journal — this is going to be useful"
```

That's it. journal creates a file at `daily/YYYY/MM/YYYY-MM-DD.md` (today's date), appends a timestamped entry, and commits it to git. You didn't have to open a file, format anything, or remember where you put it.

Want to write something longer? Leave off the text and your editor opens:

```sh
journal capture
```

Journal follows `$EDITOR`, or falls back to nano if none is set.

## Build the search index

Before you can search, journal needs to embed your notes:

```sh
journal index
```

This runs through your notes and builds the local search index using Ollama. It only processes notes that are new or changed since the last index run.

## Search

Now try a search:

```sh
journal search "what did I set up today"
```

Journal embeds your query and finds the most relevant notes — semantically, not just by keyword. You'll see the matching entry, the file it came from, and the exact lines.

## See today's notes at a glance

```sh
journal today
```

This shows everything you've captured today, your open todos, and any meetings. A useful command to run at the start or end of your day.

---

## Keep the index fresh

As you capture more notes, you'll want the search index to stay current. The easiest way is to run the watcher in a background terminal:

```sh
journal index --watch
```

This stays running, notices when your notes change, and re-indexes them automatically. See [Capturing Notes](capturing-notes.md) for more on the watcher and auto-indexing options.

---

You're up and running. The next sections walk through each feature in more detail — but honestly, `journal capture` and `journal search` will cover 80% of your daily use.
