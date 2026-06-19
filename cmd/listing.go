package cmd

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

// pathDateRe extracts a YYYY-MM-DD date from a note path like
// daily/2026/06/2026-06-15.md or projects/foo/notes/2026-06-15.md.
var pathDateRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// dateFromPath returns the YYYY-MM-DD date found in path, or "" if none.
func dateFromPath(path string) string {
	return pathDateRe.FindString(path)
}

// decisionsStatement returns the statement text from a (possibly collapsed)
// chunk body: text before the first " Rationale:" occurrence, or the full text.
func decisionsStatement(s string) string {
	if idx := strings.Index(s, " Rationale:"); idx != -1 {
		return strings.TrimSpace(s[:idx])
	}
	return strings.TrimSpace(s)
}

// decisionsRationale returns the "Rationale: ..." substring from a collapsed
// body, or "" if not present.
func decisionsRationale(s string) string {
	if idx := strings.Index(s, " Rationale:"); idx != -1 {
		return strings.TrimSpace(s[idx+1:])
	}
	return ""
}

// renderDecisions renders decisions as a dated, statement-first numbered list.
// In JSON mode it delegates to renderResults for the stable {results:[...]} envelope.
func renderDecisions(out io.Writer, results []Result, jsonMode bool) error {
	if jsonMode {
		return renderResults(out, results, true)
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "no decisions — capture one with `journal decide \"…\"` (decisions appear once indexed: `journal index` or the watcher)")
		return nil
	}
	for i, r := range results {
		date := dateFromPath(r.Path)
		stmt := decisionsStatement(r.Snippet)
		rationale := decisionsRationale(r.Snippet)
		fmt.Fprintf(out, "%2d. %-12s  %s\n", i+1, date, r.Citation())
		fmt.Fprintf(out, "    %s\n", stmt)
		if rationale != "" {
			fmt.Fprintf(out, "    %s\n", rationale)
		}
	}
	return nil
}

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
		return renderDecisions(out, results, decisionsJSON)
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
