package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
)

// loadConfig resolves the repo root from the current directory and loads config.
func loadConfig() (*config.Config, error) {
	root, err := config.FindRepoRoot(".")
	if err != nil {
		return nil, err
	}
	return config.Load(root)
}

// newEmbedder builds the live Ollama embedder from config. Tests inject a fake
// Embedder directly into the run* helpers instead of calling this.
func newEmbedder(cfg *config.Config) embed.Embedder {
	return embed.NewOllama(cfg.OllamaBaseURL, cfg.EmbedModel, cfg.Reranker)
}

// parseSince parses a duration with optional d (days) and w (weeks) suffixes in
// addition to Go's standard units (e.g. "2w", "14d", "36h", "90m"). An empty
// string yields a zero duration (no constraint).
func parseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, ok := strings.CutSuffix(s, "w"); ok {
		v, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return time.Duration(v * float64(7*24*time.Hour)), nil
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		v, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return time.Duration(v * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use forms like 2w, 14d, 36h)", s)
	}
	return d, nil
}
