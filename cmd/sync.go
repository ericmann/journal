package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	"github.com/ericmann/journal/internal/vcs"
	"github.com/spf13/cobra"
)

var syncDryRun bool

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Back up notes to (and from) the configured git remote",
	Long: "sync commits any pending note changes, then reconciles the current branch\n" +
		"with its upstream: it pushes when ahead, pulls and re-indexes when behind, and\n" +
		"auto-merges a divergence preferring the upstream copy on conflict. With no\n" +
		"upstream configured it is a no-op. Designed to be run from an hourly cron via\n" +
		"the generated .journal/sync.sh — see the repo README.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runSync(cmd.Context(), cfg, newEmbedder(cfg), syncDryRun, cmd.OutOrStdout())
	},
}

// runSync reconciles the repo with its upstream. It is embedder-agnostic so the
// re-index after a pull can use a fake in tests. Push/merge failures are
// returned as errors so an unattended cron run exits non-zero and is noticed;
// having no upstream is a clean no-op.
func runSync(ctx context.Context, cfg *config.Config, e embed.Embedder, dryRun bool, out io.Writer) error {
	root := cfg.Root()
	if !vcs.Available() {
		return fmt.Errorf("git is not installed; cannot sync")
	}
	if !vcs.IsRepoRoot(root) {
		return fmt.Errorf("%s is not a git repository root; nothing to sync", root)
	}

	// Sweep up any hand-edited notes the watcher/capture didn't commit so the
	// backup is complete. The markdown is always safe on disk regardless.
	if cfg.GitAutocommit && !dryRun {
		committed, err := vcs.CommitAll(root, index.SyncCommitMessage(time.Now()), cfg.GitAutocommitSign)
		if err != nil {
			fmt.Fprintf(out, "auto-commit skipped (notes are safe on disk): %v\n", err)
		} else if committed {
			fmt.Fprintln(out, "committed pending note changes")
		}
	}

	upstream, ok := vcs.Upstream(root)
	if !ok {
		fmt.Fprintln(out, "no upstream configured for the current branch; nothing to sync")
		return nil
	}

	if err := vcs.Fetch(root); err != nil {
		return err
	}
	ahead, behind, err := vcs.AheadBehind(root)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s: %d ahead, %d behind\n", upstream, ahead, behind)

	switch {
	case ahead == 0 && behind == 0:
		fmt.Fprintf(out, "up to date with %s\n", upstream)
		return nil
	case dryRun:
		fmt.Fprintf(out, "[dry-run] would %s\n", plannedAction(ahead, behind))
		return nil
	}

	// Behind (or diverged): merge the upstream in first, preferring its copy on
	// conflict, then re-index so the store reflects the pulled notes.
	if behind > 0 {
		if err := vcs.MergePreferUpstream(root); err != nil {
			return err
		}
		fmt.Fprintf(out, "merged %s (upstream preferred on conflict)\n", upstream)
		// Re-index so search reflects the pulled notes. The index is disposable
		// (rebuildable with `journal index`), so a failure here — e.g. Ollama down
		// in an unattended cron — must not abort the backup or block the push.
		if _, err := runIndex(ctx, cfg, e, indexOptions{}, out); err != nil {
			fmt.Fprintf(out, "re-index skipped (run `journal index` later): %v\n", err)
		}
		// The merge may have produced new commits to publish.
		ahead, _, err = vcs.AheadBehind(root)
		if err != nil {
			return err
		}
	}

	if ahead > 0 {
		if err := vcs.Push(root); err != nil {
			return err
		}
		fmt.Fprintf(out, "pushed to %s\n", upstream)
	}
	return nil
}

// plannedAction describes, for --dry-run, what a real run would do given the
// ahead/behind counts.
func plannedAction(ahead, behind int) string {
	switch {
	case behind > 0 && ahead > 0:
		return "merge upstream (preferring it on conflict), re-index, then push"
	case behind > 0:
		return "pull and re-index"
	default:
		return "push"
	}
}

func init() {
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "report what sync would do without pushing, pulling, or committing")
	rootCmd.AddCommand(syncCmd)
}
