# Meetings & Transcripts

journal can bring your meeting transcripts into the same search index as your notes. Once they're there, you can search across everything — your captured thoughts and your meeting discussions — in one place.

## Two ways to add meeting transcripts

### Option 1: Quill (macOS and Windows)

[Quill](https://www.quillmeetings.com) records your meetings and stores everything in a local SQLite database. journal reads that database and renders your meetings to Markdown:

```sh
journal quill-sync       # pull new meetings into transcripts/
journal index            # embed them (or let `journal index --watch` do it automatically)
```

Quill is only available on macOS and Windows. If you're on Linux, use the manual transcript path below.

### Option 2: Any recording (via WhisperX)

For Zoom calls, recorded presentations, voice memos — anything you have as an audio or video file — journal can transcribe and ingest them using [WhisperX](https://github.com/m-bain/whisperX):

```sh
# Step 1: transcribe with WhisperX (one-time Python setup required)
python scripts/transcribe.py meeting.mp4 --min-speakers 2 --max-speakers 8

# Step 2: ingest into journal
journal transcribe ./out/meeting.json --title "Q2 Planning" --date 2026-06-02
```

`journal transcribe` renders the transcript to Markdown, generates an AI summary at the top (using your configured synthesis provider), and indexes it immediately.

The summary is important: a two-hour meeting is hundreds of search chunks, so the AI notes at the top are what search hits first, instead of making you trawl the full transcript.

### Option 3: Drop in a .qm file

If you have a Quill `.qm` export file, drop it into your `transcripts/` folder. With `quill.accept_qm_imports: true` (the default), the watcher picks it up and renders it automatically. This works on Linux too.

---

## Using your meeting transcripts

Once synced and indexed, your transcripts are searchable like any other note:

```sh
journal search "what did we decide about the pricing model" --source transcript
journal search "anything about deployment" --source all      # notes + transcripts
```

**List recent meetings:**

```sh
journal meetings
```

**Get an AI digest of the last week of meetings:**

```sh
journal synth meetings --write
```

**View a specific meeting:**

```sh
journal show transcripts/2026-06-02-q2-planning.md
```

---

## Keeping transcripts fresh

Set up a schedule to run `quill-sync` regularly. If you're also running `journal index --watch`, the watcher embeds newly-synced transcripts automatically:

```sh
# In cron: sync Quill meetings every hour, then re-index
0 * * * * /usr/local/bin/journal quill-sync && /usr/local/bin/journal index
```

If the watcher is already running, you only need to schedule `quill-sync`.

---

## WhisperX setup (one-time)

WhisperX requires Python 3.11 and ffmpeg. Set up a virtual environment:

```sh
brew install ffmpeg                              # macOS; or your package manager
python3.11 -m venv ~/.venvs/whisperx
source ~/.venvs/whisperx/bin/activate
pip install -r scripts/requirements.txt          # in your journal source directory
```

You'll also need a free [Hugging Face](https://huggingface.co/settings/tokens) token (for speaker diarization), and you'll need to accept the terms for two gated models:

- https://huggingface.co/pyannote/segmentation-3.0
- https://huggingface.co/pyannote/speaker-diarization-3.1

Export the token in your shell profile: `export HF_TOKEN=hf_...`

Transcription is the slow step — a two-hour meeting can take a while on CPU. On a GPU-equipped machine it's much faster.
