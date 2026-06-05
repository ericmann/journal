package index

import (
	"context"
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
	tr, err := s.Recent(ctx, store.Filter{Source: store.SourceTranscript}, 0)
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
