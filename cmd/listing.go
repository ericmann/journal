package cmd

import (
	"context"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

var (
	recentTag     []string
	recentProject string
	recentSince   string
	recentJSON    bool

	decisionsProject string
	decisionsSince   string
	decisionsJSON    bool
)

var recentCmd = &cobra.Command{
	Use:   "recent",
	Short: "List the most recent notes (newest first)",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		f := store.Filter{Tags: recentTag, Project: recentProject}
		results, err := runListing(cmd.Context(), f, recentSince, 50)
		if err != nil {
			return renderError(out, err, recentJSON)
		}
		return renderResults(out, results, recentJSON)
	},
}

var decisionsCmd = &cobra.Command{
	Use:   "decisions",
	Short: "List @decision notes (newest first)",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		f := store.Filter{Project: decisionsProject, Markers: []string{note.MarkerDecision}}
		results, err := runListing(cmd.Context(), f, decisionsSince, 100)
		if err != nil {
			return renderError(out, err, decisionsJSON)
		}
		return renderResults(out, results, decisionsJSON)
	},
}

// runListing runs a metadata query (recent/decisions) and converts chunks to
// results. since is parsed and folded into the filter.
func runListing(ctx context.Context, f store.Filter, sinceStr string, limit int) ([]Result, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	since, err := parseSince(sinceStr)
	if err != nil {
		return nil, err
	}
	if since > 0 {
		f.Since = now().Add(-since)
	}
	return listFromStore(ctx, cfg, f, limit)
}

func listFromStore(ctx context.Context, cfg *config.Config, f store.Filter, limit int) ([]Result, error) {
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	chunks, err := s.Recent(ctx, f, limit)
	if err != nil {
		return nil, err
	}
	results := make([]Result, len(chunks))
	for i, c := range chunks {
		results[i] = chunkToResult(c, 0)
	}
	return results, nil
}

func init() {
	recentCmd.Flags().StringArrayVar(&recentTag, "tag", nil, "filter to chunks with this tag (repeatable)")
	recentCmd.Flags().StringVar(&recentProject, "project", "", "filter to a project slug")
	recentCmd.Flags().StringVar(&recentSince, "since", "", "only chunks created within this window (e.g. 1w)")
	recentCmd.Flags().BoolVar(&recentJSON, "json", false, "emit JSON ({results:[...]})")
	rootCmd.AddCommand(recentCmd)

	decisionsCmd.Flags().StringVar(&decisionsProject, "project", "", "filter to a project slug")
	decisionsCmd.Flags().StringVar(&decisionsSince, "since", "", "only decisions within this window (e.g. 4w)")
	decisionsCmd.Flags().BoolVar(&decisionsJSON, "json", false, "emit JSON ({results:[...]})")
	rootCmd.AddCommand(decisionsCmd)
}
