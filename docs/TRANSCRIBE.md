# Transcribing non-Quill recordings

[Quill](QUILL.md) meetings flow in automatically via `journal quill-sync`. For
anything else — a P2 video, a Zoom/Meet recording, a voice memo — this is the
manual path: **recording → WhisperX JSON → `journal transcribe` → indexed
transcript with an AI summary**, all retrievable like any other note.

It's a two-command pipeline once set up:

```sh
export HF_TOKEN=hf_...                                   # never type this inline; see below
python scripts/transcribe.py meeting.mp4 --min-speakers 3 --max-speakers 12
journal transcribe ./out/meeting.json --title "Acme Q2 Planning" --date 2026-06-02
```

The first command is the heavy ML step (slow, Python, GPU-optional); the second
is the journal binary doing what it's good at — rendering, **summarizing**, and
indexing. The summary is the important part for retrieval: a 2-hour transcript
is hundreds of chunks, so `journal transcribe` generates a `## Notes` section at
the top (via your configured `synth_provider`) that search hits first, instead
of forcing agents to trawl the whole meeting. This mirrors how Quill transcripts
carry their AI notes.

---

## One-time setup

### 1. ffmpeg

WhisperX decodes audio with it. `brew install ffmpeg` (macOS) / your package
manager elsewhere.

> **Harmless `torchcodec`/`libtorchcodec` warning on ffmpeg 8.** pyannote 4.x
> imports `torchcodec`, which only links against ffmpeg **4–7**; with Homebrew's
> current **ffmpeg 8** (libavutil 60) you'll see a multi-line
> `torchcodec is not installed correctly … built-in audio decoding will fail`
> warning at startup. **It's noise, not a blocker** — WhisperX decodes audio via
> its own ffmpeg subprocess, so transcription *and* diarization both run fine.
> Ignore it. (If you really want it gone: `brew install ffmpeg@7` and run with
> `DYLD_FALLBACK_LIBRARY_PATH=/opt/homebrew/opt/ffmpeg@7/lib` — but you don't need
> to.)

### 2. A Python venv with WhisperX

Use a **fresh virtualenv on Python 3.11** (3.10–3.13 also work; **3.14 has no
compatible WhisperX yet** — pip falls back to an ancient release that pins a
long-gone `ctranslate2` and the install dead-ends). A clean env also avoids
conflicts with system packages:

```sh
python3.11 -m venv ~/.venvs/whisperx && source ~/.venvs/whisperx/bin/activate
pip install -r scripts/requirements.txt
```

The transitive stack (torch, ctranslate2/faster-whisper, pyannote) is heavy and
version-sensitive. If `pip` resolves a broken set, see [Troubleshooting](#troubleshooting).

### 3. A Hugging Face token — set it in the environment, never inline

Diarization (speaker labels) uses pyannote models that require a token. **Do not
put the token on the command line** — it leaks into your shell history (and into
anything you paste). Create a *read* token at
<https://huggingface.co/settings/tokens> and export it:

```sh
export HF_TOKEN=hf_...        # add to your shell profile, or use `huggingface-cli login`
```

`scripts/transcribe.py` reads `HF_TOKEN` from the environment and never prints it.

### 4. Accept the gated diarization model (one-time)

This is the step that silently fails if skipped — pyannote's model is **gated**.
While logged into Hugging Face, click "Agree" on:

- <https://huggingface.co/pyannote/speaker-diarization-community-1>

That single repo is all current WhisperX (3.8.x / pyannote.audio 4.x) needs: the
`speaker-diarization-community-1` pipeline is self-contained — it bundles its own
segmentation and embedding models, so there's nothing else to accept. Without
accepting it, diarization 401s/403s even with a valid token.

> **If you're on an older WhisperX (3.x / pyannote 3.x)** the diarizer was
> `pyannote/speaker-diarization-3.1`, which *also* required accepting
> `pyannote/segmentation-3.0`. A fresh `pip install -r scripts/requirements.txt`
> today gives you 3.8.x, so you only need the one repo above. The voice-activity
> step is **never** gated — WhisperX ships those weights inside the package.

---

## Step 1 — recording → WhisperX JSON

```sh
python scripts/transcribe.py meeting.mp4 --min-speakers 3 --max-speakers 12
```

- Takes any ffmpeg-decodable input (mp4, mov, m4a, wav, …) — no separate audio
  extraction needed.
- Defaults: `large-v3`, English, CPU, int8, diarized. Override with `--model`,
  `--language`, `--device cuda --compute-type float16` (GPU), or `--no-diarize`
  (no speakers, no HF token needed).
- **It's slow** — often slower than real time on CPU; a 2-hour meeting can take
  a while. WhisperX streams its own progress.
- Output: `./out/<name>.json` (override with `--outdir`).

## Step 2 — JSON → indexed transcript

```sh
journal transcribe ./out/meeting.json --title "Acme Q2 Planning" --date 2026-06-02
```

This renders speaker-labeled, timestamped Markdown into your `transcripts/`
landing zone (frontmatter + `# Title` + `## Notes` + `## Transcript`, stamped
`source: whisperx`), dates the file to the meeting, generates the `## Notes`
summary with your `synth_provider`, and indexes it — immediately searchable.

- `--title` defaults to the filename; `--date` (YYYY-MM-DD) defaults to today and
  sets the transcript's timestamp (so `journal recent`/`stats` place it correctly).
- `--no-summary` skips the AI summary (e.g. if you have no synth provider set up).
- The summary uses whatever `synth_provider` you've configured — local Ollama,
  Anthropic, or an OpenAI-compatible endpoint like OpenRouter (see
  [SYNTHESIS.md](SYNTHESIS.md)). If it's unavailable, the transcript still
  ingests, just without the summary.

> **Long transcripts + local models:** a multi-hour transcript can exceed a local
> model's context window (Ollama silently truncates past `synth_num_ctx`), so the
> summary may only cover part of the meeting. For best results on long meetings,
> summarize with a large-context cloud provider (Claude, or an OpenRouter model),
> or raise `synth_num_ctx`.

---

## Troubleshooting

- **`401`/`403`, "gated repo", or "could not download pyannote model"** — you
  skipped step 4 (accept `pyannote/speaker-diarization-community-1`) or `HF_TOKEN`
  is unset/invalid. This surfaces **after** transcription, when diarization
  starts — a successful transcript followed by a gated-repo error is this, not a
  transcription failure.
- **`torchcodec`/`libtorchcodec … built-in audio decoding will fail`** — harmless
  with ffmpeg 8; ignore it (see [§1 ffmpeg](#1-ffmpeg)). It is *not* why a run
  fails — look for the gated-repo error above instead.
- **`No matching distribution … ctranslate2` / "could not find a version"** — your
  Python is too new (3.14+). WhisperX caps at `<3.14`, so pip falls back to a
  prehistoric release with dead pins. Use a 3.11 (or 3.10–3.13) venv.
- **pip dependency conflicts** — start from a clean Python 3.11 venv; don't
  install into a system or shared env. Once a working set resolves, `pip freeze >
  scripts/requirements.lock.txt` so it's reproducible.
- **Diarization is wrong/over-splits** — tune `--min-speakers`/`--max-speakers`
  to the real attendee count.
- **`whisperx: command not found`** — your venv isn't activated, or install
  didn't complete.
- **No `## Notes` in the output** — your synth provider wasn't reachable; the
  transcript still indexed. Re-run with a working `synth_provider`, or accept
  transcript-only search.
