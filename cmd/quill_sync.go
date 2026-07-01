package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/ericmann/journal/internal/config"
	jlog "github.com/ericmann/journal/internal/log"
	"github.com/ericmann/journal/internal/quill"
	"github.com/spf13/cobra"
)

var (
	quillSyncFull bool
	quillSyncDB   string
)

var quillSyncCmd = &cobra.Command{
	Use:   "quill-sync",
	Short: "Pull meeting transcripts from the local Quill app into transcripts/",
	Long: "quill-sync reads the local Quill database (read-only) and renders each new\n" +
		"meeting to Markdown in the transcripts/ landing zone, where the indexer picks\n" +
		"it up. It is incremental (a watermark under .journal/ tracks progress); --full\n" +
		"re-renders everything. The Quill app runs on macOS/Windows only — on Linux (or\n" +
		"without Quill installed) there is no database to read.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runQuillSync(cmd.Context(), cfg, cmd.OutOrStdout())
	},
}

func runQuillSync(ctx context.Context, cfg *config.Config, out io.Writer) error {
	if !cfg.Quill.Enabled {
		fmt.Fprintln(out, "quill integration is disabled (quill.enabled: false in .journal/config.yaml)")
		return nil
	}
	if !cfg.Transcripts.Enabled {
		fmt.Fprintln(out, "transcripts are disabled (transcripts.enabled: false); nothing to sync into")
		return nil
	}

	dbPath := cfg.QuillDBPath()
	if quillSyncDB != "" {
		dbPath = quillSyncDB
	}
	if dbPath == "" {
		return fmt.Errorf("no Quill database configured. Quill runs on macOS/Windows only; " +
			"set quill.db_path in .journal/config.yaml (or pass --db) if it lives elsewhere")
	}

	r, err := quill.Open(dbPath)
	if err != nil {
		return err
	}
	defer r.Close()

	journalDir := filepath.Join(cfg.Root(), config.JournalDir)
	since := time.Time{}
	if !quillSyncFull {
		// A corrupt/missing watermark yields zero → a full re-sync, which is safe
		// (files are overwritten by stable filename).
		since, _ = quill.LoadWatermark(journalDir)
	}

	warnf := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
	meetings, err := r.Meetings(ctx, since, warnf)
	if err != nil {
		return err
	}
	if len(meetings) == 0 {
		fmt.Fprintln(out, "quill-sync: no new meetings")
		return nil
	}

	dir := cfg.TranscriptsAbsPath()

	var written int
	latest := since
	for _, m := range meetings {
		if _, err := jlog.Land(dir, m.Filename(), []byte(quill.RenderMarkdown(m))); err != nil {
			return fmt.Errorf("writing %s: %w", m.Filename(), err)
		}
		written++
		if m.Start.After(latest) {
			latest = m.Start
		}
	}
	if !latest.Equal(since) {
		if err := quill.SaveWatermark(journalDir, latest); err != nil {
			fmt.Fprintf(out, "quill-sync: wrote %d file(s) but failed to save watermark: %v\n", written, err)
			return nil
		}
	}
	fmt.Fprintf(out, "quill-sync: rendered %d meeting(s) to %s\n", written, cfg.TranscriptsRelPath())
	fmt.Fprintf(out, "run `journal index` (or the watcher) to embed them\n")
	return nil
}

func init() {
	quillSyncCmd.Flags().BoolVar(&quillSyncFull, "full", false, "re-render all meetings, ignoring the sync watermark")
	quillSyncCmd.Flags().StringVar(&quillSyncDB, "db", "", "override the Quill database path")
	rootCmd.AddCommand(quillSyncCmd)
}
