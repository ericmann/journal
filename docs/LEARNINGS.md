# Learnings

Institutional memory for agent-assisted development with this template. **Read this
before implementing.** Each entry is: **Symptom** (what went wrong / was observed) →
**Root cause** → **Guardrail** (the durable change that prevents the class — a config
rule, a test, or a workflow). When a guardrail is a *test*, the lesson is enforced
automatically and can't silently regress; prefer that over prose where possible.

> Append-only. Add new entries at the top. Don't delete entries — if a lesson is
> superseded, add a new one noting that. These were captured from real runs while
> dogfooding this template; keep the project-specific ones in your own fork.

---

## Transient SIGTRAP runner crashes are a known Ollama/Metal flake — size retries to a model reload

**Symptom:** `journal index` (or `transcribe`) aborts with "after 3 retries: ollama /api/embed:
status 400: EOF" on Apple Silicon, even though re-running immediately succeeds.

**Root cause:** The embedding runner (`llama-server`) is killed by `signal: trace/BPT trap`
(SIGTRAP) — a known transient llama.cpp/Metal crash, not OOM and not a journal bug. Ollama
auto-restarts the runner, but the restart + model reload takes seconds to tens of seconds. The
original 3-attempt / ~1.4 s retry window exhausts before the reload completes.

**Guardrail:** `internal/embed` retries transient embed-runner crashes (400 with `EOF` /
`do embedding request` body) across a 45 s wall-clock budget (`transientBudget` field), with
capped exponential backoff (n²×100 ms, cap 5 s, ±25% jitter). Transport dial failures
(`ErrUnreachable`) and non-retryable 4xx still fail fast. Tests use a shrunk budget so
`TestEmbed_RetryWindowDuration` and `TestEmbed_ExhaustedRetries` remain quick.

## Feature PRs ship code + tests but skip the doc surface — name every doc target in "done"

**Symptom:** The optional **reranker** landed (`#21` added support; `#36` improved quality and
recommended `qwen3:4b`) but the user-facing "going fully local" chapter
(`book/src/local-only.md`) never mentions it, and the README only says "optional LLM reranking"
with no setup guidance. A wider sweep of the v2.7.0 features found the same drift: `journal tags`
(`#33`) is undocumented *anywhere*; the README and the Claude Desktop/Code integration chapters
still enumerate ~6 of the now-13 MCP tools (missing `today`, `stats`, `ask`, `synth`, `done`,
`show`, `capture`); MCP **resources** (`#37`) and **prompts** (`#44`) are absent from user docs.
All of it passed review with green CI.

**Root cause:** "Done" is gated on code + tests + CI, but docs live across **three** surfaces with
no single owner — the README (feature bullets *and* the MCP tool list), the `docs/*.md` reference
set, and the mdBook `book/src/` user narrative. An issue's acceptance criteria and an agent's edits
target the code path (and at most one doc file); nothing *tests* doc coverage, so CI stays green
while docs fall behind as features accrete. Cross-cutting features (a new MCP tool, a new command,
a new local-only knob) are hit hardest because their docs are spread across files none of which the
PR obviously "owns."

**Guardrail:** Treat documentation as part of the contract. Every feature issue's acceptance
criteria must **enumerate the doc targets** it touches — README (incl. the MCP tool list), the
relevant `docs/*.md`, the matching `book/src/` chapter, and a CHANGELOG entry — and the PR template
carries a "Docs updated: …" line. For the MCP surface, add the test-style guardrail that can't
silently regress: a unit test asserting every tool/resource/prompt registered in `cmd/mcp.go`
appears in the README tool list and the integration chapter. `#21`/`#36` is the worked example — a
feature can be fully built, tested, and merged and still be invisible to users.

---

## Test HTTP handlers: bare `w.Write(...)` fails `errcheck` — always discard the return with `_, _ =`

**Symptom:** An agent PR introduces `httptest.NewServer` handlers with bare `w.Write(...)` calls.
All behavioral tests pass and `go vet` is clean, but `golangci-lint` fails with `errcheck` errors
on the write calls — requiring a fix-up commit after the owner notices the CI failure.

**Root cause:** `http.ResponseWriter.Write()` returns `(int, error)`. The `errcheck` linter requires
every returned error to be explicitly checked or discarded. Bare `w.Write(...)` (no assignment, no
`_`) fails `errcheck` even though `go vet` and `go test` both pass. Agents writing tests without
being able to run `make lint` locally will consistently miss this — it is invisible until CI.

**Guardrail:** In any `httptest.NewServer` handler, write `_, _ = w.Write(...)` — never bare
`w.Write(...)`. Added to CLAUDE.md "Conventions to follow" so agents learn the pattern before
writing new test code. No automated test is feasible for a lint rule; the rule in CLAUDE.md is
the mechanism.

---

## Headless CI must force `bypassPermissions` via `claude_args` — not `acceptEdits`, not `settings:` alone

**Symptom:** An `agent-ready` issue triggers a run that reports "success" but opens **no PR**.
The agent created a branch, then its `Write`/`Bash` calls were denied and it ended by asking a
(nonexistent) human to approve a tool. Sometimes it retried through; sometimes it bailed —
non-deterministic.

**Root cause:** `claude-code-action` defaults to interactive permission mode. `acceptEdits`
does not cleanly auto-approve the *first* `Write`, so the agent hits a denial and its recovery
is non-deterministic. Worse, the SDK loads `settingSources: ["user","project","local"]`, so a
committed `.claude/settings.json` / `.claude/settings.local.json` in the checkout can pin
`permissionMode` to `default` and **override the workflow's `settings:` input** (the tell: SDK
log shows `permissionMode: default` despite a `settings` block).

**Guardrail:** Set the mode via `claude_args: |` `--permission-mode bypassPermissions` on every
claude step — this reaches the SDK's `permissionMode` directly. Never commit
`.claude/settings.local.json` (it's gitignored here). Safe on ephemeral CI runners with a
repo-scoped token. (CI config — enforced by the workflows + this note.)

## Auto-applying `agent-ready` with `GITHUB_TOKEN` silently fails to fire the agent

**Symptom:** Well-formed issues got the `agent-ready` label automatically, but the agent never
ran — and a human couldn't trigger it by adding the label via the UI either (it was already
present).

**Root cause:** `agent-ready-trigger.yml` fires on `issues: [labeled]`. GitHub suppresses
workflow-triggering for events generated by the default `GITHUB_TOKEN` (anti-recursion), so the
auto-applied label produces no `labeled` event the trigger can see. And once present, a human
"adding" it is a no-op. The auto-labeler defeats the trigger it was meant to feed.

**Guardrail:** Don't auto-*apply* the label. `auto-label-agent-ready.yml` is a **comment-only**
validator: it advises, and a human (or a PAT) applies the label — which is what fires the
trigger. General tell: a workflow mutating state via `GITHUB_TOKEN` expecting another workflow
to react to that mutation. Use a PAT, or let a human perform the triggering action.

## Compound-Engineering slash commands don't resolve in the headless action — use direct prompts

**Symptom:** Workflows whose prompt said to "invoke `/ce-work`" or "run `/ce-plan`" produced
nothing useful in CI.

**Root cause:** Those slash commands are an interactive Claude Code convenience; the headless
`claude-code-action` does not resolve them. The prompt has to spell out the work directly.

**Guardrail:** `agent-ready-trigger.yml` and `plan-approval-gate.yml` use direct, explicit
prompts (read the issue, implement to acceptance criteria, open a PR), not slash commands.

## PR "Closes" keyword requires the `#` prefix to auto-close issues

**Symptom:** A PR body said `Closes 135` (no `#`) or `**Closes:** #135` (bolded keyword). The
issue stayed open after merge.

**Root cause:** GitHub's auto-close parser only recognises `Closes #NNN` (and `Fixes`/`Resolves`
variants) with the `#` and an un-bolded keyword. One missing/extra character orphans the issue.

**Guardrail:** Write `Closes #NNN` in PR bodies and templates (the agent-generated PR template
uses the plain, correct form). Verify the PR's "Development" sidebar links the issue before merge.

## The CI agent often can't run the test suite — write tests rigorously; the PR's CI is the gate

**Symptom:** Agent PRs opened with their own newly-written tests failing.

**Root cause:** The trigger runner may have no database/services, so the agent can't execute the
suite. It writes tests "blind."

**Guardrail:** Make tests mandatory; have the agent trace each test against its implementation
before finishing, and never knowingly open a red PR. The project's CI on the PR is the real gate;
an `@claude` PR-feedback loop closes the gap when CI catches a miss.
