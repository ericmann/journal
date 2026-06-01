package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

var (
	indexRebuild  bool
	indexSinceStr string
)

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Embed and index changed notes into the sqlite-vec store",
	Long: "index walks the repo, chunks notes, and embeds only chunks whose content\n" +
		"changed. The store is a disposable cache; --rebuild discards it and re-embeds\n" +
		"everything. --since limits the walk to recently modified files.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		since, err := parseSince(indexSinceStr)
		if err != nil {
			return err
		}
		_, err = runIndex(cmd.Context(), cfg, newEmbedder(cfg), indexOptions{
			rebuild: indexRebuild,
			since:   since,
		}, cmd.OutOrStdout())
		return err
	},
}

type indexOptions struct {
	rebuild bool
	since   time.Duration
}

// runIndex performs an indexing pass. It is embedder-agnostic so tests can pass
// a fake. It returns the run stats.
func runIndex(ctx context.Context, cfg *config.Config, e embed.Embedder, opts indexOptions, out io.Writer) (index.Stats, error) {
	storePath := cfg.StoreAbsPath()
	if opts.rebuild {
		if err := removeDBFiles(storePath); err != nil {
			return index.Stats{}, fmt.Errorf("rebuild: %w", err)
		}
	}
	s, err := store.Open(storePath, cfg.EmbedDim)
	if err != nil {
		return index.Stats{}, err
	}
	defer s.Close()

	var since time.Time
	if opts.since > 0 {
		since = time.Now().Add(-opts.since)
	}
	files, err := index.Walk(cfg.Root(), cfg.Excludes, since)
	if err != nil {
		return index.Stats{}, err
	}

	start := time.Now()
	st, err := index.NewIndexer(s, e).IndexFiles(ctx, files)
	if err != nil {
		return st, fmt.Errorf("indexing: %w", err)
	}
	fmt.Fprintf(out, "indexed %d files in %s: %d embedded, %d updated, %d deleted\n",
		st.FilesScanned, time.Since(start).Round(time.Millisecond), st.Embedded, st.Updated, st.Deleted)
	return st, nil
}

// removeDBFiles deletes the sqlite database and its sidecar files so the next
// open rebuilds from scratch.
func removeDBFiles(path string) error {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func init() {
	indexCmd.Flags().BoolVar(&indexRebuild, "rebuild", false, "discard the index and re-embed everything")
	indexCmd.Flags().StringVar(&indexSinceStr, "since", "", "only index files modified within this window (e.g. 2w, 14d, 36h)")
	rootCmd.AddCommand(indexCmd)
}
