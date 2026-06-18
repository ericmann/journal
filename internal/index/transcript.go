package index

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/store"
)

// Transcript chunking is section-aware. A rendered transcript is YAML
// frontmatter (metadata — not worth indexing), a `# Title`, an optional
// `## Notes` summary, and a long `## Transcript` body. We:
//   - drop the frontmatter (its participant/tag/date noise diluted retrieval);
//   - emit each short `##` section (notably `## Notes`) as its own chunk, so the
//     summary is a clean, high-signal entry point search can hit directly;
//   - line-window only sections longer than the window (the transcript body),
//     with overlap so context isn't split mid-thought.
//
// Files without this structure (e.g. a plain dropped-in .txt) are simply
// line-windowed as one section. Both Quill and WhisperX transcripts share this
// shape, so both benefit.
const (
	transcriptWindowLines = 40
	transcriptOverlap     = 8
)

// ChunkTranscript splits a transcript into chunks (see the package comment).
// Every chunk is source=transcript, dated by the file's mtime, and carries the
// configured tag. Anchors keep per-chunk IDs stable so unchanged content does
// not re-embed.
func ChunkTranscript(relPath, content string, mtime time.Time, tag string) []store.Chunk {
	lines := strings.Split(content, "\n")
	var tags []string
	if tag != "" {
		tags = []string{tag}
	}

	var chunks []store.Chunk
	for _, sec := range splitTranscriptSections(lines, frontmatterEnd(lines)) {
		if len(sec.lines) > transcriptWindowLines {
			chunks = append(chunks, windowSection(relPath, sec, mtime, tags)...)
		} else if c, ok := singleSectionChunk(relPath, sec, mtime, tags); ok {
			chunks = append(chunks, c)
		}
	}
	return chunks
}

// frontmatterEnd returns the 0-based index of the first line after a leading
// YAML frontmatter block (--- … ---), or 0 if there is none / it's unterminated.
func frontmatterEnd(lines []string) int {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return 0
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return i + 1
		}
	}
	return 0
}

// transcriptSection is a contiguous run of original lines: the preamble (before
// the first `##`) or one `##` section. off is its 0-based start in the file.
type transcriptSection struct {
	anchor  string // ID-stability anchor base ("head" for the preamble, else the heading slug)
	heading string // display heading ("" for the preamble)
	off     int
	lines   []string
}

// splitTranscriptSections partitions lines[start:] into the preamble plus one
// section per `##` heading.
func splitTranscriptSections(lines []string, start int) []transcriptSection {
	var secs []transcriptSection
	i := start
	for ; i < len(lines) && !isH2(lines[i]); i++ {
	}
	secs = append(secs, transcriptSection{anchor: "head", off: start, lines: lines[start:i]})
	for i < len(lines) {
		heading := strings.TrimSpace(lines[i][len("## "):])
		secStart := i
		for i++; i < len(lines) && !isH2(lines[i]); i++ {
		}
		secs = append(secs, transcriptSection{
			anchor:  anchorSlug(heading),
			heading: heading,
			off:     secStart,
			lines:   lines[secStart:i],
		})
	}
	return secs
}

// windowSection line-windows a long section, offsetting line numbers by the
// section's position in the file.
func windowSection(relPath string, sec transcriptSection, mtime time.Time, tags []string) []store.Chunk {
	step := transcriptWindowLines - transcriptOverlap
	if step < 1 {
		step = transcriptWindowLines
	}
	var chunks []store.Chunk
	for s := 0; s < len(sec.lines); s += step {
		e := s + transcriptWindowLines
		if e > len(sec.lines) {
			e = len(sec.lines)
		}
		body := strings.Join(sec.lines[s:e], "\n")
		if strings.TrimSpace(body) == "" {
			if e == len(sec.lines) {
				break
			}
			continue
		}
		anchor := fmt.Sprintf("#%s-w%d", sec.anchor, s/step)
		chunks = append(chunks, store.Chunk{
			ID:        ChunkID(relPath, anchor, body),
			Path:      relPath,
			LineStart: sec.off + s + 1,
			LineEnd:   sec.off + e,
			Body:      body,
			CreatedAt: mtime.UTC(),
			Tags:      tags,
			Source:    store.SourceTranscript,
		})
		if e == len(sec.lines) {
			break
		}
	}
	return chunks
}

// singleSectionChunk emits one chunk for a short section (e.g. ## Notes), with
// trailing blank lines trimmed so LineEnd points at real content. ok is false
// for an empty section.
func singleSectionChunk(relPath string, sec transcriptSection, mtime time.Time, tags []string) (store.Chunk, bool) {
	n := len(sec.lines)
	for n > 0 && strings.TrimSpace(sec.lines[n-1]) == "" {
		n--
	}
	if n == 0 {
		return store.Chunk{}, false
	}
	body := strings.Join(sec.lines[:n], "\n")
	if strings.TrimSpace(body) == "" {
		return store.Chunk{}, false
	}
	return store.Chunk{
		ID:        ChunkID(relPath, "#"+sec.anchor, body),
		Path:      relPath,
		LineStart: sec.off + 1,
		LineEnd:   sec.off + n,
		Heading:   sec.heading,
		Body:      body,
		CreatedAt: mtime.UTC(),
		Tags:      tags,
		Source:    store.SourceTranscript,
	}, true
}

var anchorRe = regexp.MustCompile(`[^a-z0-9]+`)

// anchorSlug makes a stable anchor base from a heading ("Notes" -> "notes").
func anchorSlug(s string) string {
	s = strings.Trim(anchorRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		s = "section"
	}
	return s
}
