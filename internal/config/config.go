// Package config loads and validates the journal's non-secret settings from
// .journal/config.yaml and resolves the repo root. Secrets (the Anthropic API
// key) are read from the environment only and are never stored in config.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// JournalDir is the per-repo metadata directory that marks a journal repo root.
const JournalDir = ".journal"

// ConfigFile is the committed, non-secret settings file inside JournalDir.
const ConfigFile = "config.yaml"

// AnthropicKeyEnv is the only place the Anthropic synthesis API key is read from.
const AnthropicKeyEnv = "ANTHROPIC_API_KEY"

// OpenAIKeyEnv is where the key for OpenAI-compatible providers (synth and/or
// embeddings) is read from — for OpenRouter/Groq/etc. it holds that provider's
// key. Read from the environment only, never persisted to config.
const OpenAIKeyEnv = "OPENAI_API_KEY"

// HFTokenEnv is the only place the HuggingFace access token is read from. It
// is required only for gated model repos (e.g. pyannote diarization); ungated
// pulls work with no token set.
const HFTokenEnv = "HF_TOKEN"

// SyncConflict modes for the sync_conflict setting.
const (
	// SyncConflictManual aborts a conflicting merge and asks the user to resolve
	// it by hand — the safe default that never discards work.
	SyncConflictManual = "manual"
	// SyncConflictPreferUpstream resolves conflicts toward the remote copy.
	SyncConflictPreferUpstream = "prefer-upstream"
	// SyncConflictPreferLocal resolves conflicts toward the local copy.
	SyncConflictPreferLocal = "prefer-local"
)

// SchemaVersion is the current .journal/config.yaml schema. `journal init` on an
// older repo upgrades it to this in place.
const SchemaVersion = "2.0"

// Synthesis providers for the synth_provider setting.
const (
	// SynthProviderAnthropic sends synthesis prompts to the Anthropic API (cloud).
	SynthProviderAnthropic = "anthropic"
	// SynthProviderOllama runs synthesis against the local Ollama service —
	// note content never leaves the machine.
	SynthProviderOllama = "ollama"
	// SynthProviderOpenAI sends synthesis prompts to any OpenAI-compatible Chat
	// Completions endpoint (OpenAI, OpenRouter, Groq, Together, a local server,
	// …) at synth_openai_base_url. Cloud egress, like anthropic.
	SynthProviderOpenAI = "openai"
)

// Embedding providers for the embed_provider setting.
const (
	// EmbedProviderOllama embeds via the local Ollama service (default).
	EmbedProviderOllama = "ollama"
	// EmbedProviderOpenAI embeds via any OpenAI-compatible /embeddings endpoint
	// (OpenAI, Together, a local server, …) at embed_openai_base_url. NOTE:
	// remote embeddings can't do the Ollama LLM-as-reranker, and the chosen
	// model's vector dimension must match embed_dim (+ a `journal index
	// --rebuild`). Cloud egress for note content.
	EmbedProviderOpenAI = "openai"
)

// Modes for the local_only_mcp setting (only consulted when local_only is true).
const (
	// LocalOnlyMCPBlock disables `journal mcp` under local_only — the
	// conservative default, since a typical MCP client (e.g. Claude Desktop)
	// forwards retrieved note content to a cloud model.
	LocalOnlyMCPBlock = "block"
	// LocalOnlyMCPAllow keeps `journal mcp` available under local_only. This is
	// a user attestation, not something the server can verify: stdio gives no
	// trustworthy client identity, so "allow" means "my MCP client runs a local
	// model" (see docs/CLIENTS.md).
	LocalOnlyMCPAllow = "allow"
)

// Transcript formats for the transcripts.format setting.
const (
	TranscriptFormatAuto     = "auto"
	TranscriptFormatMarkdown = "markdown"
	TranscriptFormatTxt      = "txt"
)

// Transcriber configures the local voice-transcription model provisioned by
// `journal models pull`. The model is downloaded once into ModelDir and
// verified on subsequent pulls (checksum match → no-op).
type Transcriber struct {
	// ModelID is the HuggingFace model identifier (e.g. "Systran/faster-whisper-base.en").
	ModelID string `yaml:"model_id"`
	// Revision is the git ref to pin (e.g. "main" or a commit SHA).
	Revision string `yaml:"revision"`
	// Checksum is the expected SHA-256 hex digest of the downloaded model.bin.
	// Empty means no checksum verification (useful for local development).
	Checksum string `yaml:"checksum"`
	// ModelDir is the directory where model files are stored. ~ is expanded.
	// Defaults to ~/.cache/journal/models.
	ModelDir string `yaml:"model_dir"`
	// Gated marks ModelID as a gated HuggingFace repo requiring terms
	// acceptance and an HF_TOKEN (e.g. pyannote diarization models). Ungated
	// models (the default) leave this false.
	Gated bool `yaml:"gated"`
	// AcceptURL is the HuggingFace terms-acceptance page for a gated ModelID.
	// Shown in the pull failure message and recorded in MODELS.md. Ignored
	// when Gated is false.
	AcceptURL string `yaml:"accept_url"`
	// Filename is the remote/on-disk file name to pull (e.g. "config.yaml"
	// for a pyannote diarization repo). Empty defaults to "model.bin".
	Filename string `yaml:"filename"`
}

// Transcripts configures the meeting-transcript landing zone (populated by
// `journal quill-sync` and/or dropped-in files) and how it is indexed.
type Transcripts struct {
	// Enabled gates the whole transcript feature (a no-op when false).
	Enabled bool `yaml:"enabled"`
	// Path is the repo-relative landing zone for rendered transcripts. Gitignored.
	Path string `yaml:"path"`
	// Format hints how transcript files are parsed: auto | markdown | txt.
	Format string `yaml:"format"`
	// AutoIndex embeds new/modified transcripts as the watcher detects them.
	AutoIndex bool `yaml:"auto_index"`
	// Tag is applied to every transcript-sourced chunk (so `--tag`/source filter
	// and search find them).
	Tag string `yaml:"tag"`
	// LogCaptures, when true, appends a timestamped breadcrumb to the daily file
	// when a transcript is indexed.
	LogCaptures bool `yaml:"log_captures"`
}

// Quill configures pulling meeting transcripts from the local Quill app's
// SQLite database (macOS/Windows only — Quill does not run on Linux).
type Quill struct {
	// Enabled gates `journal quill-sync`.
	Enabled bool `yaml:"enabled"`
	// DBPath is the Quill SQLite database (read-only to us). ~ is expanded.
	DBPath string `yaml:"db_path"`
	// AcceptQMImports renders manually-dropped .qm files in the landing zone.
	AcceptQMImports bool `yaml:"accept_qm_imports"`
}

// LogAudio configures the `journal log` recorder: device selection, scratch
// storage, and the toggle's safety limits (Phase 3).
type LogAudio struct {
	Device     string `yaml:"device"`
	SampleRate int    `yaml:"sample_rate"`
	Channels   int    `yaml:"channels"`
	// Backend selects the ffmpeg input format: "" (auto-detect from GOOS —
	// avfoundation on macOS, pulse on Linux), "pulse", "alsa", or
	// "avfoundation". See audio.ResolveBackend.
	Backend string `yaml:"backend"`
	// TmpDir is where in-progress/scratch recordings are written. ~ is
	// expanded. Empty falls back to <os temp dir>/journal-log.
	TmpDir string `yaml:"tmp_dir"`
	// MaxDuration caps a single recording in seconds; 0 disables the cap.
	MaxDuration int `yaml:"max_duration"`
	// SilenceAutostop enables a safety-net stop after a sustained silence
	// interval (it is not the primary stopping mechanism — the toggle is).
	SilenceAutostop bool `yaml:"silence_autostop"`
	// SilenceDuration is how long (in seconds) a continuous silence interval
	// must last before SilenceAutostop finalizes the recording.
	SilenceDuration int `yaml:"silence_duration"`
	// SilenceNoiseDB is the ffmpeg silencedetect noise floor in dB: audio
	// quieter than this is treated as silence. More negative is quieter
	// (stricter); less negative trips on background noise sooner.
	SilenceNoiseDB int `yaml:"silence_noise_db"`
	// KeepWAV retains the recorded WAV after a successful pipeline run and
	// records its path in the landed note's `audio:` frontmatter. Default:
	// delete the scratch WAV once the note is safely landed.
	KeepWAV bool `yaml:"keep_wav"`
}

// LogTranscriber configures the local transcription backend used by `journal log <audio.wav>`.
type LogTranscriber struct {
	// Backend is the transcription engine: "whisper.cpp" (default).
	Backend string `yaml:"backend"`
	// Model is the model name used by the backend (e.g. "base.en").
	Model string `yaml:"model"`
	// ModelDir is the directory containing model files. ~ is expanded.
	// Defaults to ~/.cache/journal/models if empty.
	ModelDir string `yaml:"model_dir"`
}

// LogShaping configures the LLM shaping step for voice notes.
type LogShaping struct {
	// Enabled gates the LLM shaping call (clean, title, summarize, tag, extract markers).
	Enabled bool `yaml:"enabled"`
	// KeepRawTranscript includes a collapsed <details> block with the raw text.
	KeepRawTranscript bool `yaml:"keep_raw_transcript"`
}

// LogLanding configures where voice notes are written.
type LogLanding struct {
	// Dir is the repo-relative landing zone for voice notes.
	Dir string `yaml:"dir"`
	// BacklinkDaily, when true, appends a one-line breadcrumb to today's daily note.
	BacklinkDaily bool `yaml:"backlink_daily"`
}

// LogConfig configures `journal log` voice-note capture.
type LogConfig struct {
	Audio       LogAudio       `yaml:"audio"`
	Transcriber LogTranscriber `yaml:"transcriber"`
	Shaping     LogShaping     `yaml:"shaping"`
	Landing     LogLanding     `yaml:"landing"`
}

// defaultQuillDBPath returns the per-OS Quill database location (with ~), or ""
// where Quill is unavailable (Linux). ~ resolves on Windows too (AppData lives
// under the home dir).
func defaultQuillDBPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "~/Library/Application Support/Quill/quill.db"
	case "windows":
		return "~/AppData/Roaming/Quill/quill.db"
	default:
		return ""
	}
}

// Config holds non-secret settings. It is serialized to .journal/config.yaml.
// The Anthropic API key is deliberately NOT a field here — it comes from the
// environment only and must never be written to disk.
type Config struct {
	// EmbedProvider selects the embedding backend: "ollama" (local, default) or
	// "openai" (any OpenAI-compatible /embeddings endpoint; needs OPENAI_API_KEY).
	EmbedProvider string `yaml:"embed_provider"`
	// EmbedModel is the Ollama embedding model (e.g. qwen3-embedding:4b), used
	// when embed_provider is "ollama".
	EmbedModel string `yaml:"embed_model"`
	// EmbedOpenAIBaseURL is the OpenAI-compatible API base (incl. version path,
	// e.g. https://api.openai.com/v1) used when embed_provider is "openai".
	EmbedOpenAIBaseURL string `yaml:"embed_openai_base_url"`
	// EmbedOpenAIModel is the embedding model id when embed_provider is "openai"
	// (e.g. "text-embedding-3-small" → set embed_dim: 1536).
	EmbedOpenAIModel string `yaml:"embed_openai_model"`
	// Reranker is optional. When empty, reranking is disabled and search uses
	// vector-KNN order directly. When set, it names the Ollama generate model
	// used for LLM-as-reranker scoring (Ollama has no native rerank API).
	Reranker string `yaml:"reranker"`
	// OllamaBaseURL is the local Ollama HTTP endpoint.
	OllamaBaseURL string `yaml:"ollama_base_url"`
	// ChunkStrategy selects how notes are split into chunks. Only "heading"
	// is supported in v1.
	ChunkStrategy string `yaml:"chunk_strategy"`
	// EmbedDim is the embedding vector dimension. It MUST match the model's
	// output dimension; the vec0 table is declared float[EmbedDim]. Validated
	// against the live model by `journal doctor` and at index time.
	EmbedDim int `yaml:"embed_dim"`
	// Excludes are glob patterns (repo-relative) skipped by the indexer.
	Excludes []string `yaml:"excludes"`
	// StorePath is the repo-relative path to the sqlite-vec database.
	StorePath string `yaml:"store_path"`
	// RetrievalInstruction is prefixed to queries when embedding for search.
	RetrievalInstruction string `yaml:"retrieval_instruction"`
	// SynthProvider selects who runs synthesis jobs and search answers:
	// "anthropic" (cloud, needs ANTHROPIC_API_KEY), "ollama" (local), or
	// "openai" (any OpenAI-compatible Chat Completions endpoint — OpenAI,
	// OpenRouter, Groq, Together, …; needs OPENAI_API_KEY).
	SynthProvider string `yaml:"synth_provider"`
	// SynthModel is the Anthropic model used when synth_provider is "anthropic".
	SynthModel string `yaml:"synth_model"`
	// SynthOllamaModel is the Ollama model used when synth_provider is "ollama".
	SynthOllamaModel string `yaml:"synth_ollama_model"`
	// SynthOpenAIBaseURL is the OpenAI-compatible API base (must end in the
	// version path, e.g. https://openrouter.ai/api/v1) used when synth_provider
	// is "openai". The client POSTs to {base}/chat/completions.
	SynthOpenAIBaseURL string `yaml:"synth_openai_base_url"`
	// SynthOpenAIModel is the model id used when synth_provider is "openai"
	// (e.g. "google/gemma-3-27b-it:free" on OpenRouter, "gpt-4o-mini" on OpenAI).
	SynthOpenAIModel string `yaml:"synth_openai_model"`
	// SynthNumCtx is the context window requested per Ollama synthesis call.
	// Ollama's server default is 4096 and it truncates silently, so we always
	// send num_ctx explicitly. Ignored by the anthropic provider.
	SynthNumCtx int `yaml:"synth_num_ctx"`
	// SynthMaxTokens caps the synthesis response length.
	SynthMaxTokens int `yaml:"synth_max_tokens"`
	// LocalOnly is the cloud-AI egress kill-switch. When true: cloud synthesis
	// is refused (synth_provider must be "ollama"), `journal mcp` is disabled
	// unless local_only_mcp is "allow", and ollama_base_url must point at
	// loopback. It deliberately does NOT touch `journal sync` — that backs up to
	// the user's own remote and stays governed by sync_enabled. See
	// docs/DATA-FLOWS.md.
	LocalOnly bool `yaml:"local_only"`
	// LocalOnlyMCP controls `journal mcp` under local_only: "block" (default)
	// or "allow" — an attestation that the MCP client runs a local model and
	// keeps note content on this machine (see docs/CLIENTS.md). Ignored when
	// local_only is false.
	LocalOnlyMCP string `yaml:"local_only_mcp"`
	// VoiceProfilePath is the repo-relative markdown file describing the
	// author's writing voice; when present it is injected into synthesis
	// prompts so drafts sound like the author. Optional.
	VoiceProfilePath string `yaml:"voice_profile"`
	// GitAutocommit, when true, makes `index`/`index --watch` auto-commit note
	// changes if the repo root is a git work tree (the gitignored index is never
	// committed). A safety net against forgetting to commit a day's work.
	GitAutocommit bool `yaml:"git_autocommit"`
	// GitAutocommitSign signs auto-commits using the repo's git config. Off by
	// default so an unattended watcher doesn't trigger a signing prompt per note.
	GitAutocommitSign bool `yaml:"git_autocommit_sign"`
	// Editor is the command used to compose a note when `journal capture` is run
	// with no text (like git's core.editor). It is run as a shell command so
	// flags work (e.g. "code --wait"). Empty falls back to $JOURNAL_EDITOR,
	// $VISUAL, $EDITOR, then nano.
	Editor string `yaml:"editor"`
	// SyncEnabled gates `journal sync`. It is OFF by default: sync pushes to and
	// pulls from a git remote (and can rewrite local history on a divergence), so
	// it is strictly opt-in. See docs/SYNC.md.
	SyncEnabled bool `yaml:"sync_enabled"`
	// SyncConflict selects how `journal sync` resolves a divergence (both local
	// and remote have new commits): "manual" (default) aborts and asks you to
	// resolve by hand — it never discards work; "prefer-upstream" takes the remote
	// copy on conflict; "prefer-local" keeps the local copy on conflict.
	SyncConflict string `yaml:"sync_conflict"`
	// Transcriber configures the local voice-transcription model.
	Transcriber Transcriber `yaml:"transcriber"`
	// Diarization configures an optional, independently-provisionable
	// speaker-diarization model (e.g. pyannote) for the meeting pipeline.
	// Empty ModelID (the default) means disabled — `journal models pull`
	// skips it and no gated HF pull is attempted until a user configures it.
	Diarization Transcriber `yaml:"diarization"`
	// Transcripts configures the meeting-transcript landing zone + indexing.
	Transcripts Transcripts `yaml:"transcripts"`
	// Quill configures pulling transcripts from the local Quill app database.
	Quill Quill `yaml:"quill"`
	// Log configures `journal log` voice-note capture.
	Log LogConfig `yaml:"log"`
	// SchemaVer records the config schema version (see SchemaVersion). Lets
	// `journal init` detect and upgrade older repos.
	SchemaVer string `yaml:"schema_version"`

	// root is the absolute repo root; not serialized.
	root string
}

// Default returns a Config populated with the documented defaults. It is valid.
func Default() Config {
	return Config{
		EmbedProvider:      EmbedProviderOllama,
		EmbedModel:         "qwen3-embedding:4b",
		EmbedOpenAIBaseURL: "https://api.openai.com/v1",
		EmbedOpenAIModel:   "",
		// Reranking is off by default: Ollama has no native rerank API and there
		// is no official reranker model. Vector KNN with qwen3-embedding is
		// strong on its own. Set this to a generate model (e.g. "qwen3:4b") to
		// enable the LLM-as-reranker precision lift.
		Reranker:      "",
		OllamaBaseURL: "http://localhost:11434",
		ChunkStrategy: "heading",
		// qwen3-embedding:4b outputs 2560-dim vectors. MUST match the model;
		// `journal doctor` reports the model's actual dimension.
		EmbedDim: 2560,
		// reflections/ holds synth output; docs/ holds meta like the voice profile
		// (read directly by synth, not a journal entry); README.md is the generated
		// usage guide. All excluded so they don't pollute search.
		Excludes:             []string{"reflections/**", ".journal/**", "docs/**", "README.md"},
		StorePath:            filepath.Join(JournalDir, "index", "journal.db"),
		RetrievalInstruction: "Represent this query for retrieving relevant developer journal notes:",
		SynthProvider:        SynthProviderAnthropic,
		SynthModel:           "claude-sonnet-4-6",
		// gemma4:12b balances prose quality against memory (~8GB resident while
		// loaded); 64GB machines can step up to gemma4:26b. See docs/SYNTHESIS.md.
		SynthOllamaModel: "gemma4:12b",
		// OpenAI-compatible defaults: base points at OpenAI itself; override for
		// OpenRouter etc. (e.g. https://openrouter.ai/api/v1). Model is empty so
		// the user picks one when selecting this provider. See docs/SYNTHESIS.md.
		SynthOpenAIBaseURL: "https://api.openai.com/v1",
		SynthOpenAIModel:   "",
		SynthNumCtx:        32768,
		SynthMaxTokens:     4096,
		LocalOnly:          false,
		LocalOnlyMCP:       LocalOnlyMCPBlock,
		VoiceProfilePath:   filepath.ToSlash(filepath.Join("docs", "VOICE_PROFILE.md")),
		// Auto-commit note changes during index/watch (no-op outside a git repo);
		// unsigned by default to avoid signing prompts in an unattended watcher.
		GitAutocommit:     true,
		GitAutocommitSign: false,
		// Empty: fall back to $JOURNAL_EDITOR/$VISUAL/$EDITOR, then nano.
		Editor: "",
		// Sync is opt-in; manual conflict handling never discards work.
		SyncEnabled:  false,
		SyncConflict: SyncConflictManual,
		// Transcriber: ungated whisper model defaults. Checksum is empty by
		// default so callers can omit it without breaking validation. Users set
		// it after running `journal models pull` and noting the printed checksum.
		Transcriber: Transcriber{
			ModelID:  "Systran/faster-whisper-base.en",
			Revision: "main",
			Checksum: "",
			ModelDir: "~/.cache/journal/models",
		},
		// Quill/transcript integration (v2.0). On by default but a no-op until a
		// transcript exists; quill-sync only works where Quill runs (macOS/Windows).
		Transcripts: Transcripts{
			Enabled:     true,
			Path:        "transcripts",
			Format:      TranscriptFormatAuto,
			AutoIndex:   true,
			Tag:         "meeting",
			LogCaptures: false,
		},
		Quill: Quill{
			Enabled:         true,
			DBPath:          defaultQuillDBPath(),
			AcceptQMImports: true,
		},
		Log: LogConfig{
			Audio:       LogAudio{Device: "default", SampleRate: 16000, Channels: 1, MaxDuration: 900, SilenceDuration: 30, SilenceNoiseDB: -35},
			Transcriber: LogTranscriber{Backend: "whisper.cpp", Model: "base.en", ModelDir: "~/.cache/journal/models"},
			Shaping:     LogShaping{Enabled: true, KeepRawTranscript: true},
			Landing:     LogLanding{Dir: "logs", BacklinkDaily: false},
		},
		SchemaVer: SchemaVersion,
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

// TranscriptsRelPath is the repo-relative transcripts landing zone (slash form).
func (c *Config) TranscriptsRelPath() string {
	p := strings.TrimSpace(c.Transcripts.Path)
	if p == "" {
		p = "transcripts"
	}
	return filepath.ToSlash(p)
}

// TranscriptsAbsPath returns the absolute path to the transcripts landing zone.
func (c *Config) TranscriptsAbsPath() string {
	p := c.TranscriptsRelPath()
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.root, filepath.FromSlash(p))
}

// LogRelPath is the repo-relative voice-note landing zone (slash form).
func (c *Config) LogRelPath() string {
	p := strings.TrimSpace(c.Log.Landing.Dir)
	if p == "" {
		p = "logs"
	}
	return filepath.ToSlash(p)
}

// LogAbsPath returns the absolute path to the voice-note landing zone.
func (c *Config) LogAbsPath() string {
	p := c.LogRelPath()
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.root, filepath.FromSlash(p))
}

// expandHomeDir expands a leading ~ (or ~/...) to the user's home directory.
// Paths that don't start with ~ are returned unchanged; a failure to resolve
// the home directory also leaves the path unchanged.
func expandHomeDir(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// TranscriberModelDirAbs returns the absolute path to the transcriber model
// directory, expanding a leading ~. Returns the default when ModelDir is empty.
func (c *Config) TranscriberModelDirAbs() string {
	d := strings.TrimSpace(c.Transcriber.ModelDir)
	if d == "" {
		d = "~/.cache/journal/models"
	}
	return expandHomeDir(d)
}

// LogTranscriberModelDirAbs returns the absolute path to the log transcriber's
// model directory, expanding a leading ~. Falls back to the same default as
// TranscriberModelDirAbs when ModelDir is empty.
func (c *Config) LogTranscriberModelDirAbs() string {
	d := strings.TrimSpace(c.Log.Transcriber.ModelDir)
	if d == "" {
		d = "~/.cache/journal/models"
	}
	return expandHomeDir(d)
}

// LogAudioTmpDirAbs returns the absolute directory for scratch recording WAVs,
// expanding a leading ~ and creating it if missing. Falls back to
// <os temp dir>/journal-log when log.audio.tmp_dir is empty.
func (c *Config) LogAudioTmpDirAbs() (string, error) {
	d := strings.TrimSpace(c.Log.Audio.TmpDir)
	if d == "" {
		d = filepath.Join(os.TempDir(), "journal-log")
	} else {
		d = expandHomeDir(d)
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", fmt.Errorf("creating log audio tmp dir %q: %w", d, err)
	}
	return d, nil
}

// QuillDBPath returns the configured Quill database path with ~ expanded, or ""
// when none is configured (e.g. on Linux, where Quill is unavailable).
func (c *Config) QuillDBPath() string {
	p := strings.TrimSpace(c.Quill.DBPath)
	if p == "" {
		return ""
	}
	return expandHomeDir(p)
}

// Validate checks invariants on non-secret settings.
func (c *Config) Validate() error {
	switch c.EmbedProvider {
	case EmbedProviderOllama, EmbedProviderOpenAI:
	default:
		return fmt.Errorf("embed_provider %q unsupported (want ollama|openai)", c.EmbedProvider)
	}
	if c.EmbedProvider == EmbedProviderOllama && c.EmbedModel == "" {
		return errors.New("embed_model must not be empty when embed_provider is \"ollama\"")
	}
	if c.EmbedProvider == EmbedProviderOpenAI {
		if c.EmbedOpenAIBaseURL == "" {
			return errors.New("embed_openai_base_url must not be empty when embed_provider is \"openai\"")
		}
		if c.EmbedOpenAIModel == "" {
			return errors.New("embed_openai_model must not be empty when embed_provider is \"openai\" (and set embed_dim to its output dimension, e.g. 1536 for text-embedding-3-small)")
		}
	}
	// Reranker may be empty (reranking disabled) — no validation needed.
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
	switch c.SynthProvider {
	case SynthProviderAnthropic, SynthProviderOllama, SynthProviderOpenAI:
	default:
		return fmt.Errorf("synth_provider %q unsupported (want anthropic|ollama|openai)", c.SynthProvider)
	}
	if c.SynthModel == "" {
		return errors.New("synth_model must not be empty")
	}
	if c.SynthProvider == SynthProviderOllama && c.SynthOllamaModel == "" {
		return errors.New("synth_ollama_model must not be empty when synth_provider is \"ollama\"")
	}
	if c.SynthProvider == SynthProviderOpenAI {
		if c.SynthOpenAIBaseURL == "" {
			return errors.New("synth_openai_base_url must not be empty when synth_provider is \"openai\"")
		}
		if c.SynthOpenAIModel == "" {
			return errors.New("synth_openai_model must not be empty when synth_provider is \"openai\" (pick one from your provider, e.g. google/gemma-3-27b-it:free on OpenRouter)")
		}
	}
	if c.SynthNumCtx <= 0 {
		return fmt.Errorf("synth_num_ctx must be > 0, got %d", c.SynthNumCtx)
	}
	if c.SynthMaxTokens <= 0 {
		return fmt.Errorf("synth_max_tokens must be > 0, got %d", c.SynthMaxTokens)
	}
	if c.LocalOnly && c.SynthProvider != SynthProviderOllama {
		return fmt.Errorf("local_only is enabled but synth_provider is %q — cloud synthesis is refused under local_only, so this never works; set `synth_provider: ollama` (note: the provider switch is `synth_provider`, not `synth_model`)", c.SynthProvider)
	}
	if c.LocalOnly && c.EmbedProvider != EmbedProviderOllama {
		return fmt.Errorf("local_only is enabled but embed_provider is %q — remote embeddings send your note text off-machine; set `embed_provider: ollama`", c.EmbedProvider)
	}
	if c.LocalOnly && !isLoopbackURL(c.OllamaBaseURL) {
		return fmt.Errorf("local_only is enabled but ollama_base_url %q is not loopback — a network Ollama host is egress; point it at localhost or disable local_only", c.OllamaBaseURL)
	}
	switch c.LocalOnlyMCP {
	case LocalOnlyMCPBlock, LocalOnlyMCPAllow:
	default:
		return fmt.Errorf("local_only_mcp %q unsupported (want block|allow)", c.LocalOnlyMCP)
	}
	switch c.SyncConflict {
	case SyncConflictManual, SyncConflictPreferUpstream, SyncConflictPreferLocal:
	default:
		return fmt.Errorf("sync_conflict %q unsupported (want manual|prefer-upstream|prefer-local)", c.SyncConflict)
	}
	switch c.Transcripts.Format {
	case TranscriptFormatAuto, TranscriptFormatMarkdown, TranscriptFormatTxt:
	default:
		return fmt.Errorf("transcripts.format %q unsupported (want auto|markdown|txt)", c.Transcripts.Format)
	}
	if c.Transcripts.Enabled && strings.TrimSpace(c.Transcripts.Path) == "" {
		return errors.New("transcripts.path must not be empty when transcripts are enabled")
	}
	if strings.TrimSpace(c.Log.Landing.Dir) == "" {
		return errors.New("log.landing.dir must not be empty")
	}
	if c.Log.Audio.MaxDuration < 0 {
		return fmt.Errorf("log.audio.max_duration must be >= 0, got %d", c.Log.Audio.MaxDuration)
	}
	if c.Log.Audio.SilenceDuration <= 0 {
		return fmt.Errorf("log.audio.silence_duration must be > 0, got %d", c.Log.Audio.SilenceDuration)
	}
	switch c.Log.Audio.Backend {
	case "", "avfoundation", "pulse", "alsa":
	default:
		return fmt.Errorf("log.audio.backend %q unsupported (want \"\"|avfoundation|pulse|alsa)", c.Log.Audio.Backend)
	}
	return nil
}

// ActiveSynthModel returns the model name synthesis will actually use, per the
// configured provider.
func (c *Config) ActiveSynthModel() string {
	switch c.SynthProvider {
	case SynthProviderOllama:
		return c.SynthOllamaModel
	case SynthProviderOpenAI:
		return c.SynthOpenAIModel
	default:
		return c.SynthModel
	}
}

// isLoopbackURL reports whether the URL's host resolves textually to loopback
// (localhost, 127.0.0.0/8, or ::1). It deliberately does not do DNS: local_only
// must not depend on the network, and a hostname that *might* resolve to
// loopback is not a guarantee.
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

// OpenAIAPIKey returns the key for OpenAI-compatible providers from the
// environment. Never read from or written to config; callers must never log it.
func OpenAIAPIKey() (string, error) {
	key := os.Getenv(OpenAIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("%s is not set in the environment", OpenAIKeyEnv)
	}
	return key, nil
}

// HuggingFaceToken returns HF_TOKEN from the environment, or "" if unset.
// Never read from or written to config. Unlike AnthropicAPIKey/OpenAIAPIKey,
// a missing token is not itself an error — it's only required for gated model
// repos, and internal/models.Pull produces the "accept terms" message when a
// gated pull needs one.
func HuggingFaceToken() string {
	return os.Getenv(HFTokenEnv)
}
