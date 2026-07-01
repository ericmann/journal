package log

import (
	"fmt"
	"strings"

	"github.com/ericmann/journal/internal/note"
)

// Assemble renders a voice-note document from the shaped (or raw-fallback)
// input. The result is a spec §6.3 YAML-frontmatter Markdown file.
func Assemble(in AssembleInput) string {
	tags := dedupeTags(append([]string{"voice"}, in.Tags...))

	// Count markers by type.
	todos, decisions, questions := countMarkers(in.Markers)

	transcriber := in.Transcriber
	if transcriber == "" {
		transcriber = "text"
	}

	var b strings.Builder

	// YAML frontmatter.
	b.WriteString("---\n")
	b.WriteString("source: voice\n")
	if !in.CapturedAt.IsZero() {
		fmt.Fprintf(&b, "date: %q\n", in.CapturedAt.UTC().Format("2006-01-02"))
		fmt.Fprintf(&b, "captured_at: %q\n", in.CapturedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintf(&b, "duration_sec: %d\n", in.DurationSec)
	fmt.Fprintf(&b, "transcriber: %q\n", transcriber)
	b.WriteString("tags: " + yamlList(tags) + "\n")
	if in.Summary != "" {
		fmt.Fprintf(&b, "summary: %q\n", in.Summary)
	}
	if todos > 0 {
		fmt.Fprintf(&b, "todos: %d\n", todos)
	}
	if decisions > 0 {
		fmt.Fprintf(&b, "decisions: %d\n", decisions)
	}
	if questions > 0 {
		fmt.Fprintf(&b, "questions: %d\n", questions)
	}
	if in.AudioPath != "" {
		fmt.Fprintf(&b, "audio: %q\n", in.AudioPath)
	} else {
		b.WriteString("audio: null\n")
	}
	b.WriteString("---\n\n")

	// Title.
	title := in.Title
	if title == "" {
		title = "Voice Note"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)

	// Summary section (only when shaped).
	if in.Summary != "" {
		b.WriteString("## Summary\n\n")
		b.WriteString(strings.TrimRight(in.Summary, "\n"))
		b.WriteString("\n\n")
	}

	// Notes section: shaped body, or raw text as fallback.
	b.WriteString("## Notes\n\n")
	body := in.Body
	if body == "" {
		body = in.RawText
	}
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n")

	// Inline markers (only when shaped and markers exist).
	if len(in.Markers) > 0 {
		b.WriteString("\n")
		for _, m := range in.Markers {
			b.WriteString(m)
			b.WriteString("\n")
		}
	}

	// Collapsed raw transcript (only when enabled and raw text is present).
	if in.KeepRawTranscript && strings.TrimSpace(in.RawText) != "" {
		b.WriteString("\n<details>\n<summary>Raw transcript</summary>\n\n")
		b.WriteString(strings.TrimRight(in.RawText, "\n"))
		b.WriteString("\n\n</details>\n")
	}

	return b.String()
}

// dedupeTags returns tags with duplicates removed, preserving order.
func dedupeTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// countMarkers counts @todo, @decision, and @question entries in markers.
func countMarkers(markers []string) (todos, decisions, questions int) {
	for _, m := range markers {
		parts := strings.Fields(m)
		if len(parts) == 0 {
			continue
		}
		switch strings.TrimPrefix(parts[0], "@") {
		case note.MarkerTodo:
			todos++
		case note.MarkerDecision:
			decisions++
		case note.MarkerQuestion:
			questions++
		}
	}
	return
}

// yamlList renders a []string as an inline YAML list: ["a","b"].
func yamlList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
