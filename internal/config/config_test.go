package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncDefaultsAndValidation(t *testing.T) {
	c := Default()
	if c.SyncEnabled {
		t.Error("sync must be disabled by default")
	}
	if c.SyncConflict != SyncConflictManual {
		t.Errorf("default sync_conflict = %q, want %q", c.SyncConflict, SyncConflictManual)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
	for _, mode := range []string{SyncConflictManual, SyncConflictPreferUpstream, SyncConflictPreferLocal} {
		c.SyncConflict = mode
		if err := c.Validate(); err != nil {
			t.Errorf("sync_conflict %q should be valid: %v", mode, err)
		}
	}
	c.SyncConflict = "bogus"
	if err := c.Validate(); err == nil {
		t.Error("expected an error for an unknown sync_conflict mode")
	}
}

func TestEditorDefaultsEmptyAndMarshals(t *testing.T) {
	c := Default()
	if c.Editor != "" {
		t.Errorf("default editor = %q, want \"\" (fall back to env/nano)", c.Editor)
	}
	data, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "editor:") {
		t.Errorf("marshaled config missing editor key:\n%s", data)
	}
}

func TestDefaultConfigIsValid(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatalf("default config should validate, got: %v", err)
	}
	if c.EmbedModel != "qwen3-embedding:4b" {
		t.Errorf("embed model = %q, want qwen3-embedding:4b", c.EmbedModel)
	}
	if c.Reranker != "" {
		t.Errorf("reranker = %q, want \"\" (disabled by default)", c.Reranker)
	}
	if c.EmbedDim != 2560 {
		t.Errorf("embed_dim = %d, want 2560 (qwen3-embedding:4b)", c.EmbedDim)
	}
	if c.OllamaBaseURL != "http://localhost:11434" {
		t.Errorf("ollama url = %q", c.OllamaBaseURL)
	}
	if c.ChunkStrategy != "heading" {
		t.Errorf("chunk strategy = %q, want heading", c.ChunkStrategy)
	}
	if c.EmbedDim <= 0 {
		t.Errorf("embed dim = %d, want > 0", c.EmbedDim)
	}
	for _, want := range []string{"reflections/**", ".journal/**", "docs/**", "README.md"} {
		found := false
		for _, e := range c.Excludes {
			if e == want {
				found = true
			}
		}
		if !found {
			t.Errorf("default excludes missing %q (got %v)", want, c.Excludes)
		}
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cases := map[string]func(*Config){
		"zero dim":         func(c *Config) { c.EmbedDim = 0 },
		"negative dim":     func(c *Config) { c.EmbedDim = -1 },
		"empty embedmodel": func(c *Config) { c.EmbedModel = "" },
		"empty ollamaurl":  func(c *Config) { c.OllamaBaseURL = "" },
		"bad strategy":     func(c *Config) { c.ChunkStrategy = "paragraph" },
		"empty storepath":  func(c *Config) { c.StorePath = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := Default()
			mutate(&c)
			if err := c.Validate(); err == nil {
				t.Errorf("expected validation error for %s", name)
			}
		})
	}
}

func TestFindRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "projects", "foo", "notes")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindRepoRoot(deep)
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	// macOS temp dirs may be symlinked (/var -> /private/var); compare resolved.
	gotR, _ := filepath.EvalSymlinks(got)
	rootR, _ := filepath.EvalSymlinks(root)
	if gotR != rootR {
		t.Errorf("FindRepoRoot = %q, want %q", gotR, rootR)
	}
}

func TestFindRepoRootNotFound(t *testing.T) {
	dir := t.TempDir() // no .journal anywhere up to / (temp dir has none)
	if _, err := FindRepoRoot(dir); err == nil {
		t.Error("expected error when no .journal found")
	}
}

func TestLoadAppliesDefaultsForMissingKeys(t *testing.T) {
	root := t.TempDir()
	jdir := filepath.Join(root, ".journal")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only override the embed model + dim; everything else should default.
	yaml := "embed_model: qwen3-embedding:8b\nembed_dim: 768\n"
	if err := os.WriteFile(filepath.Join(jdir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.EmbedModel != "qwen3-embedding:8b" {
		t.Errorf("embed model = %q, want override", c.EmbedModel)
	}
	if c.EmbedDim != 768 {
		t.Errorf("embed dim = %d, want 768", c.EmbedDim)
	}
	if c.SynthModel != "claude-sonnet-4-6" {
		t.Errorf("synth_model = %q, want defaulted", c.SynthModel)
	}
	if c.ChunkStrategy != "heading" {
		t.Errorf("chunk strategy = %q, want defaulted", c.ChunkStrategy)
	}
	if c.Root() != root {
		t.Errorf("Root() = %q, want %q", c.Root(), root)
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load with no config.yaml should succeed with defaults: %v", err)
	}
	if c.EmbedModel != "qwen3-embedding:4b" {
		t.Errorf("expected default embed model, got %q", c.EmbedModel)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	root := t.TempDir()
	jdir := filepath.Join(root, ".journal")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "config.yaml"), []byte("embed_dim: \"not a number\"\n: bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Error("expected error on invalid yaml")
	}
}

func TestVoiceProfileAbsPath(t *testing.T) {
	c := Default()
	c.root = "/repo"
	want := filepath.Join("/repo", "docs", "VOICE_PROFILE.md")
	if got := c.VoiceProfileAbsPath(); got != want {
		t.Errorf("VoiceProfileAbsPath = %q, want %q", got, want)
	}
	// Absolute config path is used as-is.
	c.VoiceProfilePath = "/abs/voice.md"
	if got := c.VoiceProfileAbsPath(); got != "/abs/voice.md" {
		t.Errorf("absolute path = %q", got)
	}
	// Empty means "no profile".
	c.VoiceProfilePath = ""
	if got := c.VoiceProfileAbsPath(); got != "" {
		t.Errorf("empty profile path should yield \"\", got %q", got)
	}
}

func TestStoreAbsPath(t *testing.T) {
	c := Default()
	c.root = "/repo"
	want := filepath.Join("/repo", ".journal", "index", "journal.db")
	if got := c.StoreAbsPath(); got != want {
		t.Errorf("StoreAbsPath = %q, want %q", got, want)
	}
}

func TestAnthropicAPIKeyFromEnvOnly(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-123")
	got, err := AnthropicAPIKey()
	if err != nil {
		t.Fatalf("AnthropicAPIKey: %v", err)
	}
	if got != "sk-test-123" {
		t.Errorf("key = %q, want sk-test-123", got)
	}

	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := AnthropicAPIKey(); err == nil {
		t.Error("expected error when ANTHROPIC_API_KEY unset")
	}
}

// The API key must never be serialized into config.yaml.
func TestConfigMarshalOmitsSecret(t *testing.T) {
	c := Default()
	out, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"ANTHROPIC", "api_key", "apikey", "sk-"} {
		if containsFold(string(out), bad) {
			t.Errorf("marshaled config must not contain %q; got:\n%s", bad, out)
		}
	}
}

func containsFold(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && indexFold(s, substr) >= 0
}

func indexFold(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			a, b := s[i+j], substr[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
