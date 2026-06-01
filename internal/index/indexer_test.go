package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/store"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func timeZero() time.Time { return time.Time{} }

func newTestIndexer(t *testing.T) (*Indexer, *store.Store, *embed.Fake) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "j.db"), 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	fake := embed.NewFake(16)
	return NewIndexer(s, fake), s, fake
}

const twoBlocks = "# 2026-06-01\n\n## 09:14 #cabot\nfirst block body\n\n## 14:02 @decision\nsecond block body\n"

func TestFirstIndexEmbedsAll(t *testing.T) {
	ix, s, fake := newTestIndexer(t)
	ctx := context.Background()
	st, err := ix.IndexContent(ctx, "daily/d.md", twoBlocks)
	if err != nil {
		t.Fatal(err)
	}
	if st.Embedded != 2 {
		t.Errorf("embedded = %d, want 2", st.Embedded)
	}
	if fake.EmbedTexts != 2 {
		t.Errorf("embed calls = %d, want 2", fake.EmbedTexts)
	}
	if n, _ := s.Count(ctx); n != 2 {
		t.Errorf("stored = %d, want 2", n)
	}
}

func TestReindexUnchangedMakesZeroEmbedCalls(t *testing.T) {
	ix, _, fake := newTestIndexer(t)
	ctx := context.Background()
	if _, err := ix.IndexContent(ctx, "daily/d.md", twoBlocks); err != nil {
		t.Fatal(err)
	}
	before := fake.EmbedTexts

	st, err := ix.IndexContent(ctx, "daily/d.md", twoBlocks)
	if err != nil {
		t.Fatal(err)
	}
	if fake.EmbedTexts != before {
		t.Errorf("re-index made %d new embed calls, want 0", fake.EmbedTexts-before)
	}
	if st.Embedded != 0 || st.Updated != 2 || st.Deleted != 0 {
		t.Errorf("no-op reindex stats = %+v, want embedded=0 updated=2 deleted=0", st)
	}
}

func TestEditOneBlockReembedsOnlyThatBlock(t *testing.T) {
	ix, s, fake := newTestIndexer(t)
	ctx := context.Background()
	if _, err := ix.IndexContent(ctx, "daily/d.md", twoBlocks); err != nil {
		t.Fatal(err)
	}
	before := fake.EmbedTexts

	edited := "# 2026-06-01\n\n## 09:14 #cabot\nfirst block body EDITED\n\n## 14:02 @decision\nsecond block body\n"
	st, err := ix.IndexContent(ctx, "daily/d.md", edited)
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.EmbedTexts - before; got != 1 {
		t.Errorf("re-embed count = %d, want 1 (only the edited block)", got)
	}
	if st.Embedded != 1 || st.Deleted != 1 {
		t.Errorf("edit stats = %+v, want embedded=1 deleted=1 (old chunk id removed)", st)
	}
	if n, _ := s.Count(ctx); n != 2 {
		t.Errorf("count = %d, want 2 (edited + unchanged)", n)
	}
}

func TestInsertingBlockShiftsLinesWithoutReembed(t *testing.T) {
	ix, s, fake := newTestIndexer(t)
	ctx := context.Background()
	if _, err := ix.IndexContent(ctx, "daily/d.md", twoBlocks); err != nil {
		t.Fatal(err)
	}
	before := fake.EmbedTexts

	// Insert a new first block; the two original blocks shift down but are
	// unchanged, so only the inserted block is embedded.
	withInsert := "# 2026-06-01\n\n## 08:00 #new\ninserted block\n\n## 09:14 #cabot\nfirst block body\n\n## 14:02 @decision\nsecond block body\n"
	if _, err := ix.IndexContent(ctx, "daily/d.md", withInsert); err != nil {
		t.Fatal(err)
	}
	if got := fake.EmbedTexts - before; got != 1 {
		t.Errorf("re-embed count = %d, want 1 (only inserted block)", got)
	}
	// The shifted "first block body" chunk must have refreshed line numbers.
	chunks := Chunk("daily/d.md", withInsert)
	var firstBodyID string
	for _, c := range chunks {
		if c.Body == "first block body" {
			firstBodyID = c.ID
		}
	}
	got, err := s.Get(ctx, firstBodyID)
	if err != nil {
		t.Fatal(err)
	}
	// The "## 09:14" header is now on line 6 (was line 3 before the insert).
	if got.LineStart != 6 {
		t.Errorf("shifted chunk line_start = %d, want 6 (refreshed)", got.LineStart)
	}
}

func TestDeletingBlockRemovesItsRow(t *testing.T) {
	ix, s, _ := newTestIndexer(t)
	ctx := context.Background()
	if _, err := ix.IndexContent(ctx, "daily/d.md", twoBlocks); err != nil {
		t.Fatal(err)
	}
	onlyFirst := "# 2026-06-01\n\n## 09:14 #cabot\nfirst block body\n"
	st, err := ix.IndexContent(ctx, "daily/d.md", onlyFirst)
	if err != nil {
		t.Fatal(err)
	}
	if st.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", st.Deleted)
	}
	if n, _ := s.Count(ctx); n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}

func TestIndexFilesWalksAndEmbeds(t *testing.T) {
	ix, s, _ := newTestIndexer(t)
	root := t.TempDir()
	write(t, root, "daily/2026/06/a.md", twoBlocks)
	write(t, root, "projects/canton/_index.md", "# 2026-06-01\n\n## 10:00 @question\nis it taxable?\n")
	files, err := Walk(root, []string{".journal/**", "reflections/**"}, timeZero())
	if err != nil {
		t.Fatal(err)
	}
	st, err := ix.IndexFiles(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if st.FilesScanned != 2 || st.Embedded != 3 {
		t.Errorf("stats = %+v, want files=2 embedded=3", st)
	}
	if n, _ := s.Count(context.Background()); n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
}
