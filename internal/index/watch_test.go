package index

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	w := NewWatcher(root, []string{"reflections/**", ".journal/**"}, NewIndexer(s, fake), 20*time.Millisecond, nil, false, false)
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

func TestWatcherCommitGatedAndWorks(t *testing.T) {
	if !haveGit() {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, "daily", "d.md"), "# 2026-06-01\n\n## 09:00\nnote\n")

	// autoCommit disabled -> no commit (safe with zero commits via rev-list).
	off := NewWatcher(root, nil, nil, 10*time.Millisecond, nil, false, false)
	off.commit(Stats{Embedded: 1})
	if n := strings.TrimSpace(gitOut(t, root, "rev-list", "--all", "--count")); n != "0" {
		t.Errorf("commit happened with autoCommit off: %s commits", n)
	}

	// autoCommit enabled -> the note is committed.
	on := NewWatcher(root, nil, nil, 10*time.Millisecond, nil, true, false)
	on.commit(Stats{Embedded: 1})
	if n := strings.TrimSpace(gitOut(t, root, "rev-list", "--all", "--count")); n != "1" {
		t.Errorf("expected exactly 1 auto-commit, got %s", n)
	}
	if msg := gitOut(t, root, "log", "-1", "--pretty=%s"); !strings.Contains(msg, "notes") {
		t.Errorf("commit message lacks flair: %q", msg)
	}
}

func haveGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func TestWatcherHelpers(t *testing.T) {
	w, _, _, root := newWatchFixture(t)
	if w.rel(filepath.Join(root, "daily", "d.md")) != "daily/d.md" {
		t.Error("rel should produce slash repo-relative path")
	}
	if w.rel("/somewhere/else.md") != "" {
		t.Error("rel should return empty for a path outside root")
	}
	if !isMarkdown("a/b.MD") || isMarkdown("a/b.txt") {
		t.Error("isMarkdown wrong")
	}
	got := keys(map[string]bool{"a": true, "b": true})
	if len(got) != 2 {
		t.Errorf("keys = %v", got)
	}
}

// TestWatchRunPicksUpNewSubdirectory covers watching directories created after
// the watcher starts (the addDirUnder path).
func TestWatchRunPicksUpNewSubdirectory(t *testing.T) {
	w, s, _, root := newWatchFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	// Create a brand-new nested directory and a note inside it.
	if err := os.MkdirAll(filepath.Join(root, "projects", "newproj", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // let the watcher add the new dirs
	write(t, root, "projects/newproj/notes/2026-06-01.md", "# 2026-06-01\n\n## 09:00 @decision\nnew project note\n")

	deadline := time.After(5 * time.Second)
	for {
		if n, _ := s.Count(ctx); n == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("watcher did not index a note in a newly-created subdir within 5s")
		case <-time.After(25 * time.Millisecond):
		}
	}
	cancel()
	<-done
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
