package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/store"
)

// errEmbedder wraps Fake but returns a configurable error for the first failN
// Embed calls, then delegates to Fake. Used to simulate transient Ollama EOF.
type errEmbedder struct {
	*embed.Fake
	failN int
	calls int
}

func (e *errEmbedder) Embed(ctx context.Context, texts []string, instruction string) ([][]float32, error) {
	e.calls++
	if e.calls <= e.failN {
		return nil, errors.New("transient embed failure (EOF)")
	}
	return e.Fake.Embed(ctx, texts, instruction)
}

func newWatchFixture(t *testing.T) (*Watcher, *store.Store, *embed.Fake, string) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "j.db"), 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	fake := embed.NewFake(16)
	root := t.TempDir()
	w := NewWatcher(root, []string{"reflections/**", ".journal/**"}, NewIndexer(s, fake), 20*time.Millisecond, nil, false, false, nil)
	return w, s, fake, root
}

// newTranscriptFixture builds a watcher with transcripts enabled, returning the
// watcher, store, and repo root.
func newTranscriptFixture(t *testing.T) (*Watcher, *store.Store, string) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "j.db"), 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	root := t.TempDir()
	tc := &TranscriptConfig{
		Path:     "transcripts",
		Tag:      "meeting",
		AcceptQM: true,
		QMRender: func(content string) (string, bool) {
			// Stub renderer: any QMv2 file becomes a tiny markdown doc.
			if !strings.HasPrefix(content, "QMv2") {
				return "", false
			}
			return "# Imported\n\nrendered transcript body\n", true
		},
	}
	w := NewWatcher(root, []string{"reflections/**", ".journal/**", "transcripts/**"},
		NewIndexer(s, embed.NewFake(16)), 20*time.Millisecond, nil, false, false, tc)
	return w, s, root
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

func TestProcessTranscriptChangesIndexesAsTranscript(t *testing.T) {
	w, s, root := newTranscriptFixture(t)
	ctx := context.Background()
	mustWrite(t, filepath.Join(root, "transcripts", "m.md"),
		strings.Repeat("transcript line\n", 50))

	st, err := w.ProcessTranscriptChanges(ctx, []string{"transcripts/m.md"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Embedded == 0 {
		t.Fatal("expected transcript chunks embedded")
	}
	// Stored as source=transcript, not as notes.
	tr, err := s.Recent(ctx, store.Filter{Source: store.SourceTranscript}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr) == 0 || tr[0].Tags[0] != "meeting" {
		t.Errorf("transcript chunks missing or untagged: %+v", tr)
	}
	notes, _ := s.Recent(ctx, store.Filter{Source: store.SourceNote}, 0)
	if len(notes) != 0 {
		t.Errorf("transcript should not be stored as notes, got %d", len(notes))
	}
}

func TestProcessTranscriptChangesRendersQM(t *testing.T) {
	w, s, root := newTranscriptFixture(t)
	ctx := context.Background()
	mustWrite(t, filepath.Join(root, "transcripts", "drop.qm"), "QMv2\n{\"id\":\"x\"}")

	st, err := w.ProcessTranscriptChanges(ctx, []string{"transcripts/drop.qm"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Embedded == 0 {
		t.Error("rendered .qm should produce indexed chunks")
	}
	// The renderer wrote a sibling .md.
	if _, err := os.Stat(filepath.Join(root, "transcripts", "drop.md")); err != nil {
		t.Errorf(".qm should render a sibling .md: %v", err)
	}
	if n, _ := s.Count(ctx); n == 0 {
		t.Error("no chunks stored from .qm import")
	}
}

func TestTranscriptSkipDirStillWatched(t *testing.T) {
	w, _, _ := newTranscriptFixture(t)
	// transcripts/** is in the note excludes, but the transcripts dir must still
	// be watched (events route to the transcript path).
	if w.skipDir("transcripts") {
		t.Error("transcripts dir should be watched, not skipped")
	}
	if !w.skipDir("reflections") {
		t.Error("reflections should still be skipped")
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
	off := NewWatcher(root, nil, nil, 10*time.Millisecond, nil, false, false, nil)
	off.commit(Stats{Embedded: 1})
	if n := strings.TrimSpace(gitOut(t, root, "rev-list", "--all", "--count")); n != "0" {
		t.Errorf("commit happened with autoCommit off: %s commits", n)
	}

	// autoCommit enabled -> the note is committed.
	on := NewWatcher(root, nil, nil, 10*time.Millisecond, nil, true, false, nil)
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

// TestProcessChangesContinuesOnEmbedError verifies that a transient embed
// failure for one file does not abort processing of the remaining files in the
// batch: the error is logged, the failed file is skipped, and the watcher loop
// keeps running.
func TestProcessChangesContinuesOnEmbedError(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "j.db"), 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	var logs []string
	logf := func(msg string, args ...any) {
		logs = append(logs, fmt.Sprintf(msg, args...))
	}

	// Fail only the first Embed call to simulate a transient Ollama EOF on a.md.
	ee := &errEmbedder{Fake: embed.NewFake(16), failN: 1}
	root := t.TempDir()
	w := NewWatcher(root, nil, NewIndexer(s, ee), 20*time.Millisecond, logf, false, false, nil)

	write(t, root, "daily/a.md", "# 2026-06-01\n\n## 09:00\nalpha chunk\n")
	write(t, root, "daily/b.md", "# 2026-06-01\n\n## 10:00\nbeta chunk\n")

	ctx := context.Background()
	// a.md is processed first; its embed call fails. b.md should still succeed.
	st, err := w.ProcessChanges(ctx, []string{"daily/a.md", "daily/b.md"})
	if err != nil {
		t.Fatalf("ProcessChanges returned %v; want nil (log-and-continue)", err)
	}
	// Only b.md succeeded.
	if st.FilesScanned != 1 {
		t.Errorf("FilesScanned = %d, want 1 (only the successful file)", st.FilesScanned)
	}
	if st.Embedded != 1 {
		t.Errorf("Embedded = %d, want 1", st.Embedded)
	}
	// A log message must mention the failed file.
	mentioned := false
	for _, l := range logs {
		if strings.Contains(l, "daily/a.md") {
			mentioned = true
			break
		}
	}
	if !mentioned {
		t.Errorf("no log entry mentioning daily/a.md; got logs: %v", logs)
	}
}

// TestWatchDebounceCoalescesRapidWrites verifies that multiple rapid writes to
// the same file within the debounce window result in a single re-index pass,
// not one pass per event.
func TestWatchDebounceCoalescesRapidWrites(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "j.db"), 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	fake := embed.NewFake(16)
	root := t.TempDir()

	// Longer debounce (150 ms) so all rapid writes clearly fall within one window.
	w := NewWatcher(root, nil, NewIndexer(s, fake), 150*time.Millisecond, nil, false, false, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := os.MkdirAll(filepath.Join(root, "daily"), 0o755); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Let the initial (empty-dir) index pass finish.
	time.Sleep(80 * time.Millisecond)
	before := fake.EmbedTexts

	// Write the same file 5 times in rapid succession (≈25 ms total),
	// well within the 150 ms debounce window.
	for i := 0; i < 5; i++ {
		write(t, root, "daily/d.md", fmt.Sprintf("# 2026-06-01\n\n## s%d\ntext\n", i))
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for the debounce to fire and the single re-index to complete.
	time.Sleep(500 * time.Millisecond)

	// All 5 writes should have been coalesced: the final content has one chunk,
	// so we expect at most a small number of embed calls (not 5).
	if got := fake.EmbedTexts - before; got > 3 {
		t.Errorf("5 rapid writes caused %d embed calls; debounce should coalesce to ≤3", got)
	}

	cancel()
	<-done
}
