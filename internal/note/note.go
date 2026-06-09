// Package note defines the on-disk markdown format for the journal and the
// append-only writers and parsers for it. This is the single source of truth
// for the daily-note format: the "# YYYY-MM-DD" H1 and the "## HH:MM #tags
// @markers" block headers. The Phase 3 chunker parses the same format via the
// helpers here, so the writer and the chunker can never drift apart.
//
// Writes are strictly append-only. The tool never rewrites or deletes existing
// note bodies.
package note

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Markers are the structured annotations the synthesis and query layers key
// off. Only these four are recognized. @done is written by `journal done` when
// completing a @todo (and may be hand-written).
const (
	MarkerDecision = "decision"
	MarkerQuestion = "question"
	MarkerTodo     = "todo"
	MarkerDone     = "done"
)

var validMarkers = map[string]bool{
	MarkerDecision: true,
	MarkerQuestion: true,
	MarkerTodo:     true,
	MarkerDone:     true,
}

// timeLayout is the block-header clock format.
const timeLayout = "15:04"

// dateLayout is the daily-file / H1 date format.
const dateLayout = "2006-01-02"

var (
	// A tag is "#" at the start of the text or after whitespace, followed by word
	// chars or hyphens. The whitespace boundary (not merely "non-word char")
	// keeps URL fragments (".../page/#comment-9835") and markdown anchor links
	// ("[see](#summary)") from being mis-extracted as tags.
	tagRe = regexp.MustCompile(`(?:^|\s)#([0-9A-Za-z_-]+)`)
	// A marker is "@" at the start of the text or after whitespace, followed by
	// one of the known set — same whitespace boundary as tags so URL/path "@"s
	// are not mistaken for markers.
	markerRe = regexp.MustCompile(`(?:^|\s)@(decision|question|todo|done)\b`)
	// Block header: exactly "## HH:MM" then optional tags/markers.
	blockTimeRe = regexp.MustCompile(`^\d{2}:\d{2}$`)
	// Slug normalization.
	slugRe = regexp.MustCompile(`[^a-z0-9]+`)
)

// Block is one timestamped capture: a heading line plus a body.
type Block struct {
	Time    time.Time
	Tags    []string
	Markers []string
	Body    string
}

// ValidMarker reports whether m is one of the recognized markers.
func ValidMarker(m string) bool { return validMarkers[m] }

// ParseTags extracts unique, case-folded #tags from text, in first-seen order.
func ParseTags(text string) []string {
	return dedupeFold(tagRe.FindAllStringSubmatch(text, -1))
}

// ParseMarkers extracts unique @markers (from the known set) in first-seen order.
func ParseMarkers(text string) []string {
	return dedupeFold(markerRe.FindAllStringSubmatch(text, -1))
}

func dedupeFold(matches [][]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range matches {
		v := strings.ToLower(m[1])
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// DailyH1 returns the canonical daily-file H1 line, e.g. "# 2026-06-01".
func DailyH1(t time.Time) string {
	return "# " + t.Format(dateLayout)
}

// FormatBlock renders a Block as markdown: a "## HH:MM #tags @markers" header
// followed by the body and a trailing newline.
func FormatBlock(b Block) string {
	var sb strings.Builder
	sb.WriteString("## ")
	sb.WriteString(b.Time.Format(timeLayout))
	for _, tag := range b.Tags {
		sb.WriteString(" #")
		sb.WriteString(tag)
	}
	for _, m := range b.Markers {
		sb.WriteString(" @")
		sb.WriteString(m)
	}
	sb.WriteString("\n")
	body := strings.TrimRight(b.Body, "\n \t")
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	return sb.String()
}

// ParseBlockHeading parses a "## HH:MM #tags @markers" header line. It returns
// the clock string, tags, markers, and ok=false if the line is not a block
// header.
func ParseBlockHeading(line string) (clock string, tags, markers []string, ok bool) {
	if !strings.HasPrefix(line, "## ") {
		return "", nil, nil, false
	}
	rest := strings.TrimSpace(line[len("## "):])
	fields := strings.Fields(rest)
	if len(fields) == 0 || !blockTimeRe.MatchString(fields[0]) {
		return "", nil, nil, false
	}
	meta := strings.Join(fields[1:], " ")
	return fields[0], ParseTags(meta), ParseMarkers(meta), true
}

// DailyPath returns the absolute path of the daily file for t:
// <root>/daily/YYYY/MM/YYYY-MM-DD.md.
func DailyPath(root string, t time.Time) string {
	return filepath.Join(root, "daily", t.Format("2006"), t.Format("01"), t.Format(dateLayout)+".md")
}

// ProjectNotesPath returns the absolute path of the dated project note file:
// <root>/projects/<slug>/notes/YYYY-MM-DD.md.
func ProjectNotesPath(root, slug string, t time.Time) string {
	return filepath.Join(root, "projects", slug, "notes", t.Format(dateLayout)+".md")
}

// Slugify normalizes a free-form project name into a path-safe slug.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// AppendDaily appends b to today's daily file, creating the file (with its
// H1) and parent directories if needed. It returns the file path written.
func AppendDaily(root string, b Block) (string, error) {
	return appendBlock(DailyPath(root, b.Time), DailyH1(b.Time), b)
}

// AppendProject appends b to the dated note file for the given project slug,
// creating the file (with its H1) and directories if needed.
func AppendProject(root, slug string, b Block) (string, error) {
	return appendBlock(ProjectNotesPath(root, slug, b.Time), DailyH1(b.Time), b)
}

// appendBlock implements the shared append-only write: never mutate existing
// bytes except to normalize trailing newlines into exactly one blank-line
// separator before the new block.
func appendBlock(path, h1 string, b Block) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating note directory: %w", err)
	}
	block := FormatBlock(b)

	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		content := h1 + "\n\n" + block
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing note: %w", err)
		}
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("reading note: %w", err)
	}

	// Preserve existing bytes verbatim, normalizing only trailing newlines so
	// exactly one blank line separates the previous content from the new block.
	trimmed := strings.TrimRight(string(existing), "\n")
	content := trimmed + "\n\n" + block
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("appending note: %w", err)
	}
	return path, nil
}
