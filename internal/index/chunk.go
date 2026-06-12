// Package index turns markdown notes into chunks, hashes them stably, and keeps
// the sqlite-vec store in sync (embedding only what changed). The chunk unit is
// the level-2 (`##`) heading block, matching the daily-note format defined in
// internal/note — the writer and this chunker share that one definition.
package index

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
)

// h1DateRe matches a daily/project H1 like "# 2026-06-01".
var h1DateRe = regexp.MustCompile(`^#\s+(\d{4}-\d{2}-\d{2})\s*$`)

// Chunk parses markdown content for repoRelPath into chunks, one per `##`
// heading block. Line numbers are 1-based and inclusive. The body (used for
// embedding and hashing) excludes the heading line. A chunk's CreatedAt is
// derived from the file's H1 date plus the block's HH:MM when both are present.
//
// Content before the first `##` (H1, frontmatter, intro prose) is preamble and
// is not chunked, matching the capture format. Only a date H1 ("# YYYY-MM-DD")
// is structural; any other `# ` line is user content (e.g. markdown pasted into
// a capture block) and stays in the current block's body.
func Chunk(repoRelPath, content string) []store.Chunk {
	lines := strings.Split(content, "\n")
	project := ProjectForPath(repoRelPath)

	var chunks []store.Chunk
	var curDate string // last seen H1 date (YYYY-MM-DD), "" if none

	// Block accumulator.
	inBlock := false
	var bHeading string
	var bStart int // 1-based line of the `##` header
	var bBody []string

	flush := func(end int) {
		if !inBlock {
			return
		}
		chunks = append(chunks, buildChunk(repoRelPath, project, curDate, bHeading, bStart, end, bBody))
		inBlock = false
		bBody = nil
	}

	for i, line := range lines {
		lineNo := i + 1
		switch {
		case isH2(line):
			flush(lineNo - 1)
			inBlock = true
			bHeading = strings.TrimSpace(line[len("## "):])
			bStart = lineNo
		case isH1Date(line):
			flush(lineNo - 1)
			curDate = h1DateRe.FindStringSubmatch(line)[1]
		default:
			if inBlock {
				bBody = append(bBody, line)
			}
		}
	}
	flush(len(lines))
	return chunks
}

func buildChunk(relPath, project, date, heading string, start, end int, bodyLines []string) store.Chunk {
	// Trim trailing blank lines from the block so line_end points at real
	// content, and adjust end accordingly.
	for len(bodyLines) > 0 && strings.TrimSpace(bodyLines[len(bodyLines)-1]) == "" {
		bodyLines = bodyLines[:len(bodyLines)-1]
		end--
	}
	if end < start {
		end = start
	}
	body := strings.Join(bodyLines, "\n")

	// Tags/markers come from the heading line and the body.
	meta := heading + "\n" + body
	tags := note.ParseTags(meta)
	markers := note.ParseMarkers(meta)

	return store.Chunk{
		ID:        ChunkID(relPath, heading, body),
		Path:      relPath,
		LineStart: start,
		LineEnd:   end,
		Heading:   heading,
		Body:      body,
		Project:   project,
		CreatedAt: createdAt(date, heading),
		Tags:      tags,
		Markers:   markers,
	}
}

// ChunkID is the stable identity of a chunk: a hash of the path, the heading
// anchor, and the normalized body. It is independent of line numbers, so moving
// a block within a file does not change its identity (and does not trigger a
// re-embed); editing the body does.
func ChunkID(relPath, heading, body string) string {
	data := relPath + "\x00" + strings.TrimSpace(heading) + "\x00" + normalizeBody(body)
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// normalizeBody makes hashing insensitive to trailing whitespace and surrounding
// blank lines, but sensitive to actual content changes.
func normalizeBody(body string) string {
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	// Drop leading/trailing blank lines.
	start, end := 0, len(lines)
	for start < end && lines[start] == "" {
		start++
	}
	for end > start && lines[end-1] == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// createdAt combines an H1 date (YYYY-MM-DD) with a block's HH:MM heading time.
// It returns the zero time if either is missing or unparseable.
func createdAt(date, heading string) time.Time {
	if date == "" {
		return time.Time{}
	}
	clock, _, _, ok := note.ParseBlockHeading("## " + heading)
	if !ok {
		// Non-time heading (e.g. a project _index.md section): date only.
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			return time.Time{}
		}
		return t.UTC()
	}
	t, err := time.Parse("2006-01-02 15:04", date+" "+clock)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func isH2(line string) bool { return strings.HasPrefix(line, "## ") }

// isH1Date reports whether the line is a structural date H1 ("# YYYY-MM-DD").
// Non-date H1s must not end a block: notes often contain pasted markdown whose
// own `# ` headings are content, not journal structure.
func isH1Date(line string) bool { return h1DateRe.MatchString(line) }

// ProjectForPath returns the project slug for a repo-relative path under
// projects/<slug>/..., or "" if the path is not in a project.
func ProjectForPath(relPath string) string {
	relPath = path.Clean(strings.ReplaceAll(relPath, "\\", "/"))
	parts := strings.Split(relPath, "/")
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1]
	}
	return ""
}
