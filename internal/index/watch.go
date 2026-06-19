package index

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// TranscriptConfig enables the watcher to also index a transcripts landing zone.
// Transcript files are chunked by sliding window, tagged source=transcript, and
// are NEVER auto-committed (they're ephemeral input, gitignored).
type TranscriptConfig struct {
	Path     string // repo-relative landing zone, slash form (e.g. "transcripts")
	Tag      string // tag applied to every transcript chunk
	AcceptQM bool   // render dropped .qm files to markdown before indexing
	// QMRender converts .qm file content to rendered markdown; nil disables .qm.
	QMRender func(content string) (markdown string, ok bool)
}

// Watcher re-indexes markdown files as they change. It watches the repo tree
// (honoring excludes), debounces bursts of filesystem events, and re-indexes
// only the files that changed — deletions remove the file's chunks.
type Watcher struct {
	root       string
	excludes   []string
	ix         *Indexer
	debounce   time.Duration
	logf       func(string, ...any)
	autoCommit bool
	signCommit bool
	tr         *TranscriptConfig // nil = transcripts disabled
}

// NewWatcher constructs a Watcher. debounce is the quiet period after the last
// event before a re-index runs; logf (may be nil) receives one-line status logs.
// When autoCommit is true, the watcher commits note changes after each re-index
// (no-op outside a git repo); signCommit signs those commits. tr (may be nil)
// enables indexing a transcripts landing zone.
func NewWatcher(root string, excludes []string, ix *Indexer, debounce time.Duration, logf func(string, ...any), autoCommit, signCommit bool, tr *TranscriptConfig) *Watcher {
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Watcher{root: root, excludes: excludes, ix: ix, debounce: debounce, logf: logf, autoCommit: autoCommit, signCommit: signCommit, tr: tr}
}

// isTranscript reports whether a repo-relative path is a transcript file to
// index (under the configured landing zone, with an accepted extension).
func (w *Watcher) isTranscript(rel string) bool {
	if w.tr == nil || w.tr.Path == "" {
		return false
	}
	if rel != w.tr.Path && !strings.HasPrefix(rel, w.tr.Path+"/") {
		return false
	}
	low := strings.ToLower(rel)
	if strings.HasSuffix(low, ".md") || strings.HasSuffix(low, ".txt") {
		return true
	}
	return w.tr.AcceptQM && strings.HasSuffix(low, ".qm")
}

// commit auto-commits note changes if enabled, logging the outcome. Commit
// failures are never fatal — the markdown is already safe on disk.
func (w *Watcher) commit(st Stats) {
	if !w.autoCommit {
		return
	}
	committed, err := AutoCommit(w.root, st, w.signCommit, time.Now())
	switch {
	case err != nil:
		w.logf("auto-commit failed (notes are safe on disk): %v", err)
	case committed:
		w.logf("auto-committed note changes")
	}
}

// Run watches until ctx is cancelled. It performs an initial full index, then
// re-indexes changed files as events arrive.
func (w *Watcher) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := w.addDirs(watcher); err != nil {
		return err
	}

	// Initial pass so the index reflects the current tree on startup.
	files, err := Walk(w.root, w.excludes, time.Time{})
	if err != nil {
		return err
	}
	if st, err := w.ix.IndexFiles(ctx, files); err != nil {
		w.logf("initial index error: %v", err)
	} else {
		w.logf("watching %s — initial index: %d files, %d embedded", w.root, st.FilesScanned, st.Embedded)
		w.commit(st) // commit anything captured before the watcher started
	}
	// Initial transcript pass (separate: never committed).
	if w.tr != nil {
		if trFiles, err := WalkTranscripts(w.root, w.tr.Path, time.Time{}); err == nil && len(trFiles) > 0 {
			rels := make([]string, len(trFiles))
			for i, f := range trFiles {
				rels[i] = f.RelPath
			}
			if st, err := w.ProcessTranscriptChanges(ctx, rels); err == nil {
				w.logf("initial transcript index: %d embedded", st.Embedded)
			}
		}
	}

	pending := map[string]bool{}
	pendingTr := map[string]bool{}
	var timer *time.Timer
	var fire <-chan time.Time
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(w.debounce)
		} else {
			timer.Reset(w.debounce)
		}
		fire = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			// Newly created directories must be watched too.
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					w.addDirUnder(watcher, ev.Name)
					continue
				}
			}
			rel := w.rel(ev.Name)
			if rel == "" {
				continue
			}
			switch {
			case w.isTranscript(rel):
				pendingTr[rel] = true
				arm()
			case isMarkdown(rel) && !w.skipDir(rel):
				pending[rel] = true
				arm()
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			w.logf("watch error: %v", err)

		case <-fire:
			fire = nil
			if changed := keys(pending); len(changed) > 0 {
				pending = map[string]bool{}
				st, err := w.ProcessChanges(ctx, changed)
				if err != nil {
					w.logf("re-index error: %v", err)
				} else {
					w.logf("re-indexed %d file(s): %d embedded, %d updated, %d deleted",
						len(changed), st.Embedded, st.Updated, st.Deleted)
					w.commit(st)
				}
			}
			if changed := keys(pendingTr); len(changed) > 0 {
				pendingTr = map[string]bool{}
				st, err := w.ProcessTranscriptChanges(ctx, changed)
				if err != nil {
					w.logf("transcript index error: %v", err)
				} else {
					w.logf("indexed %d transcript(s): %d embedded, %d updated, %d deleted (not committed)",
						len(changed), st.Embedded, st.Updated, st.Deleted)
				}
			}
		}
	}
}

// ProcessChanges re-indexes the given repo-relative paths. A path whose file no
// longer exists has its chunks removed. Transient per-file errors (read or embed
// failures) are logged and skipped so the rest of the batch still processes; only
// context cancellation bubbles up as an error. It is the unit-testable core of
// the watch loop.
func (w *Watcher) ProcessChanges(ctx context.Context, relPaths []string) (Stats, error) {
	var total Stats
	for _, rel := range relPaths {
		if !isMarkdown(rel) || Excluded(rel, w.excludes) {
			continue
		}
		abs := filepath.Join(w.root, filepath.FromSlash(rel))
		content, err := os.ReadFile(abs)
		switch {
		case errors.Is(err, os.ErrNotExist):
			content = nil // deleted: re-index empty -> removes its chunks
		case err != nil:
			w.logf("watch: skipping %s (read error): %v", rel, err)
			continue
		}
		st, err := w.ix.IndexContent(ctx, rel, string(content))
		if err != nil {
			if ctx.Err() != nil {
				return total, ctx.Err()
			}
			w.logf("watch: skipping %s (index error): %v", rel, err)
			continue
		}
		total.FilesScanned++
		total.Embedded += st.Embedded
		total.Updated += st.Updated
		total.Deleted += st.Deleted
	}
	return total, nil
}

// addDirs walks the tree and watches every non-excluded directory.
func (w *Watcher) addDirs(watcher *fsnotify.Watcher) error {
	return filepath.WalkDir(w.root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel := w.rel(p)
		if rel != "" && w.skipDir(rel) {
			return filepath.SkipDir
		}
		return watcher.Add(p)
	})
}

// skipDir reports whether a directory should not be watched: the git internals
// (which churn on every auto-commit) or any configured exclude. The transcripts
// landing zone is excluded from the NOTE walk but still watched (events route to
// the transcript path), so it is never skipped when transcripts are enabled.
func (w *Watcher) skipDir(rel string) bool {
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return true
	}
	if w.tr != nil && w.tr.Path != "" && (rel == w.tr.Path || strings.HasPrefix(rel, w.tr.Path+"/")) {
		return false
	}
	return Excluded(rel, w.excludes)
}

// ProcessTranscriptChanges indexes transcript files as source=transcript and
// never commits them. A .qm file is rendered to a sibling .md (when a QMRender
// hook is configured) which is then indexed; a deleted file has its chunks
// removed. It is the unit-testable core of the transcript watch path.
func (w *Watcher) ProcessTranscriptChanges(ctx context.Context, relPaths []string) (Stats, error) {
	var total Stats
	if w.tr == nil {
		return total, nil
	}
	for _, rel := range relPaths {
		abs := filepath.Join(w.root, filepath.FromSlash(rel))

		// Render a dropped .qm into a sibling .md, then index that instead.
		if strings.HasSuffix(strings.ToLower(rel), ".qm") {
			if w.tr.QMRender == nil {
				continue
			}
			content, err := os.ReadFile(abs)
			if errors.Is(err, os.ErrNotExist) {
				continue // a removed .qm: nothing to do (its .md is managed separately)
			}
			if err != nil {
				w.logf("watch: skipping transcript %s (read error): %v", rel, err)
				continue
			}
			md, ok := w.tr.QMRender(string(content))
			if !ok {
				w.logf("skipping %s: not a recognizable .qm export", rel)
				continue
			}
			mdRel := strings.TrimSuffix(rel, filepath.Ext(rel)) + ".md"
			mdAbs := filepath.Join(w.root, filepath.FromSlash(mdRel))
			if err := os.WriteFile(mdAbs, []byte(md), 0o644); err != nil {
				w.logf("watch: skipping transcript %s (render write error): %v", rel, err)
				continue
			}
			rel, abs = mdRel, mdAbs
		}

		var mtime time.Time
		content, err := os.ReadFile(abs)
		switch {
		case errors.Is(err, os.ErrNotExist):
			content = nil // deleted: re-index empty -> removes its chunks
			mtime = time.Now()
		case err != nil:
			w.logf("watch: skipping transcript %s (read error): %v", rel, err)
			continue
		default:
			if info, serr := os.Stat(abs); serr == nil {
				mtime = info.ModTime()
			} else {
				mtime = time.Now()
			}
		}
		st, err := w.ix.IndexTranscript(ctx, rel, string(content), mtime, w.tr.Tag)
		if err != nil {
			if ctx.Err() != nil {
				return total, ctx.Err()
			}
			w.logf("watch: skipping transcript %s (index error): %v", rel, err)
			continue
		}
		total.FilesScanned++
		total.Embedded += st.Embedded
		total.Updated += st.Updated
		total.Deleted += st.Deleted
	}
	return total, nil
}

// addDirUnder watches a newly-created directory subtree (best effort).
func (w *Watcher) addDirUnder(watcher *fsnotify.Watcher, dir string) {
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries
		}
		rel := w.rel(p)
		if rel != "" && w.skipDir(rel) {
			return filepath.SkipDir
		}
		_ = watcher.Add(p)
		return nil
	})
}

// rel returns the slash-separated repo-relative path, or "" if outside root.
func (w *Watcher) rel(abs string) string {
	r, err := filepath.Rel(w.root, abs)
	if err != nil || strings.HasPrefix(r, "..") {
		return ""
	}
	return filepath.ToSlash(r)
}

func isMarkdown(rel string) bool {
	return strings.HasSuffix(strings.ToLower(rel), ".md")
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
