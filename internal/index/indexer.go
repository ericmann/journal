package index

import (
	"context"
	"fmt"
	"time"

	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/store"
)

// Stats summarizes an indexing run.
type Stats struct {
	FilesScanned int
	Embedded     int // chunks newly embedded (Ollama embed calls, by chunk)
	Updated      int // existing chunks whose line numbers were refreshed
	Deleted      int // chunks removed because their content disappeared
}

// Indexer keeps the store in sync with the markdown corpus, embedding only
// chunks whose content hash is new.
type Indexer struct {
	store    *store.Store
	embedder embed.Embedder
	now      func() time.Time
}

// NewIndexer constructs an Indexer over the given store and embedder.
func NewIndexer(s *store.Store, e embed.Embedder) *Indexer {
	return &Indexer{store: s, embedder: e, now: time.Now}
}

// IndexFiles synchronizes the given files into the store. For each file it
// chunks the content, refreshes line numbers for unchanged chunks (no embed),
// embeds+upserts new/changed chunks, and deletes chunks whose ids disappeared.
func (ix *Indexer) IndexFiles(ctx context.Context, files []File) (Stats, error) {
	var st Stats
	for _, f := range files {
		content, err := ReadFile(f)
		if err != nil {
			return st, fmt.Errorf("reading %s: %w", f.RelPath, err)
		}
		fs, err := ix.indexOne(ctx, f.RelPath, content)
		if err != nil {
			return st, err
		}
		st.FilesScanned++
		st.Embedded += fs.Embedded
		st.Updated += fs.Updated
		st.Deleted += fs.Deleted
	}
	return st, nil
}

// IndexContent indexes a single file's content (used by tests and the watcher).
func (ix *Indexer) IndexContent(ctx context.Context, relPath, content string) (Stats, error) {
	return ix.indexOne(ctx, relPath, content)
}

func (ix *Indexer) indexOne(ctx context.Context, relPath, content string) (Stats, error) {
	var st Stats
	chunks := Chunk(relPath, content)

	storedIDs, err := ix.store.ChunkIDsByPath(ctx, relPath)
	if err != nil {
		return st, err
	}
	stored := make(map[string]bool, len(storedIDs))
	for _, id := range storedIDs {
		stored[id] = true
	}

	current := make(map[string]bool, len(chunks))
	var toEmbed []store.Chunk
	indexedAt := ix.now().UTC()

	for _, c := range chunks {
		current[c.ID] = true
		if stored[c.ID] {
			// Unchanged content: refresh location only, never re-embed.
			if err := ix.store.UpdateLines(ctx, c.ID, c.LineStart, c.LineEnd, indexedAt); err != nil {
				return st, err
			}
			st.Updated++
		} else {
			c.IndexedAt = indexedAt
			toEmbed = append(toEmbed, c)
		}
	}

	if len(toEmbed) > 0 {
		bodies := make([]string, len(toEmbed))
		for i, c := range toEmbed {
			bodies[i] = embedText(c)
		}
		vecs, err := ix.embedder.Embed(ctx, bodies, "")
		if err != nil {
			return st, fmt.Errorf("embedding %s: %w", relPath, err)
		}
		if len(vecs) != len(toEmbed) {
			return st, fmt.Errorf("embedder returned %d vectors for %d chunks", len(vecs), len(toEmbed))
		}
		for i, c := range toEmbed {
			if err := ix.store.Upsert(ctx, c, vecs[i]); err != nil {
				return st, err
			}
			st.Embedded++
		}
	}

	// Delete chunks that no longer exist in this file.
	var toDelete []string
	for id := range stored {
		if !current[id] {
			toDelete = append(toDelete, id)
		}
	}
	if len(toDelete) > 0 {
		if err := ix.store.Delete(ctx, toDelete...); err != nil {
			return st, err
		}
		st.Deleted += len(toDelete)
	}
	return st, nil
}

// embedText is the text handed to the embedder for a chunk: heading + body, so
// the timestamp/tags/markers in the heading inform the embedding.
func embedText(c store.Chunk) string {
	if c.Heading == "" {
		return c.Body
	}
	return c.Heading + "\n" + c.Body
}
