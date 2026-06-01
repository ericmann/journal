// Package config loads and validates the journal's non-secret settings from
// .journal/config.yaml and resolves the repo root. Secrets (the Anthropic API
// key) are read from the environment only and are never stored in config.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// JournalDir is the per-repo metadata directory that marks a journal repo root.
const JournalDir = ".journal"

// ConfigFile is the committed, non-secret settings file inside JournalDir.
const ConfigFile = "config.yaml"

// AnthropicKeyEnv is the only place the synthesis API key is read from.
const AnthropicKeyEnv = "ANTHROPIC_API_KEY"

// Config holds non-secret settings. It is serialized to .journal/config.yaml.
// The Anthropic API key is deliberately NOT a field here — it comes from the
// environment only and must never be written to disk.
type Config struct {
	// EmbedModel is the Ollama embedding model (e.g. qwen3-embedding:4b).
	EmbedModel string `yaml:"embed_model"`
	// Reranker is the Ollama reranking model (e.g. qwen3-reranker).
	Reranker string `yaml:"reranker"`
	// OllamaBaseURL is the local Ollama HTTP endpoint.
	OllamaBaseURL string `yaml:"ollama_base_url"`
	// ChunkStrategy selects how notes are split into chunks. Only "heading"
	// is supported in v1.
	ChunkStrategy string `yaml:"chunk_strategy"`
	// EmbedDim is the embedding vector dimension. It MUST match the model's
	// output dimension; the vec0 table is declared float[EmbedDim]. Validated
	// against the live model at index time.
	EmbedDim int `yaml:"embed_dim"`
	// Excludes are glob patterns (repo-relative) skipped by the indexer.
	Excludes []string `yaml:"excludes"`
	// StorePath is the repo-relative path to the sqlite-vec database.
	StorePath string `yaml:"store_path"`
	// RetrievalInstruction is prefixed to queries when embedding for search.
	RetrievalInstruction string `yaml:"retrieval_instruction"`
	// SynthModel is the Anthropic model used for synthesis jobs.
	SynthModel string `yaml:"synth_model"`
	// SynthMaxTokens caps the synthesis response length.
	SynthMaxTokens int `yaml:"synth_max_tokens"`
	// VoiceProfilePath is the repo-relative markdown file describing the
	// author's writing voice; when present it is injected into synthesis
	// prompts so drafts sound like the author. Optional.
	VoiceProfilePath string `yaml:"voice_profile"`

	// root is the absolute repo root; not serialized.
	root string
}

// Default returns a Config populated with the documented defaults. It is valid.
func Default() Config {
	return Config{
		EmbedModel:           "qwen3-embedding:4b",
		Reranker:             "qwen3-reranker",
		OllamaBaseURL:        "http://localhost:11434",
		ChunkStrategy:        "heading",
		EmbedDim:             1024,
		Excludes:             []string{"reflections/**", ".journal/**"},
		StorePath:            filepath.Join(JournalDir, "index", "journal.db"),
		RetrievalInstruction: "Represent this query for retrieving relevant developer journal notes:",
		SynthModel:           "claude-sonnet-4-6",
		SynthMaxTokens:       4096,
		VoiceProfilePath:     filepath.ToSlash(filepath.Join("docs", "VOICE_PROFILE.md")),
	}
}

// VoiceProfileAbsPath returns the absolute path to the voice profile, or "" if
// none is configured.
func (c *Config) VoiceProfileAbsPath() string {
	if c.VoiceProfilePath == "" {
		return ""
	}
	if filepath.IsAbs(c.VoiceProfilePath) {
		return c.VoiceProfilePath
	}
	return filepath.Join(c.root, c.VoiceProfilePath)
}

// Root returns the absolute repo root this config was loaded from.
func (c *Config) Root() string { return c.root }

// StoreAbsPath returns the absolute path to the sqlite-vec database.
func (c *Config) StoreAbsPath() string {
	if filepath.IsAbs(c.StorePath) {
		return c.StorePath
	}
	return filepath.Join(c.root, c.StorePath)
}

// Validate checks invariants on non-secret settings.
func (c *Config) Validate() error {
	if c.EmbedModel == "" {
		return errors.New("embed_model must not be empty")
	}
	if c.Reranker == "" {
		return errors.New("reranker must not be empty")
	}
	if c.OllamaBaseURL == "" {
		return errors.New("ollama_base_url must not be empty")
	}
	if c.ChunkStrategy != "heading" {
		return fmt.Errorf("chunk_strategy %q unsupported (only \"heading\")", c.ChunkStrategy)
	}
	if c.EmbedDim <= 0 {
		return fmt.Errorf("embed_dim must be > 0, got %d", c.EmbedDim)
	}
	if c.StorePath == "" {
		return errors.New("store_path must not be empty")
	}
	if c.SynthModel == "" {
		return errors.New("synth_model must not be empty")
	}
	if c.SynthMaxTokens <= 0 {
		return fmt.Errorf("synth_max_tokens must be > 0, got %d", c.SynthMaxTokens)
	}
	return nil
}

// Marshal serializes the config to YAML (non-secret fields only).
func (c *Config) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

// Load reads .journal/config.yaml under root, applying defaults for any missing
// keys, then validates. A missing config file is not an error — defaults apply.
func Load(root string) (*Config, error) {
	c := Default()
	path := filepath.Join(root, JournalDir, ConfigFile)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// No file: pure defaults.
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	default:
		if err := yaml.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
	}
	c.root = root
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config in %s: %w", path, err)
	}
	return &c, nil
}

// FindRepoRoot walks up from start looking for a directory containing a
// .journal directory, returning the first such directory.
func FindRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(filepath.Join(dir, JournalDir))
		if err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a journal repo (no %s found from %s upward); run `journal init`", JournalDir, start)
		}
		dir = parent
	}
}

// AnthropicAPIKey returns the synthesis key from the environment. It is never
// read from or written to config, and callers must never log it.
func AnthropicAPIKey() (string, error) {
	key := os.Getenv(AnthropicKeyEnv)
	if key == "" {
		return "", fmt.Errorf("%s is not set in the environment", AnthropicKeyEnv)
	}
	return key, nil
}
