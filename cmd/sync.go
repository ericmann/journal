package cmd

import (
	"context"
	"errors"
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
	Short: "Back up notes to (and from) the configured git remote (opt-in)",
	Long: "sync commits any pending note changes, then reconciles the current branch\n" +
		"with its upstream: it pushes when ahead, pulls and re-indexes when behind, and\n" +
		"handles a divergence per the sync_conflict setting. With no upstream configured\n" +
		"it is a no-op.\n\n" +
		"sync is OFF by default — set `sync_enabled: true` in .journal/config.yaml to use\n" +
		"it. Designed to be run from an hourly cron via the generated .journal/sync.sh.\n" +
		"See docs/SYNC.md for setup and conflict tuning.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runSync(cmd.Context(), cfg, newEmbedder(cfg), syncDryRun, cmd.OutOrStdout())
	},
}

// conflictStrategy maps a sync_conflict config value to a vcs.Merge strategy.
func conflictStrategy(mode string) string {
	switch mode {
	case config.SyncConflictPreferUpstream:
		return "theirs"
	case config.SyncConflictPreferLocal:
		return "ours"
	default: // SyncConflictManual
		return ""
	}
}

// runSync reconciles the repo with its upstream. It is embedder-agnostic so the
// re-index after a pull can use a fake in tests. Push/merge failures are
// returned as errors so an unattended cron run exits non-zero and is noticed;
// having no upstream is a clean no-op.
func runSync(ctx context.Context, cfg *config.Config, e embed.Embedder, dryRun bool, out io.Writer) error {
	root := cfg.Root()
	if !cfg.SyncEnabled {
		fmt.Fprintln(out, "sync is disabled (sync_enabled: false in .journal/config.yaml).")
		fmt.Fprintln(out, "It backs notes up to a git remote and can rewrite local history on a")
		fmt.Fprintln(out, "divergence, so it is opt-in. To enable it, set `sync_enabled: true` and")
		fmt.Fprintln(out, "review the conflict modes — see docs/SYNC.md.")
		return nil
	}
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

	// Behind (or diverged): merge the upstream in first per the conflict policy,
	// then re-index so the store reflects the pulled notes.
	if behind > 0 {
		if err := vcs.Merge(root, conflictStrategy(cfg.SyncConflict)); err != nil {
			if errors.Is(err, vcs.ErrMergeConflict) {
				return fmt.Errorf("local and %s have diverged with conflicts; the merge was aborted "+
					"(no changes lost). Resolve it by hand with `git pull` (or set sync_conflict to "+
					"prefer-upstream/prefer-local). See docs/SYNC.md", upstream)
			}
			return err
		}
		fmt.Fprintf(out, "merged %s (%s)\n", upstream, cfg.SyncConflict)
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
		return "merge upstream (per sync_conflict), re-index, then push"
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
