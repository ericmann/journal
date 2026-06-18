// Package transcribe turns WhisperX JSON output into an indexable transcript
// Markdown document — the same shape Quill transcripts render to (frontmatter,
// # Title, an optional ## Notes summary, and a speaker-labeled ## Transcript) so
// non-Quill meetings land in the index identically. Stamped source: whisperx.
package transcribe

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Segment is one diarized utterance from a WhisperX transcript.
type Segment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Speaker string  `json:"speaker"` // e.g. "SPEAKER_00"; empty if diarization didn't label it
}

type whisperXDoc struct {
	Segments []Segment `json:"segments"`
}

// ParseWhisperX parses WhisperX JSON output (its top-level "segments" array),
// trimming each segment's text.
func ParseWhisperX(data []byte) ([]Segment, error) {
	var d whisperXDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parsing WhisperX JSON: %w", err)
	}
	if len(d.Segments) == 0 {
		return nil, fmt.Errorf("no segments found (expected a top-level \"segments\" array — is this WhisperX JSON?)")
	}
	out := d.Segments[:0]
	for _, s := range d.Segments {
		s.Text = strings.TrimSpace(s.Text)
		if s.Text != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// Speakers returns the distinct speaker labels in first-appearance order.
func Speakers(segs []Segment) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range segs {
		if s.Speaker != "" && !seen[s.Speaker] {
			seen[s.Speaker] = true
			out = append(out, s.Speaker)
		}
	}
	return out
}

// PlainText renders the transcript as "Speaker: text" lines, for feeding a
// summarizer — no markdown, no timestamps.
func PlainText(segs []Segment) string {
	var b strings.Builder
	for _, s := range coalesce(segs) {
		if s.Speaker != "" {
			fmt.Fprintf(&b, "%s: %s\n", s.Speaker, s.Text)
		} else {
			fmt.Fprintf(&b, "%s\n", s.Text)
		}
	}
	return b.String()
}

// Render produces the transcript Markdown: YAML frontmatter, the title as H1, an
// optional ## Notes summary (omitted when empty), and the speaker-labeled,
// timestamped ## Transcript. The transcript indexer treats the result as plain
// markdown (line-windowed), so a summary up top is the first thing search hits.
func Render(title string, date time.Time, segs []Segment, notes string, tags []string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %q\n", title)
	if !date.IsZero() {
		fmt.Fprintf(&b, "date: %q\n", date.UTC().Format("2006-01-02"))
	}
	b.WriteString("participants: " + yamlList(Speakers(segs)) + "\n")
	b.WriteString("tags: " + yamlList(tags) + "\n")
	b.WriteString("source: whisperx\n")
	b.WriteString("---\n\n")

	fmt.Fprintf(&b, "# %s\n\n", title)

	if strings.TrimSpace(notes) != "" {
		b.WriteString("## Notes\n\n")
		b.WriteString(strings.TrimRight(notes, "\n"))
		b.WriteString("\n\n")
	}

	b.WriteString("## Transcript\n\n")
	for _, s := range coalesce(segs) {
		ts := clock(s.Start)
		switch {
		case s.Speaker != "":
			fmt.Fprintf(&b, "**[%s] %s:** %s\n\n", ts, s.Speaker, s.Text)
		default:
			fmt.Fprintf(&b, "**[%s]** %s\n\n", ts, s.Text)
		}
	}
	return b.String()
}

// Filename is the landing-zone filename: <YYYY-MM-DD>-<slug>.md, stable so
// re-ingesting the same meeting overwrites rather than duplicates.
func Filename(date time.Time, title string) string {
	d := "undated"
	if !date.IsZero() {
		d = date.UTC().Format("2006-01-02")
	}
	slug := slugify(title)
	if slug == "" {
		slug = "meeting"
	}
	return fmt.Sprintf("%s-%s.md", d, slug)
}

// coalesce merges consecutive segments from the same speaker into one turn,
// keeping the first segment's start time.
func coalesce(segs []Segment) []Segment {
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

// clock formats seconds as H:MM:SS (or MM:SS under an hour).
func clock(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	s := int(sec)
	h, m, s := s/3600, s%3600/60, s%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func yamlList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = fmt.Sprintf("%q", it)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
