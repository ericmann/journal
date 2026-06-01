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

func newWatchFixture(t *testing.T) (*Watcher, *store.Store, *embed.Fake, string) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "j.db"), 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	fake := embed.NewFake(16)
	root := t.TempDir()
	w := NewWatcher(root, []string{"reflections/**", ".journal/**"}, NewIndexer(s, fake), 20*time.Millisecond, nil)
	return w, s, fake, root
}

func TestProcessChangesIndexesEditsAndDeletes(t *testing.T) {
	w, s, fake, root := newWatchFixture(t)
	ctx := context.Background()
	write(t, root, "daily/d.md", "# 2026-06-01\n\n## 09:00\nalpha\n\n## 10:00\nbeta\n")

	st, err := w.ProcessChanges(ctx, []string{"daily/d.md"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Embedded != 2 {
		t.Errorf("embedded = %d, want 2", st.Embedded)
	}

	// Edit one block: only that block re-embeds.
	before := fake.EmbedTexts
	write(t, root, "daily/d.md", "# 2026-06-01\n\n## 09:00\nalpha EDITED\n\n## 10:00\nbeta\n")
	if _, err := w.ProcessChanges(ctx, []string{"daily/d.md"}); err != nil {
		t.Fatal(err)
	}
	if got := fake.EmbedTexts - before; got != 1 {
		t.Errorf("edit re-embedded %d, want 1", got)
	}

	// Delete the file: its chunks are removed.
	if err := os.Remove(filepath.Join(root, "daily", "d.md")); err != nil {
		t.Fatal(err)
	}
	st, err = w.ProcessChanges(ctx, []string{"daily/d.md"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Deleted != 2 {
		t.Errorf("delete removed %d chunks, want 2", st.Deleted)
	}
	if n, _ := s.Count(ctx); n != 0 {
		t.Errorf("count after delete = %d, want 0", n)
	}
}

func TestProcessChangesSkipsExcludedAndNonMarkdown(t *testing.T) {
	w, _, fake, root := newWatchFixture(t)
	write(t, root, "reflections/r.md", "# 2026-06-01\n\n## 09:00\nignored\n")
	write(t, root, "notes.txt", "not markdown")
	st, err := w.ProcessChanges(context.Background(), []string{"reflections/r.md", "notes.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if st.FilesScanned != 0 || fake.EmbedTexts != 0 {
		t.Errorf("excluded/non-md were processed: scanned=%d embeds=%d", st.FilesScanned, fake.EmbedTexts)
	}
}

// TestWatchRunReindexesOnEdit exercises the real fsnotify loop end to end.
func TestWatchRunReindexesOnEdit(t *testing.T) {
	w, s, _, root := newWatchFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "daily"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Give the watcher a moment to set up, then create a note.
	time.Sleep(50 * time.Millisecond)
	write(t, root, "daily/d.md", "# 2026-06-01\n\n## 09:00\nwatched note\n")

	deadline := time.After(5 * time.Second)
	for {
		if n, _ := s.Count(ctx); n == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("watcher did not index the new file within 5s")
		case <-time.After(25 * time.Millisecond):
		}
	}
	cancel()
	<-done
}
