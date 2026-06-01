package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher re-indexes markdown files as they change. It watches the repo tree
// (honoring excludes), debounces bursts of filesystem events, and re-indexes
// only the files that changed — deletions remove the file's chunks.
type Watcher struct {
	root     string
	excludes []string
	ix       *Indexer
	debounce time.Duration
	logf     func(string, ...any)
}

// NewWatcher constructs a Watcher. debounce is the quiet period after the last
// event before a re-index runs; logf (may be nil) receives one-line status logs.
func NewWatcher(root string, excludes []string, ix *Indexer, debounce time.Duration, logf func(string, ...any)) *Watcher {
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Watcher{root: root, excludes: excludes, ix: ix, debounce: debounce, logf: logf}
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
	}

	pending := map[string]bool{}
	var timer *time.Timer
	var fire <-chan time.Time

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
			if rel == "" || !isMarkdown(rel) || Excluded(rel, w.excludes) {
				continue
			}
			pending[rel] = true
			if timer == nil {
				timer = time.NewTimer(w.debounce)
			} else {
				timer.Reset(w.debounce)
			}
			fire = timer.C

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			w.logf("watch error: %v", err)

		case <-fire:
			changed := keys(pending)
			pending = map[string]bool{}
			fire = nil
			st, err := w.ProcessChanges(ctx, changed)
			if err != nil {
				w.logf("re-index error: %v", err)
				continue
			}
			w.logf("re-indexed %d file(s): %d embedded, %d updated, %d deleted",
				len(changed), st.Embedded, st.Updated, st.Deleted)
		}
	}
}

// ProcessChanges re-indexes the given repo-relative paths. A path whose file no
// longer exists has its chunks removed. It is the unit-testable core of the
// watch loop.
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
			return total, fmt.Errorf("reading %s: %w", rel, err)
		}
		st, err := w.ix.IndexContent(ctx, rel, string(content))
		if err != nil {
			return total, err
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
		if rel != "" && Excluded(rel, w.excludes) {
			return filepath.SkipDir
		}
		return watcher.Add(p)
	})
}

// addDirUnder watches a newly-created directory subtree (best effort).
func (w *Watcher) addDirUnder(watcher *fsnotify.Watcher, dir string) {
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries
		}
		rel := w.rel(p)
		if rel != "" && Excluded(rel, w.excludes) {
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
