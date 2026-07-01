package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ericmann/journal/internal/embed"
)

// fakeChecker satisfies ollamaChecker: canned tags + a fake embedder whose
// vector length is `dim` (so embed_dim probing can be exercised).
type fakeChecker struct {
	tags []string
	err  error
	dim  int
}

func (f fakeChecker) Tags(context.Context) ([]string, error) { return f.tags, f.err }

func (f fakeChecker) Embed(ctx context.Context, texts []string, instruction string) ([][]float32, error) {
	d := f.dim
	if d == 0 {
		d = 2560 // default-config dimension
	}
	return embed.NewFake(d).Embed(ctx, texts, instruction)
}

func findCheck(rep doctorReport, name string) (check, bool) {
	for _, c := range rep.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return check{}, false
}

func TestDoctorAllHealthy(t *testing.T) {
	cfg := testRepo(t, nil) // default config: reranker disabled, embed_dim 2560
	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)
	if !rep.OK {
		t.Errorf("expected healthy, got: %+v", rep.Checks)
	}
	// Reranker disabled by default -> OK (informational).
	if c, _ := findCheck(rep, "reranker"); !c.OK {
		t.Errorf("disabled reranker should be OK: %+v", c)
	}
	if c, _ := findCheck(rep, "embed_dim"); !c.OK {
		t.Errorf("embed_dim should match: %+v", c)
	}
}

func TestDoctorOllamaDownFails(t *testing.T) {
	cfg := testRepo(t, nil)
	checker := fakeChecker{err: errors.New("connection refused")}
	rep := runDoctor(context.Background(), cfg, checker)
	if rep.OK {
		t.Error("expected failure when Ollama is down")
	}
	c, ok := findCheck(rep, "ollama")
	if !ok || c.OK {
		t.Errorf("ollama check should fail: %+v", c)
	}
	if _, ok := findCheck(rep, "embed_model"); ok {
		t.Error("model checks should be skipped when Ollama is down")
	}
}

func TestDoctorMissingEmbedModelFails(t *testing.T) {
	cfg := testRepo(t, nil)
	checker := fakeChecker{tags: []string{"some-other-model"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)
	if rep.OK {
		t.Error("expected failure when the embed model is missing")
	}
	if c, _ := findCheck(rep, "embed_model"); c.OK {
		t.Errorf("embed_model check should fail: %+v", c)
	}
}

func TestDoctorEmbedDimMismatchFails(t *testing.T) {
	cfg := testRepo(t, nil) // embed_dim 2560
	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: 1024}
	rep := runDoctor(context.Background(), cfg, checker)
	if rep.OK {
		t.Error("expected failure when probed dimension != embed_dim")
	}
	c, _ := findCheck(rep, "embed_dim")
	if c.OK || !contains(c.Detail, "1024") {
		t.Errorf("embed_dim check should report actual dim: %+v", c)
	}
}

func TestDoctorRerankerMissingIsNotFatal(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Reranker = "qwen3:4b" // set but not pulled
	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)
	c, _ := findCheck(rep, "reranker")
	if !c.OK {
		t.Errorf("a missing-but-optional reranker must not fail the verdict: %+v", c)
	}
}

func TestDoctorReportsChunkCount(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00\nnote\n",
	})
	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)
	c, _ := findCheck(rep, "index")
	if !c.OK {
		t.Errorf("index check should pass: %+v", c)
	}
	if !contains(c.Detail, "1 chunks") {
		t.Errorf("index detail = %q, want chunk count", c.Detail)
	}
}

// stubWhisperBinAvailable overrides the whisper.cpp binary lookup so doctor
// tests never depend on (or require the absence of) a real install.
func stubWhisperBinAvailable(t *testing.T, path string, err error) {
	t.Helper()
	orig := checkWhisperBinAvailable
	checkWhisperBinAvailable = func() (string, error) { return path, err }
	t.Cleanup(func() { checkWhisperBinAvailable = orig })
}

// stubNotifierAvailable overrides the notifier-backend lookup so doctor tests
// never depend on (or require the absence of) osascript/terminal-notifier.
func stubNotifierAvailable(t *testing.T, name string) {
	t.Helper()
	orig := checkNotifierAvailable
	checkNotifierAvailable = func() string { return name }
	t.Cleanup(func() { checkNotifierAvailable = orig })
}

func TestDoctorAudioAllPresent(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Transcriber.ModelDir = t.TempDir()
	cfg.Log.Transcriber.Model = "base.en"
	modelPath := filepath.Join(cfg.Log.Transcriber.ModelDir, "base.en.bin")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0o644); err != nil {
		t.Fatal(err)
	}

	stubFfmpegAvailable(t, nil)
	stubWhisperBinAvailable(t, "/usr/local/bin/whisper-cli", nil)
	stubNotifierAvailable(t, "osascript")

	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)

	if !rep.OK {
		t.Errorf("expected healthy report, got: %+v", rep.Checks)
	}
	for _, name := range []string{"ffmpeg", "whisper_bin", "transcriber_model", "notifier"} {
		if c, ok := findCheck(rep, name); !ok || !c.OK {
			t.Errorf("%s check missing or failed: %+v", name, c)
		}
	}
	if c, _ := findCheck(rep, "transcriber_model"); !contains(c.Detail, "present") {
		t.Errorf("transcriber_model detail = %q, want mention of 'present'", c.Detail)
	}
	if c, _ := findCheck(rep, "whisper_bin"); c.Detail != "/usr/local/bin/whisper-cli" {
		t.Errorf("whisper_bin detail = %q, want resolved path", c.Detail)
	}
	if c, _ := findCheck(rep, "notifier"); !contains(c.Detail, "osascript") {
		t.Errorf("notifier detail = %q, want mention of osascript", c.Detail)
	}
}

func TestDoctorAudioAllMissingIsNotFatal(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Transcriber.ModelDir = t.TempDir() // left empty — model file absent
	cfg.Log.Transcriber.Model = "base.en"

	stubFfmpegAvailable(t, errors.New("ffmpeg not found"))
	stubWhisperBinAvailable(t, "", errors.New("whisper.cpp binary not found in PATH"))
	stubNotifierAvailable(t, "")

	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)

	if !rep.OK {
		t.Errorf("a missing audio toolchain must not fail the overall verdict: %+v", rep.Checks)
	}
	if c, _ := findCheck(rep, "ffmpeg"); !c.OK || !contains(c.Detail, "not found") {
		t.Errorf("ffmpeg check = %+v", c)
	}
	if c, _ := findCheck(rep, "whisper_bin"); !c.OK || !contains(c.Detail, "not found") {
		t.Errorf("whisper_bin check = %+v", c)
	}
	if c, _ := findCheck(rep, "transcriber_model"); !c.OK || !contains(c.Detail, "journal models pull") {
		t.Errorf("transcriber_model check = %+v", c)
	}
	if c, _ := findCheck(rep, "notifier"); !c.OK || !contains(c.Detail, "not found") {
		t.Errorf("notifier check = %+v", c)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
