package synth

import (
	"fmt"
	"strings"

	"github.com/ericmann/journal/internal/store"
)

// Kind identifies a synthesis job.
type Kind string

const (
	KindWeekly    Kind = "weekly"
	KindDecisions Kind = "decisions"
	KindStale     Kind = "stale"
)

// renderChunks formats chunks as a stable, citation-friendly note list for a
// prompt. Output is deterministic for golden-file testing.
func renderChunks(chunks []store.Chunk) string {
	if len(chunks) == 0 {
		return "(no notes in range)\n"
	}
	var sb strings.Builder
	for _, c := range chunks {
		date := ""
		if !c.CreatedAt.IsZero() {
			date = c.CreatedAt.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&sb, "- [%s:%d-%d] %s", c.Path, c.LineStart, c.LineEnd, date)
		if c.Heading != "" {
			fmt.Fprintf(&sb, " — %s", c.Heading)
		}
		if len(c.Markers) > 0 {
			fmt.Fprintf(&sb, " (@%s)", strings.Join(c.Markers, " @"))
		}
		sb.WriteString("\n")
		for _, line := range strings.Split(strings.TrimRight(c.Body, "\n"), "\n") {
			fmt.Fprintf(&sb, "  %s\n", line)
		}
	}
	return sb.String()
}

// AssembleWeekly builds the weekly-reflection prompt for the given ISO week
// label (e.g. "2026-W23") and the week's chunks.
func AssembleWeekly(weekLabel string, chunks []store.Chunk) string {
	var sb strings.Builder
	sb.WriteString("You are drafting a weekly reflection from a developer's raw journal notes.\n")
	sb.WriteString("Synthesize the week into a curated draft the author will edit and post to their team.\n\n")
	sb.WriteString("Guidelines:\n")
	sb.WriteString("- Group related notes into themes; lead with what mattered.\n")
	sb.WriteString("- Call out decisions (@decision), open questions (@question), and unresolved threads.\n")
	sb.WriteString("- Be concise and concrete; preserve technical specifics.\n")
	sb.WriteString("- Cite supporting notes inline as path:line_start-line_end.\n")
	sb.WriteString("- Output GitHub-flavored markdown. Do not invent facts not in the notes.\n\n")
	fmt.Fprintf(&sb, "## Week %s — raw notes\n\n", weekLabel)
	sb.WriteString(renderChunks(chunks))
	return sb.String()
}

// AssembleDecisions builds the decision-rollup prompt. scope is a human label
// for what was gathered (e.g. a project slug or "all projects").
func AssembleDecisions(scope string, chunks []store.Chunk) string {
	var sb strings.Builder
	sb.WriteString("You are compiling a decision log from a developer's journal.\n")
	sb.WriteString("Produce a concise rollup of the decisions below.\n\n")
	sb.WriteString("Guidelines:\n")
	sb.WriteString("- One bullet per decision: the decision, then its rationale if present.\n")
	sb.WriteString("- Preserve dates and cite the source note as path:line_start-line_end.\n")
	sb.WriteString("- Group by theme if helpful. Do not invent decisions not in the notes.\n")
	sb.WriteString("- Output GitHub-flavored markdown.\n\n")
	fmt.Fprintf(&sb, "## Decisions — %s\n\n", scope)
	sb.WriteString(renderChunks(chunks))
	return sb.String()
}

// AssembleStale builds the stale-thread prompt from threads with no recent
// activity, described by lines (project, days idle, open questions).
func AssembleStale(days int, threadLines []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "You are reviewing project threads with no activity in the last %d days.\n", days)
	sb.WriteString("For each, suggest whether to revive, park, or close it, and the single next action if reviving.\n\n")
	sb.WriteString("Guidelines:\n")
	sb.WriteString("- Be brief: one short paragraph per thread.\n")
	sb.WriteString("- Flag any open questions that are blocking.\n")
	sb.WriteString("- Output GitHub-flavored markdown.\n\n")
	sb.WriteString("## Stale threads\n\n")
	if len(threadLines) == 0 {
		sb.WriteString("(no stale threads)\n")
	} else {
		for _, l := range threadLines {
			fmt.Fprintf(&sb, "- %s\n", l)
		}
	}
	return sb.String()
}
