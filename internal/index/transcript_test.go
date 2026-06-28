package index

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/store"
)

func TestChunkTranscriptWindowsAndTags(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString("\n")
	}
	mtime := time.Date(2026, 6, 5, 14, 30, 0, 0, time.UTC)
	chunks := ChunkTranscript("transcripts/m.md", b.String(), mtime, "meeting")

	if len(chunks) < 2 {
		t.Fatalf("100 lines should produce multiple windows, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.Source != store.SourceTranscript {
			t.Errorf("chunk source = %q, want transcript", c.Source)
		}
		if len(c.Tags) != 1 || c.Tags[0] != "meeting" {
			t.Errorf("chunk tags = %v, want [meeting]", c.Tags)
		}
		if !c.CreatedAt.Equal(mtime) {
			t.Errorf("chunk CreatedAt = %v, want %v", c.CreatedAt, mtime)
		}
	}
	// Windows overlap (so context isn't split): window 2 starts before window 1 ends.
	if chunks[1].LineStart > chunks[0].LineEnd {
		t.Errorf("windows should overlap: w0 ends %d, w1 starts %d", chunks[0].LineEnd, chunks[1].LineStart)
	}
	// Stable IDs: re-chunking identical content yields identical IDs.
	again := ChunkTranscript("transcripts/m.md", b.String(), mtime, "meeting")
	for i := range chunks {
		if chunks[i].ID != again[i].ID {
			t.Errorf("chunk %d ID not stable across runs", i)
		}
	}
}

func TestChunkTranscriptSummaryIsItsOwnChunk(t *testing.T) {
	var body strings.Builder
	body.WriteString("---\ntitle: \"CAB\"\nparticipants: [\"SPEAKER_00\", \"SPEAKER_01\"]\ntags: [\"meeting\"]\nsource: whisperx\n---\n\n")
	body.WriteString("# CAB\n\n")
	body.WriteString("## Notes\n\nOverview: we decided to ship Answer Engine Optimization work.\n\n")
	body.WriteString("## Transcript\n\n")
	for i := 0; i < 80; i++ {
		body.WriteString(fmt.Sprintf("**[0:%02d] SPEAKER_00:** utterance number %d here\n\n", i, i))
	}
	chunks := ChunkTranscript("transcripts/cab.md", body.String(), time.Now().UTC(), "meeting")

	// The Notes section is exactly one chunk, with its heading and no frontmatter
	// or transcript bleed.
	var notes *store.Chunk
	for i := range chunks {
		if chunks[i].Heading == "Notes" {
			if notes != nil {
				t.Fatal("expected exactly one Notes chunk")
			}
			notes = &chunks[i]
		}
	}
	if notes == nil {
		t.Fatal("no Notes chunk emitted")
	}
	if !strings.Contains(notes.Body, "Answer Engine Optimization") {
		t.Errorf("Notes chunk missing the summary: %q", notes.Body)
	}
	for _, leak := range []string{"participants:", "source: whisperx", "SPEAKER_00:** utterance"} {
		if strings.Contains(notes.Body, leak) {
			t.Errorf("Notes chunk leaked %q (frontmatter/transcript bleed): %q", leak, notes.Body)
		}
	}
	// The long transcript body is still windowed into multiple chunks.
	windows := 0
	for _, c := range chunks {
		if strings.Contains(c.Body, "SPEAKER_00:** utterance") {
			windows++
		}
	}
	if windows < 2 {
		t.Errorf("transcript body should be windowed into >=2 chunks, got %d", windows)
	}
	// Frontmatter is not indexed at all.
	for _, c := range chunks {
		if strings.Contains(c.Body, "source: whisperx") {
			t.Errorf("frontmatter leaked into a chunk: %q", c.Body)
		}
	}
}

func TestIndexTranscriptEmbedsAndReindexIsNoOp(t *testing.T) {
	ix, s, _ := newTestIndexer(t)
	ctx := context.Background()
	content := strings.Repeat("some transcript line\n", 60)
	mtime := time.Now()

	st, err := ix.IndexTranscript(ctx, "transcripts/m.md", content, mtime, "meeting")
	if err != nil {
		t.Fatal(err)
	}
	if st.Embedded == 0 {
		t.Fatal("expected transcript chunks to be embedded")
	}
	// The stored chunks are tagged source=transcript.
	tr, err := s.Recent(ctx, store.Filter{Sources: []string{store.SourceTranscript}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr) != st.Embedded {
		t.Errorf("stored transcript chunks = %d, embedded = %d", len(tr), st.Embedded)
	}
	// Re-indexing identical content embeds nothing.
	st2, err := ix.IndexTranscript(ctx, "transcripts/m.md", content, mtime, "meeting")
	if err != nil {
		t.Fatal(err)
	}
	if st2.Embedded != 0 {
		t.Errorf("re-index embedded %d, want 0", st2.Embedded)
	}
}
