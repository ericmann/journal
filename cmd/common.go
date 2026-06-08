package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
)

// journalDir is bound to the global --journal-dir flag (see init in root.go). It
// lets every command operate on a journal repo other than the current directory.
var journalDir string

// JournalDirEnv is the environment variable equivalent of --journal-dir, so you
// can point at one journal from anywhere without a flag (e.g. in an alias/shell).
const JournalDirEnv = "JOURNAL_DIR"

// resolveStart returns where to look for the journal repo: the --journal-dir
// flag, else $JOURNAL_DIR, else the current directory. ~ is expanded.
func resolveStart() string {
	if s := strings.TrimSpace(journalDir); s != "" {
		return expandTilde(s)
	}
	if s := strings.TrimSpace(os.Getenv(JournalDirEnv)); s != "" {
		return expandTilde(s)
	}
	return "."
}

// resolveRoot finds the journal root (nearest .journal) from resolveStart().
func resolveRoot() (string, error) {
	return config.FindRepoRoot(resolveStart())
}

// expandTilde expands a leading ~ to the user's home directory.
func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// hintOllama augments an Ollama-unreachable error with actionable setup steps.
// Indexing, search, and synthesis all need a local Ollama; when it can't be
// reached, a raw "connection refused" is unfriendly to a new user, so we point
// them at the fix. Other errors pass through unchanged.
func hintOllama(cfg *config.Config, err error) error {
	if err == nil || !errors.Is(err, embed.ErrUnreachable) {
		return err
	}
	return fmt.Errorf("%w\n\n"+
		"journal needs a local Ollama for embeddings. To set it up:\n"+
		"  1. install Ollama:        https://ollama.com\n"+
		"  2. pull the embed model:  ollama pull %s\n"+
		"  3. verify:                journal doctor\n"+
		"(Ollama is expected at %s — change ollama_base_url in .journal/config.yaml if it runs elsewhere.)",
		err, cfg.EmbedModel, cfg.OllamaBaseURL)
}

// loadConfig resolves the repo root (honoring --journal-dir / $JOURNAL_DIR, else
// the current directory) and loads config.
func loadConfig() (*config.Config, error) {
	return loadConfigFrom(resolveStart())
}

// loadConfigFrom resolves the repo root by walking up from start and loads
// config. Used by `mcp --repo` to bind to a specific workspace.
func loadConfigFrom(start string) (*config.Config, error) {
	root, err := config.FindRepoRoot(start)
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
