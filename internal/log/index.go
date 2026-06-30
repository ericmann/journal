package log

import (
	"context"
	"time"

	"github.com/ericmann/journal/internal/index"
)

// IndexVoice indexes a voice note file into the store tagged SourceVoice.
// On error the note remains on disk and the caller should warn but not fail.
func IndexVoice(ctx context.Context, ix *index.Indexer, relPath, content string, mtime time.Time) (index.Stats, error) {
	return ix.IndexVoice(ctx, relPath, content, mtime)
}
