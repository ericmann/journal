package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/vcs"
	"github.com/spf13/cobra"
)

var (
	dismissProject    string
	dismissBefore     string
	dismissResolution string
	dismissYes        bool
)

var dismissCmd = &cobra.Command{
	Use:   "dismiss",
	Short: "Bulk dismiss open @todos (by project or age) in one commit",
	Long: "dismiss selects all open @todos matching the given filters and rewrites each\n" +
		"to @done <date>, optionally appending a resolution note. Requires --yes or an\n" +
		"interactive confirmation before mutating any files. All rewrites and\n" +
		"re-indexing land in a single auto-commit.\n\n" +
		"Examples:\n" +
		"  journal dismiss --project acme --yes\n" +
		"  journal dismiss --before 4w --resolution \"superseded by new plan\"\n" +
		"  journal dismiss --project janus --before 2w --yes",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if dismissProject == "" && dismissBefore == "" {
			return fmt.Errorf("at least one filter required: --project or --before/--older-than")
		}
		logf := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
		return runBulkDismiss(cmd.Context(), cfg, newEmbedder(cfg),
			dismissProject, dismissBefore, dismissResolution, dismissYes,
			cmd.InOrStdin(), out, logf)
	},
}

// runBulkDismiss selects open todos by project/age filters, confirms with the
// user, rewrites each @todo → @done in source files (grouped per file so each
// file is read and written once), re-indexes, then commits the whole batch in
// a single auto-commit.
func runBulkDismiss(ctx context.Context, cfg *config.Config, e embed.Embedder,
	project, beforeStr, resolution string, yes bool,
	stdin io.Reader, out io.Writer, logf func(string, ...any)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}

	// Parse --before window into a cutoff: todos created BEFORE this time match.
	var cutoff time.Time
	if beforeStr != "" {
		d, err := parseSince(beforeStr)
		if err != nil {
			return err
		}
		if d > 0 {
			cutoff = now().Add(-d)
		}
	}

	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return err
	}
	defer s.Close()

	f := store.Filter{Markers: []string{note.MarkerTodo}, Project: project}
	all, err := s.Recent(ctx, f, 0)
	if err != nil {
		return err
	}

	// Apply the --before cutoff in Go (store.Filter.Since filters >= not <).
	// Chunks with a zero CreatedAt (unknown age) are included; they can't be proven recent.
	var targets []store.Chunk
	for _, c := range all {
		if !cutoff.IsZero() && !c.CreatedAt.IsZero() && !c.CreatedAt.Before(cutoff) {
			continue
		}
		targets = append(targets, c)
	}

	if len(targets) == 0 {
		fmt.Fprintln(out, "no open todos match the filter")
		return nil
	}

	// Show the matched set before asking for confirmation.
	fmt.Fprintf(out, "About to dismiss %d open todo(s):\n", len(targets))
	for _, c := range targets {
		r := chunkToResult(c, 0)
		fmt.Fprintf(out, "  %s", r.Citation())
		if r.Heading != "" {
			fmt.Fprintf(out, "  %s", r.Heading)
		}
		fmt.Fprintln(out)
		if r.Snippet != "" {
			fmt.Fprintf(out, "      %s\n", r.Snippet)
		}
	}

	if !yes {
		fmt.Fprintf(out, "\nDismiss all %d? [y/N] ", len(targets))
		sc := bufio.NewScanner(stdin)
		if !sc.Scan() || !strings.EqualFold(strings.TrimSpace(sc.Text()), "y") {
			fmt.Fprintln(out, "aborted")
			return nil
		}
	}

	// Group by file so each file is read and written once.
	byFile := map[string][]store.Chunk{}
	for _, c := range targets {
		byFile[c.Path] = append(byFile[c.Path], c)
	}

	stamp := now().Format("2006-01-02")
	matched := len(targets)
	dismissed, failed := 0, 0

	ix := index.NewIndexer(s, e)

	for path, chunks := range byFile {
		abs := filepath.Join(cfg.Root(), filepath.FromSlash(path))
		data, err := os.ReadFile(abs)
		if err != nil {
			logf("error reading %s: %v (skipping)", path, err)
			failed += len(chunks)
			continue
		}
		lines := strings.Split(string(data), "\n")

		// Process bottom-up (highest LineStart first) so resolution insertions
		// don't shift the line indices of chunks above the current one.
		sort.Slice(chunks, func(i, j int) bool {
			return chunks[i].LineStart > chunks[j].LineStart
		})

		fileDismissed := 0
		for _, chunk := range chunks {
			var replaced bool
			lines, replaced = applyDone(lines, chunk, stamp, resolution)
			if !replaced {
				logf("warning: %s no longer contains @todo at lines %d-%d (stale index; skipped)",
					path, chunk.LineStart, chunk.LineEnd)
				failed++
				continue
			}
			fileDismissed++
		}

		newContent := strings.Join(lines, "\n")
		if err := os.WriteFile(abs, []byte(newContent), 0o644); err != nil {
			logf("error writing %s: %v", path, err)
			failed += fileDismissed
			continue
		}
		dismissed += fileDismissed

		// Re-index (non-fatal; the watcher or next `journal index` catches up).
		if _, ierr := ix.IndexContent(ctx, path, newContent); ierr != nil {
			logf("re-index skipped for %s (run `journal index` later): %v", path, ierr)
		}
	}

	fmt.Fprintf(out, "matched %d, dismissed %d, failed %d\n", matched, dismissed, failed)

	// Single auto-commit for the entire batch.
	if cfg.GitAutocommit && vcs.IsRepoRoot(cfg.Root()) {
		msg := index.DismissCommitMessage(dismissed, project, beforeStr, now())
		if committed, cerr := vcs.CommitAll(cfg.Root(), msg, cfg.GitAutocommitSign); cerr != nil {
			logf("auto-commit skipped (notes are safe on disk): %v", cerr)
		} else if committed {
			logf("committed ✓")
		}
	}

	return nil
}

func init() {
	dismissCmd.Flags().StringVar(&dismissProject, "project", "", "dismiss todos in this project")
	dismissCmd.Flags().StringVar(&dismissBefore, "before", "", "dismiss todos older than this window (e.g. 4w, 14d, 36h)")
	dismissCmd.Flags().StringVar(&dismissBefore, "older-than", "", "alias for --before")
	dismissCmd.Flags().StringVar(&dismissResolution, "resolution", "", "resolution note appended to each dismissed todo")
	dismissCmd.Flags().BoolVar(&dismissYes, "yes", false, "skip interactive confirmation")
	rootCmd.AddCommand(dismissCmd)
}
