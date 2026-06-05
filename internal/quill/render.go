package quill

import (
	"fmt"
	"strings"
	"time"
)

// Filename is the transcript landing-zone filename for a meeting:
// <YYYY-MM-DD>-<slug>.md (or <meeting-id> when undated/untitled), stable so
// re-syncing the same meeting overwrites rather than duplicates.
func (m Meeting) Filename() string {
	date := "undated"
	if !m.Start.IsZero() {
		date = m.Start.UTC().Format("2006-01-02")
	}
	slug := slugify(m.Title)
	if slug == "" {
		slug = slugify(m.ID)
	}
	if slug == "" {
		slug = "meeting"
	}
	return fmt.Sprintf("%s-%s.md", date, slug)
}

// RenderMarkdown renders a meeting as a Markdown document: YAML frontmatter, the
// title as an H1, Quill's AI notes, and the speaker-labeled transcript. The
// existing transcript indexer treats the result as plain markdown.
func RenderMarkdown(m Meeting) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "meeting_id: %q\n", m.ID)
	fmt.Fprintf(&b, "title: %q\n", m.Title)
	if m.Type != "" {
		fmt.Fprintf(&b, "type: %q\n", m.Type)
	}
	if !m.Start.IsZero() {
		fmt.Fprintf(&b, "date: %q\n", m.Start.UTC().Format("2006-01-02"))
		fmt.Fprintf(&b, "start: %q\n", m.Start.UTC().Format(time.RFC3339))
	}
	if !m.End.IsZero() {
		fmt.Fprintf(&b, "end: %q\n", m.End.UTC().Format(time.RFC3339))
	}
	if !m.Start.IsZero() && !m.End.IsZero() && m.End.After(m.Start) {
		fmt.Fprintf(&b, "duration: %q\n", m.End.Sub(m.Start).Round(time.Minute).String())
	}
	b.WriteString("participants: " + yamlList(m.Participants) + "\n")
	b.WriteString("tags: " + yamlList(m.Tags) + "\n")
	b.WriteString("source: quill\n")
	b.WriteString("---\n\n")

	fmt.Fprintf(&b, "# %s\n\n", m.Title)

	if strings.TrimSpace(m.Notes) != "" {
		b.WriteString("## Notes\n\n")
		b.WriteString(strings.TrimRight(m.Notes, "\n"))
		b.WriteString("\n\n")
	}

	if len(m.Transcript) > 0 {
		b.WriteString("## Transcript\n\n")
		for _, seg := range coalesceSegments(m.Transcript) {
			if seg.Speaker != "" {
				fmt.Fprintf(&b, "**%s:** %s\n\n", seg.Speaker, seg.Text)
			} else {
				fmt.Fprintf(&b, "%s\n\n", seg.Text)
			}
		}
	}
	return b.String()
}

// coalesceSegments merges consecutive turns from the same speaker into one, so a
// speaker who talks across many short blocks reads as a single labeled paragraph
// rather than a repeated "**Speaker 1:**" wall.
func coalesceSegments(segs []Segment) []Segment {
	var out []Segment
	for _, s := range segs {
		if n := len(out); n > 0 && out[n-1].Speaker == s.Speaker {
			out[n-1].Text = strings.TrimSpace(out[n-1].Text + " " + s.Text)
			continue
		}
		out = append(out, s)
	}
	return out
}

// yamlList renders a YAML flow sequence with quoted items: [] or ["a", "b"].
func yamlList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = fmt.Sprintf("%q", s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
	}
	return out
}
