package log

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	"github.com/ericmann/journal/internal/store"
)

func TestIndexVoiceChunksLandWithVoiceSource(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	const dim = 4
	s, err := store.Open(dbPath, dim)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	content := "---\nsource: voice\n---\n\n# Voice Note\n\n## Notes\n\nhello world from a voice note\n"
	mtime := time.Date(2026, 6, 30, 14, 35, 0, 0, time.UTC)

	fake := embed.NewFake(dim)
	ix := index.NewIndexer(s, fake)

	relPath := "logs/2026-06-30-1435-test.md"
	_, err = IndexVoice(context.Background(), ix, relPath, content, mtime)
	if err != nil {
		t.Fatal(err)
	}

	// Write a temp file so we can get mtime for the store query.
	_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, relPath)), 0o755)
	_ = os.WriteFile(filepath.Join(dir, relPath), []byte(content), 0o644)

	// KNN with SourceVoice filter should find the chunk.
	q := make([]float32, dim)
	candidates, err := s.KNN(context.Background(), q, 10, store.Filter{Sources: []string{store.SourceVoice}})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 {
		t.Fatal("no voice chunks found after IndexVoice")
	}
	for _, c := range candidates {
		if c.Source != store.SourceVoice {
			t.Errorf("chunk source = %q, want %q", c.Source, store.SourceVoice)
		}
	}

	// SourceNote filter should return nothing (no note chunks).
	noteCandidates, err := s.KNN(context.Background(), q, 10, store.Filter{Sources: []string{store.SourceNote}})
	if err != nil {
		t.Fatal(err)
	}
	if len(noteCandidates) != 0 {
		t.Errorf("SourceNote filter returned %d results, want 0", len(noteCandidates))
	}
}
