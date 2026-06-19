# Searching Your Journal

journal's search understands what you *mean*, not just what you typed. This matters because you rarely remember your exact words from six months ago — but you remember the idea.

## Basic search

```sh
journal search "how did we handle the auth token expiry issue"
```

journal embeds your question (using the same local Ollama model that indexed your notes), finds the most similar passages, and returns them with file and line citations.

```
● daily/2026/05/2026-05-14.md:23-27  (score: 0.91)
  ## 14:32 #auth #backend
  Decided to use short-lived JWTs (15 min) with a sliding refresh window.
  The key insight: treat the refresh token as the session, not the JWT itself.
```

The `path:line_start-line_end` reference is clickable in most terminals and editors. You can open that exact section of your notes instantly.

## Getting an AI answer (optional)

If you have `ANTHROPIC_API_KEY` set in your environment, `journal search` also generates a short grounded answer above the raw results — synthesized from the matching notes, not from general knowledge. If the notes don't cover the question, it says so.

```sh
journal search "what did we decide about the auth approach"
# → AI answer: "Based on your notes from May 14, you decided to use short-lived
#   JWTs with a sliding refresh window, treating the refresh token as the session..."
# → raw results follow
```

Pass `--no-answer` to skip the AI summary, or `--answer` to require it (and fail if no key is set). The `--json` flag never includes the AI answer.

## Filtering results

**By tag:**
```sh
journal search "caching strategy" --tag redis
```

**By project:**
```sh
journal search "pricing decision" --project acme-launch
```

**By time:**
```sh
journal search "deployment approach" --since 2w    # last 2 weeks
journal search "auth design" --since 3m            # last 3 months
```

**By number of results:**
```sh
journal search "redis" --k 10    # top 10 results (default is 5)
```

## Other retrieval commands

### Recent notes

```sh
journal recent               # newest notes first
journal recent --tag redis   # filtered
journal recent --since 1w    # last week
```

### Decisions

```sh
journal decisions                          # all your @decision notes
journal decisions --project acme-launch    # just for one project
journal decisions --since 4w               # last month
```

### Project threads

```sh
journal threads            # all active projects with recent activity
journal threads --stale    # projects with no activity in 14+ days
```

### Today's notes

```sh
journal today              # today's notes, open todos, and meetings
journal show               # render today's notes (or pass a date/path)
journal show 2026-05-14    # a specific day
```

## Machine-readable output

Every retrieval command supports `--json` for use in scripts or with AI tools:

```sh
journal search "auth" --json | jq '.results[].path'
journal decisions --json | jq '.results[].snippet'
```

The schema is stable across versions. An empty result set looks like `{"results": []}`, not an error — so you can tell the difference between "found nothing" and "something went wrong."

## Managing tags

`journal tags` lists every distinct `#tag` in your indexed corpus with its usage count — useful for spotting typos and inconsistencies (`#redis` vs `#Redis` vs `#redis-cache`):

```sh
journal tags           # list all tags with usage counts, sorted by frequency
journal tags --json    # machine-readable: {"tags": [{"tag": "redis", "count": 12}, ...]}
```

### Renaming a tag

```sh
journal tags rename redis redis-cache           # rewrite #redis → #redis-cache in all notes
journal tags rename redis redis-cache --dry-run # preview which files would change
```

`rename` rewrites the tag across all matching notes, re-indexes the changed files, and auto-commits — one command to tidy the whole corpus. The leading `#` is optional on both arguments.

---

## How semantic search works (the short version)

When you run `journal index`, each heading block in your notes gets turned into a vector (a list of numbers) that represents its meaning. Your search query gets the same treatment. journal then finds the notes whose vectors are closest to your query's vector — "closest" here means "most similar in meaning."

The model that does this runs entirely on your machine via Ollama. No notes are sent anywhere.
