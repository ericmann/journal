# Usage

The day-to-day guide: how notes are structured, the command surface, search, and
running the indexer continuously. See also
[Configuration](CONFIGURATION.md) · [Synthesis](SYNTHESIS.md) ·
[Remote backup](SYNC.md).

## Working from anywhere

By default journal operates on the repo found by walking up from the current
directory. To capture into (or query) a specific journal **from any directory**,
use the global `--journal-dir` flag or the `JOURNAL_DIR` environment variable
(the flag wins; `~` expands):

```sh
journal capture "idea from a random terminal" --journal-dir ~/Projects/devnotes
export JOURNAL_DIR=~/Projects/devnotes   # then `journal …` works anywhere
alias jc='journal capture --journal-dir ~/Projects/devnotes'
```

It applies to every command (capture, search, index, synth, quill-sync, …) and
accepts the repo root or any subdirectory of it.

## Capture conventions

The daily file is intentionally minimal — an `# YYYY-MM-DD` H1 and a series of
timestamped blocks. Each `journal capture` appends one block:

```markdown
# 2026-06-01

## 09:14 #cabot #litellm
Routing fallback isn't triggering when Qwen OOMs — errors instead of
falling through to cloud.

## 14:02 #displace #canton @decision
Declaring the dev fund payment through Displace as business income.
```

- **`#tags`** → faceted retrieval. Pass `--tags a,b` _or_ just write `#tag`
  inline; both are detected, case-folded, and deduped onto the block header.
  (URL fragments and `[text](#anchor)` links are *not* treated as tags.)
- **`@markers`** → structured annotations the query and synthesis layers key
  off. The recognized set is **`@decision`**, **`@question`**, **`@todo`**.
  Pass `--marker decision` _or_ write `@decision` inline.
- **Heading blocks (`##`)** are the unit of chunking and retrieval.

`--project <name>` routes a capture into `projects/<slug>/notes/YYYY-MM-DD.md`
instead of the daily file, using the same block format (the project name is
slugified, e.g. `"Canton COI"` → `canton-coi`).

With **no text**, `journal capture` opens your editor to compose the note (like
`git commit`), or reads it from **stdin** when input is piped
(`journal capture < note.md`). The editor follows `$JOURNAL_EDITOR`, the `editor`
config key, `$VISUAL`, `$EDITOR`, then `nano`.

Capture appends instantly and, in a git repo, **auto-commits the note** (see
[Auto-commit](#auto-commit)). It does no embedding — run `journal index` (or the
watcher) to make new notes searchable.

## Command surface

```
journal init    [path]                          # bootstrap (or upgrade) a repo
journal capture [text] [--tags a,b] [--project slug] [--marker decision|question|todo]
journal index   [--rebuild] [--since 2w]        # embed changed notes (one-shot)
journal index --watch                           # continuous, debounced re-index
journal search  <query> [--k 5] [--tag t] [--project slug] [--since 2w] [--answer|--no-answer] [--json]
journal recent  [--tag t] [--project slug] [--since 1w] [--json]
journal decisions [--project slug] [--since 4w] [--json]
journal threads [--stale] [--days 14] [--json]
journal todos   [--done|--all] [--project slug] [--since 2w] [--json]
journal done    <path:line | text fragment>       # complete an open @todo
journal dismiss [--project slug] [--before 4w] [--yes] [--resolution text]  # bulk dismiss
journal today   [--json]                          # day at a glance (notes + todos + meetings)
journal show    [date|path]                       # render a day's notes or any note file
journal edit    [date]                            # open a daily file in your editor
journal stats   [--json]                          # volume, streaks, markers, top tags
journal tags    [--json]                          # list #tags with usage counts
journal tags rename <old> <new> [--dry-run]       # rewrite a tag across all notes, re-index, auto-commit
journal tui                                       # interactive dashboard
journal sync    [--dry-run]                      # back up notes to/from the git remote
journal synth   weekly|daily|meetings|decisions|stale [--dry-run] [--write] [--project slug] [--days N] [--date YYYY-MM-DD]
journal quill-sync [--full] [--db path]          # pull Quill meeting transcripts into transcripts/
journal log                                       # toggle mic recording: first press starts, second press stops + processes
journal log     --start|--stop|--cancel|--status  # explicit recording controls (see below)
journal log     --text "..."                      # capture a voice note (typed text)
journal log     <audio.wav>                       # transcribe a WAV → voice note (requires whisper.cpp + model)
journal meetings [--json]                         # recent meeting transcripts, newest first
journal doctor  [--json]                          # health checks
journal mcp     [--repo path]                     # MCP server (stdio) for Claude clients
```

Meeting transcripts (via [Quill](QUILL.md)) are indexed as a separate `transcript`
source. Filter search with `--source notes|transcript|meetings|voice|all`, list them with
`journal meetings`, and digest them with `journal synth meetings`. The MCP server
mirrors this: a `source` param on `search` and a `meetings` tool.

### Voice notes (`journal log`)

`journal log` captures quick voice-style notes. Three entry points:

**Mic recording toggle (bare `journal log`, macOS and Linux):**
```sh
journal log            # first press: starts recording, prints "● recording"
journal log            # second press: stops, finalizes the WAV, processes it in the background
```
The toggle is a lockfile (`$XDG_RUNTIME_DIR/journal-log.lock`, or
`<tmp>/journal-log/journal-log.lock`) holding the recorder's PID, WAV path, and
start time — so a hotkey can bind to the bare command and "do the right
thing" on every press. If the lockfile's PID is no longer running (e.g. the
machine slept mid-recording), the next press cleans it up and starts fresh.
Recording captures the default input device (`log.audio.device`) to a
temporary 16 kHz/mono/16-bit WAV via ffmpeg, using a backend selected by OS:
`avfoundation` on macOS, `pulse` on Linux (the common case — PipeWire ships a
pulse-compatible socket on virtually all modern desktop distros). On
ALSA-only Linux boxes (containers, minimal server installs), set
`log.audio.backend: alsa`. A missing `ffmpeg`, an unsupported platform, or an
unavailable device fails fast on the starting press, before anything is
written. The stop press returns immediately — transcription and landing
happen asynchronously in the background.

Each press also pops a desktop notification (in addition to the terminal
output above), so the toggle is usable from a hotkey without watching a
terminal: starting a recording sends "● recording", and the finalized note
landing (asynchronously, after the stop press) sends "✓ logged: `<title>`"
with the note's path. On macOS, notifications go through `osascript`,
falling back to `terminal-notifier` if it's unavailable; on Linux, through
`notify-send`. If no notifier is available the failure is logged, not
surfaced — a broken notifier never blocks or fails a recording.
See [Hammerspoon hotkey binding](#hammerspoon-hotkey-binding) below to wire
this to a single key press (macOS).

Explicit controls, for scripting or a hotkey that always wants one direction:
- `journal log --start` — start recording; a no-op ("already recording") if one is active.
- `journal log --stop` — stop and process; prints "no active recording" if idle.
- `journal log --cancel` — stop and **discard** the recording (no note is produced).
- `journal log --status` — report idle/recording, elapsed time, and the WAV path.

Safety limits (`.journal/config.yaml`, see [CONFIGURATION.md](CONFIGURATION.md)):
- `log.audio.max_duration` (default 900s) self-finalizes and processes a
  recording that hits the cap — you never lose a long recording to a
  forgotten stop press.
- `log.audio.silence_autostop` (default off) is a safety-net stop after a
  sustained silence interval; it is not the primary stopping mechanism.
  `log.audio.silence_duration` (default 30s) and `log.audio.silence_noise_db`
  (default -35dB) tune how long and how quiet that interval must be.
- `log.audio.keep_wav` (default off) retains the WAV after a successful run
  and records its path in the landed note's `audio:` frontmatter field;
  otherwise the scratch WAV is deleted once the note is safely landed.

Failure handling (a recording never silently vanishes):
- A missing `ffmpeg` binary or an unsupported platform (anything other than
  macOS or Linux) fails fast on the starting press, before anything is
  written. Other recorder failures (mic permission denied by the OS, no
  such device) are caught inside the background recorder — the starting press
  still prints "● recording", but no WAV or note is produced once the failure
  surfaces; `journal log --status` shortly after a press confirms whether a
  recording is actually active.
- Empty/silent recording → the pipeline is skipped and the WAV discarded.
- Transcription error → the WAV is kept and the failure is retryable
  (re-run `journal log <path-to-wav>` once the issue is fixed).
- A second press while a prior recording's pipeline is still processing
  starts a fresh recording once the lock is released (processing doesn't
  block the next capture).

**Typed text (`--text`):**
```sh
journal log --text "reviewed the deploy logs, no anomalies"
```

**Audio file (WAV transcription):**
```sh
journal log note.wav
```
Requires the `whisper.cpp` binary in PATH and a model provisioned via
`journal models pull`. Diarization is off; a small English model is used by
default for fast (~2 s) desk-dictation results. Configure via
`log.transcriber.{backend,model,model_dir}` in `.journal/config.yaml`.

All three entry points run the same pipeline (the recording toggle transcribes
the finalized WAV exactly like the audio-file path, just asynchronously):

1. **Transcribe** (audio path only) — runs `whisper.cpp` locally; no network. A
   missing model fails fast with "run `journal models pull`". An empty/silent
   recording skips the rest of the pipeline. A transcription error is reported and
   retryable — the WAV file is never deleted.
2. **Shape** — the configured synthesis provider cleans disfluencies, generates a title
   and summary, extracts `@todo`/`@decision`/`@question` markers, and tags the note.
   Skipped when `log.shaping.enabled: false`, `local_only: true` with a cloud provider,
   or when no synthesis key is available; the raw text lands instead.
3. **Assemble** — renders a Markdown document with YAML frontmatter
   (`source: voice`, `duration_sec`, `transcriber`, tags, marker counts)
   plus `## Summary`, `## Notes`, and an optional collapsed `## Raw transcript` block.
4. **Land** — writes `logs/YYYY-MM-DD-HHMM-<slug>.md` (configurable via `log.landing.dir`).
5. **Index** — embeds the note as `source=voice`; failure is non-fatal and retryable.

Voice notes are returned by `journal search --source voice` (aliases `log`/`logs`).

#### Hammerspoon hotkey binding

Because the bare `journal log` self-toggles, a single hotkey can start and stop
every recording — the hotkey never needs to know which state it's in. Add to
`~/.hammerspoon/init.lua` (requires [Hammerspoon](https://www.hammerspoon.org/)):

```lua
-- Bind a hotkey to journal's mic-recording toggle. Adjust the modifier keys
-- and the binary path (`which journal`) to match your setup.
hs.hotkey.bind({"cmd", "alt", "ctrl"}, "J", function()
  hs.task.new("/usr/local/bin/journal", nil, {"log"}):start()
end)
```

Every press just runs `journal log`; the lockfile toggle in `internal/audio`
(see above) decides whether that press starts or stops the recording. Desktop
notifications (`osascript`/`terminal-notifier`) confirm each press without
needing a terminal window open.

An optional menubar indicator (not shipped by `journal` — a self-contained
Hammerspoon add-on) can mirror the recording state with a colored dot:

```lua
-- Optional: a small menubar dot that tracks recording state by polling
-- `journal log --status`. Purely cosmetic — the notifications above are
-- the primary feedback loop; skip this if you don't want a persistent
-- menubar item.
local journalDot = hs.menubar.new()
local function refreshJournalDot()
  local _, status = hs.execute("/usr/local/bin/journal log --status")
  if status and status:match("^recording") then
    journalDot:setTitle("●") -- recording
  else
    journalDot:setTitle("○") -- idle
  end
end
hs.timer.doEvery(5, refreshJournalDot)
refreshJournalDot()
```

This polling menubar dot is a convenience layer only — it is not a persistent
daemon `journal` ships or manages; `journal log` remains a single static
binary with no background service beyond the detached per-recording process
started by the toggle itself.

`journal mcp` runs an MCP server (stdio) exposing 15 tools — `search`, `recent`,
`decisions`, `threads`, `show`, `capture`, `journal_log_text`, `journal_log_audio`,
`meetings`, `todos`, `done`, `stats`, `today`, `ask`, `synth` — plus read resources
(`journal://today`, `journal://recent`, `journal://projects/{slug}/index`) and
pre-built prompts (`weekly-reflection`, `decisions-review`, `project-status`). See
[INTEGRATIONS.md](INTEGRATIONS.md) §3b for the one-block config. `journal_log_text`
and `journal_log_audio` run the same shape→assemble→land→index core as `journal log
--text` / `journal log <audio.wav>` — the mic-recording stage stays CLI-only and is
never exposed over MCP. Synthesis is documented in [SYNTHESIS.md](SYNTHESIS.md);
backup in [SYNC.md](SYNC.md).

## Retrieval & queries

- **`search`** embeds your query (with a retrieval instruction), runs a
  brute-force vector KNN over the index, optionally reranks the top candidates
  (if a `reranker` is configured), and returns the best `--k` with
  `path:line_start-line_end` citations. With reranking off, results are in
  vector-distance order.
- **`recent`** / **`decisions`** are plain metadata queries (newest first);
  `decisions` filters to `@decision` blocks.
- **`threads`** summarizes project activity; `--stale` surfaces projects with no
  activity in `--days` (default 14).

**AI answer (key-gated).** When `ANTHROPIC_API_KEY` is set, `search` also generates
a short, grounded answer to your question with the configured `synth_model` and
prints it (formatted) **above** the raw hits — the raw results are always kept. It
answers only from the retrieved notes and says so when they don't cover the
question. It's automatic when a key is present; `--no-answer` skips it, `--answer`
forces it (and errors if no key). `--json` never includes the answer. The answer
is rendered richly on a terminal and as plain markdown when piped.

Every read command supports **`--json`** with a stable schema:

```json
{ "results": [ { "path": "daily/2026/06/2026-06-01.md", "line_start": 3,
  "line_end": 5, "heading": "09:14 #cabot", "snippet": "…", "score": 0.87,
  "tags": ["cabot"], "markers": [] } ] }
```

`threads --json` emits `{ "threads": [ { "project", "last_activity", "chunks",
"open_questions", "stale", "days_since" } ] }`. On failure, commands emit
`{ "error": "…" }` to stdout and exit non-zero — so an empty result set is
distinguishable from an error.

## Todos

Any `@todo` in a captured note becomes a tracked item once indexed (the watcher
does this live; otherwise run `journal index`):

```sh
journal capture "follow up with bob on pricing @todo"
journal todos                       # numbered list with path:line citations
journal done "follow up with bob"   # by unique text fragment…
journal done daily/2026/06/2026-06-09.md:12   # …or by citation
journal todos --done                # what you've finished
```

Completing a todo rewrites that one `@todo` token to `@done YYYY-MM-DD` in the
note file (the content is otherwise untouched — it's the markdown equivalent of
checking a checkbox), re-indexes, and auto-commits. The same `todos`/`done`
operations are exposed as MCP tools, so a connected Claude can check items off.

### Bulk dismissal

When a batch of todos becomes irrelevant at once (a project wraps up, a planning
doc is superseded, older items pile up), `journal dismiss` clears them all in a
single command and a single git commit:

```sh
# Dismiss all open todos in a project (prompts for confirmation):
journal dismiss --project acme

# Dismiss todos older than 4 weeks (interactive confirm):
journal dismiss --before 4w

# Combine filters; skip prompt with --yes:
journal dismiss --project janus --before 2w --yes

# Attach a resolution note to every dismissed todo:
journal dismiss --project acme --resolution "superseded by new plan" --yes

# --older-than is an alias for --before:
journal dismiss --older-than 4w --yes
```

`dismiss` shows the matched set before mutating anything and requires explicit
confirmation (type `y` at the prompt, or pass `--yes`). Each `@todo` is rewritten
to `@done YYYY-MM-DD` exactly as `journal done` does. Re-indexing is non-fatal —
the notes are always safe on disk. All rewrites land in **one** git commit whose
message records the filter used (e.g. `dismissed 5 todo(s) (project=acme)`).

At least one filter (`--project` or `--before`/`--older-than`) is required.

## Tags

`journal tags` lists every distinct `#tag` in the indexed corpus with its usage count — useful
for auditing consistency (`#redis` vs `#Redis`) and deciding which tags to consolidate:

```sh
journal tags           # sorted by frequency
journal tags --json    # {"tags": [{"tag": "redis", "count": 12}, ...]}
```

`journal tags rename` rewrites a tag across all notes, re-indexes the changed files, and
auto-commits:

```sh
journal tags rename redis redis-cache        # #redis → #redis-cache in all notes
journal tags rename redis redis-cache --dry-run   # preview: which files would change?
```

The leading `#` is optional on both arguments. The rename is atomic from the user's
perspective: all files are rewritten before re-indexing begins; a re-index failure
is non-fatal (the watcher or a subsequent `journal index` will catch up).

## Reading your notes

```sh
journal today              # day at a glance: today's notes + open todos + meetings
journal show               # render today's notes (glamour on a TTY, plain piped)
journal show yesterday     # or YYYY-MM-DD, or any repo-relative path
journal edit               # open today's daily file in your editor (created if needed)
journal edit 2026-06-08    # touch up a past day; auto-commits when you close
```

`journal today` aggregates **all note chunks captured today** across every
location — the catch-all daily file (`daily/YYYY/MM/YYYY-MM-DD.md`),
per-project notes (`projects/<slug>/notes/YYYY-MM-DD.md`), and any other
note indexed for the day. Project note sections are labelled with their
source path so you can tell them apart. Meeting transcripts continue to
appear under the **Today's meetings** section rather than Notes. The
`journal://today` MCP resource still corresponds to the literal daily file;
the aggregated view is the `today` MCP *tool*.

## Stats

`journal stats` reports capture volume (chunks by source, projects, meetings),
**momentum** (current/longest daily streak, this week vs last), marker counts
(open/done todos, decisions, questions), and your top tags. `--json` emits a
stable schema.

## The TUI dashboard

`journal tui` is a full-screen interactive view over everything above:

| Tab | Contents | Keys |
| --- | --- | --- |
| Today | rendered daily notes | `r` refresh |
| Todos | open todos | **`d` complete selected**, `enter` open |
| Search | semantic search | type + `enter`; `/` edits the query |
| Recent / Meetings | latest notes & transcripts | `enter` open, `esc` back |
| Stats | the stats report | |

`tab`/`shift+tab` or `1–6` switch tabs; `q` quits. Search errors (e.g. Ollama
down) land in the status bar — the dashboard keeps working.

## The watcher

Indexing stays fresh via a long-running `journal index --watch`: it does an
initial index, then debounces filesystem events and re-indexes only changed files
(deletions remove their chunks). Ctrl-C stops it cleanly.

The **recommended way to run it is a dedicated `tmux` pane** — simple, visible,
and easy to restart:

```sh
tmux new-session -d -s journal 'cd ~/journal && journal index --watch'
tmux attach -t journal     # watch it;  Ctrl-b d to detach
```

### Unattended — launchd (macOS)

To survive logout/reboot, install a per-user launchd agent at
`~/Library/LaunchAgents/com.ericmann.journal-watch.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>            <string>com.ericmann.journal-watch</string>
  <key>ProgramArguments</key> <array>
    <string>/usr/local/bin/journal</string>
    <string>index</string>
    <string>--watch</string>
  </array>
  <key>WorkingDirectory</key> <string>/Users/you/journal</string>
  <key>RunAtLoad</key>        <true/>
  <key>KeepAlive</key>        <true/>
  <key>StandardOutPath</key>  <string>/tmp/journal-watch.log</string>
  <key>StandardErrorPath</key><string>/tmp/journal-watch.log</string>
</dict>
</plist>
```

```sh
launchctl load   ~/Library/LaunchAgents/com.ericmann.journal-watch.plist
launchctl unload ~/Library/LaunchAgents/com.ericmann.journal-watch.plist   # to stop
```

### Unattended — systemd (Linux)

A per-user service at `~/.config/systemd/user/journal-watch.service`:

```ini
[Unit]
Description=journal watcher (re-index notes on change)

[Service]
Type=simple
WorkingDirectory=%h/notes                  # your journal repo
ExecStart=/usr/local/bin/journal index --watch
Restart=on-failure

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now journal-watch.service
loginctl enable-linger "$USER"   # keep it running without an active login (servers/headless)
journalctl --user -u journal-watch -f   # tail its logs
```

User services run with a **minimal PATH**, so use an absolute `ExecStart`. For the
periodic backup (`journal sync`), a systemd **timer** is the native alternative to
cron — see [SYNC.md](SYNC.md).

## Auto-commit

When the repo is a git repository, `journal capture`, `journal index`, and
`index --watch` all **auto-commit your note changes** (controlled by
`git_autocommit`, default on). **Capture commits the note immediately** — so your
words are committed the moment you capture them, watcher or not. The watcher /
`index` then keep the search index fresh and also commit any direct file edits.
You can't forget. Details:

- It commits **only your markdown** (`git add -A` honors `.gitignore`, so the
  vector index is never committed).
- It's a **no-op unless the repo root is a git top level** — it never commits your
  notes into a parent repository, and does nothing if git isn't installed or the
  folder isn't a repo.
- Commits are **unsigned by default** (`git_autocommit_sign: false`) so an
  unattended watcher doesn't trigger a signing prompt per note; set it `true` for
  signed note-commits.
- Commit failures are **logged, never fatal** — your markdown is always safe on
  disk. Messages are auto-generated, e.g.
  `📓 scribbled notes — +1 new, ~1 revised, -0 removed · Mon 2026-06-01 12:32`.

Set `git_autocommit: false` to manage commits yourself. To get notes *off* the
machine to a remote, see [Remote backup](SYNC.md).
