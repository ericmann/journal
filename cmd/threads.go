package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

var (
	threadsStale bool
	threadsDays  int
	threadsJSON  bool
)

// Thread is one project thread in the threads --json schema.
type Thread struct {
	Project       string `json:"project"`
	LastActivity  string `json:"last_activity"` // RFC3339, or "" if unknown
	Chunks        int    `json:"chunks"`
	OpenQuestions int    `json:"open_questions"`
	Stale         bool   `json:"stale"`
	DaysSince     int    `json:"days_since"` // days since last activity, -1 if unknown
}

type threadsEnvelope struct {
	Threads []Thread `json:"threads"`
}

var threadsCmd = &cobra.Command{
	Use:   "threads",
	Short: "List project threads and their activity (use --stale to surface neglected ones)",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		threads, err := runThreads(cmd.Context(), threadsStale, threadsDays)
		if err != nil {
			if threadsJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				_ = enc.Encode(errorEnvelope{Error: err.Error()})
				return errSilent
			}
			return err
		}
		return renderThreads(out, threads, threadsJSON)
	},
}

// runThreads gathers project activity, marks threads with no activity in the
// last `days` as stale, and (when staleOnly) returns only those.
func runThreads(ctx context.Context, staleOnly bool, days int) ([]Thread, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return threadsFromStore(ctx, cfg, staleOnly, days)
}

// threadsFromStore is the cfg-explicit variant used by both the CLI and the MCP
// server.
func threadsFromStore(ctx context.Context, cfg *config.Config, staleOnly bool, days int) ([]Thread, error) {
	if days <= 0 {
		days = 14
	}
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	infos, err := s.Projects(ctx)
	if err != nil {
		return nil, err
	}
	return buildThreads(infos, staleOnly, days, now()), nil
}

// buildThreads is the pure shaping logic (testable without a store).
func buildThreads(infos []store.ProjectInfo, staleOnly bool, days int, ref time.Time) []Thread {
	cutoff := ref.AddDate(0, 0, -days)
	threads := make([]Thread, 0, len(infos))
	for _, pi := range infos {
		t := Thread{
			Project:       pi.Slug,
			Chunks:        pi.Chunks,
			OpenQuestions: pi.OpenQuestions,
			DaysSince:     -1,
		}
		if pi.LastActivity.IsZero() {
			t.Stale = true // unknown activity counts as stale
		} else {
			t.LastActivity = pi.LastActivity.UTC().Format(time.RFC3339)
			t.DaysSince = int(ref.Sub(pi.LastActivity).Hours() / 24)
			t.Stale = pi.LastActivity.Before(cutoff)
		}
		if staleOnly && !t.Stale {
			continue
		}
		threads = append(threads, t)
	}
	return threads
}

func renderThreads(out io.Writer, threads []Thread, jsonMode bool) error {
	if jsonMode {
		if threads == nil {
			threads = []Thread{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(threadsEnvelope{Threads: threads})
	}
	if len(threads) == 0 {
		fmt.Fprintln(out, "no threads")
		return nil
	}
	for _, t := range threads {
		stale := ""
		if t.Stale {
			stale = "  STALE"
		}
		last := t.LastActivity
		if last == "" {
			last = "never"
		}
		fmt.Fprintf(out, "%-24s  last: %s  chunks: %d  open-questions: %d%s\n",
			t.Project, last, t.Chunks, t.OpenQuestions, stale)
	}
	return nil
}

func init() {
	threadsCmd.Flags().BoolVar(&threadsStale, "stale", false, "only show threads with no activity in --days")
	threadsCmd.Flags().IntVar(&threadsDays, "days", 14, "staleness threshold in days")
	threadsCmd.Flags().BoolVar(&threadsJSON, "json", false, "emit JSON ({threads:[...]})")
	rootCmd.AddCommand(threadsCmd)
}
