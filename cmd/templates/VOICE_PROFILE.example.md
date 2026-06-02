# Voice profile (example)

Copy this to `docs/VOICE_PROFILE.md` **in your own journal repo** and edit it to
describe how you write. `journal synth` reads this file from disk at generation
time (path set by `voice_profile` in `.journal/config.yaml`) and injects it into
the prompt as a *style reference*, so drafts sound like you. It is optional —
synthesis works fine without it.

It is treated as **style only**: the synth prompt explicitly ignores any
meta-instructions here (e.g. "ask which platform"), since the output destination
is fixed. Keep it plain markdown and evolve it over time.

> Your real profile is personal. This repo gitignores `docs/VOICE_PROFILE.md` so
> it never gets committed to the public source tree; in your own (private) notes
> repo, commit it or not as you like.

---

## Tone & stance

- e.g. Direct and concrete; favor plain words over jargon; wry but not flippant.
- Write for a future version of myself skimming in a hurry.

## Sentence & structure

- e.g. Short paragraphs. Lead with the decision, then the why.
- Prefer active voice; cut hedging ("I think", "maybe", "just").

## Vocabulary

- Words/phrases I use: …
- Domain terms to keep verbatim (don't paraphrase): …

## Avoid (anti-patterns)

- e.g. No "delve", "leverage", "in today's fast-paced world", em-dash pile-ups.
- No false enthusiasm or marketing voice; no AI throat-clearing
  ("Certainly!", "Great question!").

## Examples

A couple of short, representative snippets in my own words help the model
calibrate. Paste 2–3 here.
