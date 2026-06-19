# Plan: Rethink decisions & todos — legibility and lifecycle
**Issue:** #10  
**Date:** 2026-06-19  
**Type:** feature  
**Status:** awaiting approval

---

## Problem frame

`@decision` and `@todo` markers are attached to block headers, meaning the "decision" or "todo" is the entire note block — which can be paragraphs of free-form prose. `journal decisions` and `journal todos` render these as generic chunk snippets truncated at 240 characters, with no prominence for the statement itself, no date column, and no lifecycle awareness. In practice, with one or two large opaque blocks, users never look at the output.

The value proposition is real: "what did I commit to / decide, and is it still open?" is a genuinely useful primitive for a work journal. The current implementation just doesn't deliver it.

### What "crisp" means here

A crisp decision is a one-line statement (the thing decided) followed by optional rationale. A crisp todo is a one-line action item. The block format already supports this — it's a capture-UX and rendering problem, not a format problem. The fix is:

1. Low-friction dedicated capture commands that naturally produce well-formed entries.
2. Rendering that surfaces the statement (first body line) prominently, not a truncated prose blob.
3. Proactive surfacing in `journal today` so decisions and todos are seen without hunting.
4. `journal done` that accepts a resolution note, closing the loop on what happened.

---

## Requirements

### Must-have (this issue)

- `journal decide "..."` capture command — one-line statement, optional `--rationale` and `--project` flags.
- `journal todo "..."` capture command — one-line action item, optional `--project` flag.
- `journal decisions` renders crisply: date · statement (first body line) · citation. No raw prose blobs.
- `journal today` surfaces: open todos (existing, top 10) + recent decisions (top 5, last 2w).
- `journal done <ref> [resolution]` accepts an optional resolution note appended to the completed block.
- MCP `today` tool includes decisions in its JSON response.
- Existing notes with `@decision`/`@todo`/`@done` markers still parse and surface correctly.
- Full test coverage: unit tests for new commands, updated tests for changed rendering, backward-compat fixture test.

### Nice-to-have (out of scope for this issue, document as follow-up)

- `journal decide "..." --supersedes <citation>` — supersedence tracking.
- `journal decisions --active` — filter to non-superseded decisions.
- TUI integration for the new commands.

---

## Scope boundaries

**In scope:** `internal/note/note.go`, `cmd/decide.go` (new), `cmd/todo.go` (new), `cmd/todos.go`, `cmd/done.go`, `cmd/listing.go`, `cmd/show.go`, `cmd/mcp.go`, and their `*_test.go` siblings.

**Out of scope:**
- New marker types (no `@superseded`, `@resolved`, or similar).
- The MCP resources/prompts work (#8/#9) — the plan coordinates but does not duplicate.
- `.goreleaser.yaml`, release pipeline, Homebrew tap.
- TUI (`cmd/tui.go`).

---

## Key technical decisions

### 1. Dedicated capture commands vs. improving `capture --marker`

`journal capture "call bob @todo"` already works, but "works" ≠ "discoverable." A user looking for the todo workflow shouldn't need to know the `@marker` syntax. `journal decide "..."` and `journal todo "..."` are thin wrappers that improve discoverability and produce consistently structured entries (one-line statement with optional labeled rationale) without duplicating logic — they call the existing `capture()` function.

**Decision:** Add `cmd/decide.go` and `cmd/todo.go` as dedicated cobra commands; they call the existing `capture()` internal function directly.

### 2. Backward compatibility — no format changes

The on-disk format (`## HH:MM @marker` block header + free-form body) is unchanged. Existing opaque blocks continue to parse, index, and appear in listings. The new commands produce the same format — they just naturally produce one-line bodies when given a one-line argument.

**Decision:** Zero changes to `internal/note` parsing. Backward compat is structural, not a migration.

### 3. First body line as the decision/todo statement

For rendering, the "statement" is `strings.Fields(body)[0:N]` joined until a newline — concretely, the first non-empty line of the block body. For entries created by the new commands this is always the argument text. For legacy opaque blocks it's the first line of whatever the user wrote, which may be mid-sentence but is still more signal than a truncated 240-char blob.

**Decision:** `decisionsStatement(body string) string` helper extracts the first line of a chunk body; used in the custom decisions renderer and in `composeToday`.

### 4. Resolution note in `done` — in-place block edit

`completeTodo` already rewrites the `@todo` marker in-place (the one sanctioned in-file mutation). Extending it to also append `Resolution: <text>` as the last line of that block's line range (before the blank-line separator) is a minimal extension of the same surgery. It does not touch other blocks.

**Decision:** Extend `completeTodo(ctx, cfg, e, ref, resolution, logf)` to accept a `resolution string`; when non-empty, append `Resolution: <text>` after the existing body within the chunk's line range. All callers updated (MCP `done` tool, TUI `done` handler).

### 5. `journal today` decisions section

`todayReport` gains a `Decisions []Result` field (JSON tag `"decisions"`). `gatherToday` queries `listFromStore` with `Markers: ["decision"]`, `Since: 2w`, limit 5. `composeToday` adds a "Recent decisions" section between the open-todos and meetings sections. The JSON shape is additive (new field); existing consumers see it as an optional field — no breaking change.

**Decision:** Add `Decisions []Result` to `todayReport`; gather top-5 last-2w decisions; render in text and JSON.

### 6. MCP `today` tool includes decisions

`mcpToday` marshals `todayReport`, which now includes `Decisions`. The MCP `decisions` tool itself is unchanged — it already works and is called independently. No schema-breaking changes.

### 7. Custom renderer for `journal decisions`

`decisionsCmd` currently calls `renderResults`, the generic renderer. Replace with a dedicated `renderDecisions(out, results, jsonMode)` that formats each decision as:

```
 1. 2026-06-15  Use sqlite-vec for vectors           daily/2026/06/2026-06-15.md:9-14
                Rationale: Pure-Go, no cgo needed
```

The first body line is the statement; remaining lines (up to 80 chars) are shown as a sub-line if present. JSON mode continues to return the stable `{results:[...]}` envelope unchanged.

---

## Implementation units

### Unit 1 — `cmd/decide.go` (new file)

**Goal:** Low-friction `journal decide "..."` capture command.

**Files:** `cmd/decide.go`, `cmd/decide_test.go`

**Approach:**
```
decideCmd = &cobra.Command{
    Use:   "decide <statement>",
    Short: "Capture a @decision — a one-line statement with optional rationale",
    Args:  cobra.MinimumNArgs(1),
    RunE:  ...,
}
```
- Joins args into statement text (same pattern as `captureCmd`).
- `--rationale <text>` flag: appends `\nRationale: <text>` to body.
- `--project <slug>` flag: routes to project notes.
- Calls `capture(root, now(), body, nil, project, "decision")`.
- Prints `decided → <citation>` and auto-commit line (same pattern as `captureCmd`).
- Registered in `init()` via `rootCmd.AddCommand(decideCmd)`.

**Test scenarios (table-driven):**
- Statement only → body is statement text, marker is `decision`.
- Statement + `--rationale` → body ends with `\nRationale: ...`.
- `--project` flag → routes to project notes path.
- Empty statement → returns error.
- Inline `@decision` token in args still captured (not double-added in body display).

---

### Unit 2 — `cmd/todo.go` (new file)

**Goal:** Low-friction `journal todo "..."` capture command.

**Files:** `cmd/todo.go`, `cmd/todo_test.go`

**Approach:** Identical pattern to `decide.go` but with `--marker todo`. No `--rationale` flag (todos are action items, not decisions). Optional `--project` flag.

```
todoCmd = &cobra.Command{
    Use:   "todo <action>",
    Short: "Capture a @todo — a one-line action item",
    Args:  cobra.MinimumNArgs(1),
    RunE:  ...,
}
```

Note: `todo` and `todos` coexist as distinct cobra commands with no collision.

**Test scenarios:**
- Action text → block with `@todo` marker.
- `--project` → project notes.
- Empty action → error.

---

### Unit 3 — Crisp `journal decisions` rendering

**Goal:** `journal decisions` output is a dated, statement-first list.

**Files:** `cmd/listing.go`, `cmd/listing_test.go`

**Approach:**
1. Add `decisionsStatement(body string) string` helper: returns the first non-empty line of body (trimmed).
2. Add `renderDecisions(out io.Writer, results []Result, jsonMode bool) error`: numbered list with date, statement, citation, and optional sub-line for rationale/second line.
3. `decisionsCmd.RunE` calls `renderDecisions` instead of `renderResults`.
4. JSON output path (`jsonMode=true`) still calls the existing `renderResults` for the stable `{results:[...]}` envelope — no JSON schema change.

**Format (text mode):**
```
 1. 2026-06-15  Use sqlite-vec for vectors           daily/2026/.../2026-06-15.md:9-14
                Rationale: Pure-Go, no cgo needed
 2. 2026-06-10  Adopt Conventional Commits            daily/2026/.../2026-06-10.md:5-7
```

**Test scenarios (table-driven in listing_test.go):**
- Single decision with rationale → statement + rationale rendered.
- Multi-line body → only first line shown as statement; second line shown if it starts with `Rationale:`.
- Legacy opaque block (long prose body) → first line shown, rest truncated.
- Empty results → "no decisions" hint.
- `--json` flag → unchanged `{results:[...]}` envelope.

---

### Unit 4 — `journal done` resolution note

**Goal:** `journal done <ref> [resolution]` closes a todo with a short note.

**Files:** `cmd/done.go`, `cmd/todos_test.go` (existing tests extended)

**Approach:**
1. `doneCmd` changes `Args: cobra.ExactArgs(1)` to `cobra.RangeArgs(1, 2)`.
2. When two args given, `args[1]` is the resolution text.
3. `completeTodo` signature extended: `completeTodo(ctx, cfg, e, ref, resolution string, logf func(...))`.
4. In `completeTodo`, after the `@todo → @done YYYY-MM-DD` rewrite, if `resolution != ""`: scan the block's line range for the last non-empty line and insert `Resolution: <resolution>` after it (before the block's trailing blank line). Uses the same `strings.Split / strings.Join` surgery as the existing rewrite.
5. All callers updated: MCP `doneInput` gains optional `Resolution string` field (JSON `"resolution,omitempty"`); `mcpDone` passes it through.

**Test scenarios:**
- `done <ref>` (no resolution) → existing behavior unchanged.
- `done <ref> "resolution text"` → file contains `Resolution: resolution text` within the block.
- MCP done with `resolution` field → same file edit.
- Stale-index error path → unchanged.

---

### Unit 5 — `journal today` decisions section

**Goal:** `journal today` proactively surfaces recent decisions.

**Files:** `cmd/show.go`, `cmd/show_test.go`

**Approach:**
1. `todayReport` struct gains `Decisions []Result \`json:"decisions"\``.
2. `gatherToday` after gathering todos: calls `listFromStore(ctx, cfg, store.Filter{Markers: []string{note.MarkerDecision}, Since: twoWeeksAgo}, 5)` and stores in `rep.Decisions`.
3. `composeToday`: adds "## Recent decisions (N)" section between todos and meetings sections, rendered as:
   ```
   - 2026-06-15 · Use sqlite-vec for vectors · `daily/…/2026-06-15.md:9-14`
   ```
4. `todayCmd` `--json` output naturally includes `decisions` via `todayReport`.
5. `mcpToday` marshals the updated `todayReport` — MCP consumers get `decisions` as a new optional field.

**Test scenarios (in show_test.go):**
- Today with indexed decisions → `Decisions` slice populated, text output contains "Recent decisions" section.
- Today with no decisions → section omitted from text output; `decisions: []` in JSON.
- Decision from > 2w ago → not included (since filter applied).
- `--json` output includes `"decisions"` key.

---

### Unit 6 — MCP alignment

**Goal:** MCP `today` tool returns decisions; `done` tool supports resolution.

**Files:** `cmd/mcp.go`, `cmd/mcp_test.go`

**Approach:**
1. `todayMCPInput` unchanged; `mcpToday` already marshals `todayReport` — Unit 5 changes flow through automatically.
2. `doneInput` gains `Resolution string \`json:"resolution,omitempty"\``.
3. `mcpDone` passes `in.Resolution` to the updated `completeTodo`.
4. MCP tool description for `done` updated to mention the optional resolution field.
5. No other schema changes; `decisions` and `todos` tool schemas unchanged.

**Test scenarios (extend existing mcp_test.go):**
- `mcpToday` JSON includes `"decisions"` key.
- `mcpDone` with `Resolution` field → file contains `Resolution:` line.

---

## Backward compatibility

All existing `@decision` / `@todo` / `@done` notes parse identically — no changes to `internal/note` parsing. `journal decisions` and `journal todos` surface legacy blocks using the same statement-extraction logic (first body line); the output is crisper, not different in structure. The JSON schemas for `decisions`, `todos`, `done` MCP tools are additive: `doneInput` gains one optional field. `todayReport` gains one optional array field. Existing consumers see no breakage.

A backward-compat test fixture (`TestDecisionsLegacyOpaqueBlock`) in `listing_test.go` verifies that a multi-line opaque block captured the old way still appears in `journal decisions` output with its first line as the statement.

---

## Testing strategy

- **Unit tests** for each new command (`decide_test.go`, `todo_test.go`) covering: happy path, flag combinations, empty-input error.
- **Rendering tests** (`listing_test.go`) using in-memory fixtures (no store): `decisionsStatement` helper, `renderDecisions` output format.
- **Integration tests** using `indexedRepo` helper (see `todos_test.go` pattern): decisions surfacing in `gatherToday`, `journal done` with resolution note.
- **MCP tests** (extend `mcp_test.go`): `mcpToday` includes decisions; `mcpDone` with resolution.
- **Backward-compat fixture**: opaque legacy block appears correctly in `journal decisions`.
- No network calls in any test. No new test infrastructure needed — `indexedRepo` and `embed.NewFake` cover all paths.

---

## File checklist

| File | Action |
|------|--------|
| `cmd/decide.go` | Create |
| `cmd/decide_test.go` | Create |
| `cmd/todo.go` | Create |
| `cmd/todo_test.go` | Create |
| `cmd/listing.go` | Modify (`renderDecisions`, `decisionsStatement`) |
| `cmd/listing_test.go` | Modify (new render tests, backward-compat fixture) |
| `cmd/done.go` | Modify (`completeTodo` resolution param, `doneCmd` 1-2 args) |
| `cmd/todos_test.go` | Modify (resolution-note test cases) |
| `cmd/show.go` | Modify (`todayReport.Decisions`, `gatherToday`, `composeToday`) |
| `cmd/show_test.go` | Modify (decisions-in-today tests) |
| `cmd/mcp.go` | Modify (`doneInput.Resolution`, `mcpDone` passthrough) |
| `cmd/mcp_test.go` | Modify (today+decisions, done+resolution) |

No changes to `internal/note`, `internal/store`, `internal/index`, or `internal/synth`.

---

## Follow-up issues (not in scope)

- **Supersedence tracking**: `journal decide "..." --supersedes <citation>` writes a `Supersedes:` body line; `journal decisions` renders "(superseded by X)".
- **`journal decisions --active`**: filters to decisions not referenced by a `Supersedes:` line in a newer decision.
- **TUI integration**: `journal todo "..."` / `journal decide "..."` accessible from the TUI action menu.
- **Synthesis integration**: the `decisions` synth kind (already in `internal/synth`) uses the crisp statement rendering.
