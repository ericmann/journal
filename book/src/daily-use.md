# A Day in the Life

Here's what a typical day with journal looks like — from morning standup to end-of-day wrap.

---

## Morning: get your bearings

Start the day by checking what's on your plate:

```sh
journal today
```

This shows today's notes (empty at the start of the day), your open todos, and any meetings that were indexed overnight. It's a quick 10-second pulse check. The notes section aggregates **everything captured today** — the daily note, any per-project notes (`journal capture --project foo …`), and more — so you see the full picture regardless of where each note landed.

If you run the index watcher, it's already running in the background from yesterday. If not, kick it off now:

```sh
journal index --watch &    # or in a dedicated terminal tab
```

---

## During the day: capture as you go

The goal is to capture with as little friction as possible. Don't format, don't organize — just get it out:

```sh
journal capture "standup: blocked on auth PR review, picking up #auth after"
journal capture "discovered the redis timeout is 5s not 500ms, explains the latency spikes #redis @decision"
journal capture "need to follow up with Sarah about the API contract @todo #acme-launch"
```

Each capture is timestamped, committed to git, and (if the watcher is running) searchable within seconds.

When something needs more thought, open your editor:

```sh
journal capture   # opens $EDITOR, write freely, save and close
```

---

## Midday: find something from last week

You half-remember something about a caching approach from a few weeks ago:

```sh
journal search "caching strategy for the API layer"
```

journal finds the right notes even if you used different words at the time. The results come with `file:line` citations you can open directly.

If you have a synthesis provider configured, you'll also get a short AI answer that synthesizes the relevant notes for you.

Need to see all your open action items?

```sh
journal todos
```

Mark one done:

```sh
journal done "follow up with Sarah"
```

---

## End of day: review and synthesize

Check what you captured:

```sh
journal today
```

If you have synthesis configured, generate a daily digest:

```sh
journal synth daily --write
```

This writes a summary to `reflections/daily-YYYY-MM-DD.md` — useful for standups, weekly reviews, or just reminding yourself what you accomplished.

---

## Interactive mode

Prefer a terminal UI over individual commands? `journal tui` gives you an interactive dashboard:

```sh
journal tui
```

The TUI has tabs for:

- **Today** — your daily notes, rendered
- **Todos** — open action items; press `d` to complete the selected one
- **Search** — type a query, hit enter; results update in real time
- **Recent / Meetings** — latest notes and transcripts
- **Stats** — capture volume, streaks, tag breakdown

Use `tab` / `shift+tab` or `1–6` to switch tabs; `q` to quit.

---

## Weekly rhythm

At the end of the week:

```sh
journal synth weekly --write
```

This generates a digest of the whole week at `reflections/YYYY-Www.md`. Over time you'll accumulate a searchable archive of weekly summaries — a natural project history.

Check stale projects:

```sh
journal threads --stale
```

This surfaces anything you haven't touched in two weeks, so you don't lose track of things you meant to come back to.

---

## Stats

```sh
journal stats
```

Shows capture volume, your current and longest streaks, marker counts (open todos, decisions, questions), and your top tags. Satisfying to look at on a Friday.
