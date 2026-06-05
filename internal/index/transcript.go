package index

import (
	"fmt"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/store"
)

// Transcript chunking is line-windowed rather than heading-based: meeting
// transcripts are long prose without the daily-note `##` structure, so we slide
// a fixed window (with overlap so context isn't split mid-thought across the
// boundary). Tunable; chosen to keep each chunk a reasonable embedding unit.
const (
	transcriptWindowLines = 40
	transcriptOverlap     = 8
)

// ChunkTranscript splits a transcript into overlapping line windows. Every chunk
// is tagged source=transcript, carries the file's mtime as CreatedAt (transcripts
// lack the daily date+time structure), and gets the configured tag so it is
// findable. Stable per-window IDs mean re-indexing unchanged content re-embeds
// nothing.
func ChunkTranscript(relPath, content string, mtime time.Time, tag string) []store.Chunk {
	lines := strings.Split(content, "\n")
	step := transcriptWindowLines - transcriptOverlap
	if step < 1 {
		step = transcriptWindowLines
	}
	var tags []string
	if tag != "" {
		tags = []string{tag}
	}

	var chunks []store.Chunk
	for start := 0; start < len(lines); start += step {
		end := start + transcriptWindowLines
		if end > len(lines) {
			end = len(lines)
		}
		body := strings.Join(lines[start:end], "\n")
		if strings.TrimSpace(body) == "" {
			if end == len(lines) {
				break
			}
			continue
		}
		// Window index in the anchor keeps IDs distinct and stable for unchanged
		// content; line numbers are 1-based inclusive.
		anchor := fmt.Sprintf("#transcript-w%d", start/step)
		chunks = append(chunks, store.Chunk{
			ID:        ChunkID(relPath, anchor, body),
			Path:      relPath,
			LineStart: start + 1,
			LineEnd:   end,
			Body:      body,
			CreatedAt: mtime.UTC(),
			Tags:      tags,
			Source:    store.SourceTranscript,
		})
		if end == len(lines) {
			break
		}
	}
	return chunks
}
