package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogAudioDefaults(t *testing.T) {
	c := Default()
	if c.Log.Audio.MaxDuration != 900 {
		t.Errorf("log.audio.max_duration = %d, want 900", c.Log.Audio.MaxDuration)
	}
	if c.Log.Audio.SilenceAutostop {
		t.Error("log.audio.silence_autostop should default to false")
	}
	if c.Log.Audio.KeepWAV {
		t.Error("log.audio.keep_wav should default to false")
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestLogAudioTmpDirAbsFallsBackToOSTempDir(t *testing.T) {
	c := Default()
	got, err := c.LogAudioTmpDirAbs()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(os.TempDir(), "journal-log")
	if got != want {
		t.Errorf("LogAudioTmpDirAbs() = %q, want %q", got, want)
	}
	if info, statErr := os.Stat(got); statErr != nil || !info.IsDir() {
		t.Errorf("LogAudioTmpDirAbs() should create the directory, stat err = %v", statErr)
	}
	_ = os.RemoveAll(got)
}

func TestLogAudioTmpDirAbsExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	c := Default()
	c.Log.Audio.TmpDir = "~/.cache/journal-log-test-tmp"
	got, err := c.LogAudioTmpDirAbs()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".cache", "journal-log-test-tmp")
	if got != want {
		t.Errorf("LogAudioTmpDirAbs() = %q, want %q", got, want)
	}
	_ = os.RemoveAll(got)
}

func TestLogAudioTmpDirAbsHonorsExplicitDir(t *testing.T) {
	c := Default()
	c.Log.Audio.TmpDir = filepath.Join(t.TempDir(), "scratch")
	got, err := c.LogAudioTmpDirAbs()
	if err != nil {
		t.Fatal(err)
	}
	if got != c.Log.Audio.TmpDir {
		t.Errorf("LogAudioTmpDirAbs() = %q, want %q", got, c.Log.Audio.TmpDir)
	}
}

func TestLoadOldConfigWithoutLogAudioKeysDefaultsCleanly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulates a pre-Phase-3 config: log.audio has only the original three keys.
	yaml := "log:\n  audio:\n    device: default\n    sample_rate: 16000\n    channels: 1\n"
	if err := os.WriteFile(filepath.Join(root, ".journal", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Log.Audio.MaxDuration != 900 {
		t.Errorf("max_duration absent from YAML should keep the Default() value, got %d", c.Log.Audio.MaxDuration)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("old config missing new log.audio keys should still validate: %v", err)
	}
}

func TestValidateRejectsNegativeMaxDuration(t *testing.T) {
	c := Default()
	c.Log.Audio.MaxDuration = -1
	if err := c.Validate(); err == nil {
		t.Error("expected error for negative log.audio.max_duration")
	}
}

func TestLogAudioBackendDefaultsEmpty(t *testing.T) {
	c := Default()
	if c.Log.Audio.Backend != "" {
		t.Errorf("log.audio.backend = %q, want \"\" (auto-detect)", c.Log.Audio.Backend)
	}
}

func TestValidateAcceptsKnownBackends(t *testing.T) {
	for _, backend := range []string{"", "avfoundation", "pulse", "alsa"} {
		t.Run(backend, func(t *testing.T) {
			c := Default()
			c.Log.Audio.Backend = backend
			if err := c.Validate(); err != nil {
				t.Errorf("Validate() with backend %q: %v", backend, err)
			}
		})
	}
}

func TestValidateRejectsUnknownBackend(t *testing.T) {
	c := Default()
	c.Log.Audio.Backend = "dshow"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for unknown log.audio.backend")
	}
	if !strings.Contains(err.Error(), "dshow") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoadOldConfigWithoutBackendKeyDefaultsCleanly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulates a pre-Linux-support config: log.audio has no backend key.
	yaml := "log:\n  audio:\n    device: default\n    sample_rate: 16000\n    channels: 1\n"
	if err := os.WriteFile(filepath.Join(root, ".journal", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Log.Audio.Backend != "" {
		t.Errorf("backend absent from YAML should default to \"\", got %q", c.Log.Audio.Backend)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("old config missing log.audio.backend should still validate: %v", err)
	}
}
