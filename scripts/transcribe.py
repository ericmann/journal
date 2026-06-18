#!/usr/bin/env python3
"""Transcribe an audio/video recording to WhisperX JSON for `journal transcribe`.

This is the one heavy step the journal binary deliberately does NOT bundle (it
needs Python + WhisperX + ffmpeg + a Hugging Face token). It wraps WhisperX with
sane defaults and, importantly, reads the HF token from the environment so it
never lands in your shell history or a committed command.

    export HF_TOKEN=hf_xxx                      # do NOT type the token inline
    python scripts/transcribe.py meeting.mp4 --min-speakers 3 --max-speakers 12
    # -> ./out/meeting.json
    journal transcribe ./out/meeting.json --title "WPVIP CAB" --date 2026-06-02

See docs/TRANSCRIBE.md for setup, gated-model acceptance, and troubleshooting.
"""
import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(1)


def main() -> None:
    ap = argparse.ArgumentParser(description="Audio/video -> WhisperX JSON for journal.")
    ap.add_argument("input", help="path to the recording (mp4, mov, m4a, wav, …)")
    ap.add_argument("--outdir", default="./out", help="directory for the JSON output (default ./out)")
    ap.add_argument("--model", default="large-v3", help="whisper model (default large-v3)")
    ap.add_argument("--language", default="en", help="language code (default en)")
    ap.add_argument("--min-speakers", type=int, default=None, help="diarization: minimum speakers")
    ap.add_argument("--max-speakers", type=int, default=None, help="diarization: maximum speakers")
    ap.add_argument("--device", default="cpu", help="cpu or cuda (default cpu)")
    ap.add_argument("--compute-type", default="int8", help="int8 (cpu) | float16 (gpu) (default int8)")
    ap.add_argument("--no-diarize", action="store_true", help="skip speaker diarization (no HF token needed)")
    args = ap.parse_args()

    if shutil.which("whisperx") is None:
        die("whisperx not found on PATH — see docs/TRANSCRIBE.md (pip install whisperx in a venv).")
    if shutil.which("ffmpeg") is None:
        die("ffmpeg not found on PATH — WhisperX needs it to decode audio. `brew install ffmpeg`.")
    if not Path(args.input).is_file():
        die(f"input not found: {args.input}")

    token = os.environ.get("HF_TOKEN") or os.environ.get("HUGGING_FACE_HUB_TOKEN")
    if not args.no_diarize and not token:
        die(
            "diarization needs a Hugging Face token, but HF_TOKEN is not set.\n"
            "  1. create a read token: https://huggingface.co/settings/tokens\n"
            "  2. accept the gated models (one-time, while logged in):\n"
            "       https://huggingface.co/pyannote/segmentation-3.0\n"
            "       https://huggingface.co/pyannote/speaker-diarization-3.1\n"
            "  3. export HF_TOKEN=hf_...   (then re-run; or pass --no-diarize to skip speakers)"
        )

    Path(args.outdir).mkdir(parents=True, exist_ok=True)
    cmd = [
        "whisperx", args.input,
        "--model", args.model,
        "--language", args.language,
        "--output_format", "json",
        "--output_dir", args.outdir,
        "--device", args.device,
        "--compute_type", args.compute_type,
    ]
    if not args.no_diarize:
        cmd += ["--diarize", "--hf_token", token]  # from env, not the shell you typed
        if args.min_speakers is not None:
            cmd += ["--min_speakers", str(args.min_speakers)]
        if args.max_speakers is not None:
            cmd += ["--max_speakers", str(args.max_speakers)]

    # Print the command WITHOUT the token so logs/CI stay clean.
    printable = [("hf_***" if c == token else c) for c in cmd]
    print("running:", " ".join(printable), file=sys.stderr)
    # WhisperX is slow on CPU (often slower than real time) and prints its own
    # progress; stream it straight through.
    rc = subprocess.call(cmd)
    if rc != 0:
        die(f"whisperx exited {rc} — see docs/TRANSCRIBE.md troubleshooting.")

    stem = Path(args.input).stem
    out_json = Path(args.outdir) / f"{stem}.json"
    print(f"\n✓ {out_json}\n  next: journal transcribe {out_json} --title \"<meeting>\" --date <YYYY-MM-DD>")


if __name__ == "__main__":
    main()
