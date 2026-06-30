package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	jlog "github.com/ericmann/journal/internal/log"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/synth"
)

func TestLogTextLocalOnlyLandsRawNote(t *testing.T) {
	cfg := testRepo(t, nil)
	// local_only requires ollama provider.
	cfg.LocalOnly = true
	cfg.SynthProvider = config.SynthProviderOllama

	fake := embed.NewFake(cfg.EmbedDim)
	// Pass a client that would fail if called — local_only must skip shaping.
	fc := &synth.Fake{Reply: "should not be called"}

	var out bytes.Buffer
	err := runLogText(context.Background(), cfg, fake, fc, "quick note about deploy logs", &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "logged:") {
		t.Errorf("expected 'logged:' in output, got: %q", out.String())
	}

	// Verify file exists in the logs directory.
	entries, readErr := os.ReadDir(cfg.LogAbsPath())
	if readErr != nil {
		t.Fatalf("logs dir not created: %v", readErr)
	}
	if len(entries) == 0 {
		t.Error("no files in logs dir after log --text")
	}

	// fc must not have been called (local_only blocks cloud shaping).
	if fc.CallCount > 0 {
		t.Errorf("shaping client called %d times under local_only, want 0", fc.CallCount)
	}
}

func TestLogTextWithFakeSynthLandsShapedNote(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = true
	cfg.Log.Shaping.KeepRawTranscript = true

	validReply := `{"title":"Deploy Review","summary":"Checked deploy logs.","body":"Reviewed the deploy logs and found no anomalies.","tags":["deploy"],"markers":["@todo double-check deploy"]}`
	fc := &synth.Fake{Reply: validReply}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	err := runLogText(context.Background(), cfg, fake, fc, "I reviewed the deploy logs no anomalies", &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "logged:") {
		t.Errorf("expected logged output, got: %q", out.String())
	}

	// Verify the landed file contains the shaped title and source marker.
	logsDir := cfg.LogAbsPath()
	entries, err := os.ReadDir(logsDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("no files in logs dir")
	}
	content, err := os.ReadFile(filepath.Join(logsDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Deploy Review") {
		t.Errorf("shaped title not in landed file: %q", string(content))
	}
	if !strings.Contains(string(content), "source: voice") {
		t.Errorf("missing 'source: voice' in landed file")
	}
}

func TestLogTextIndexFailureIsNonFatal(t *testing.T) {
	cfg := testRepo(t, nil)
	// Bad store path forces index failure.
	cfg.StorePath = "/nonexistent/bad.db"

	fake := embed.NewFake(cfg.EmbedDim)
	var out bytes.Buffer
	err := runLogText(context.Background(), cfg, fake, nil, "a note that fails to index", &out)
	if err != nil {
		t.Errorf("index failure should be non-fatal, got: %v", err)
	}
	if !strings.Contains(out.String(), "logged:") {
		t.Errorf("logged: should appear even on index failure, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "index skipped") {
		t.Errorf("index skip warning expected, got: %q", out.String())
	}
}

func TestLogTextShaperErrorFallsBackToRaw(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = true

	// Force the shaper to return an error → raw fallback.
	fc := &synth.Fake{ForcedErr: errors.New("fake network error")}

	var out bytes.Buffer
	err := runLogText(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), fc, "note text here", &out)
	if err != nil {
		t.Errorf("shaping error should be non-fatal, got: %v", err)
	}
	if !strings.Contains(out.String(), "logged:") {
		t.Errorf("expected logged output after shaping failure, got: %q", out.String())
	}
}

func TestLogTextSearchableByVoiceSource(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = false // skip shaping, pure raw land

	fake := embed.NewFake(cfg.EmbedDim)
	var out bytes.Buffer
	if err := runLogText(context.Background(), cfg, fake, nil, "searched by voice source", &out); err != nil {
		t.Fatal(err)
	}

	// Search with SourceVoice filter should find the chunk.
	results, err := runSearch(context.Background(), cfg, fake, "searched by voice source", 5,
		store.Filter{Sources: []string{store.SourceVoice}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("search --source voice returned no results")
	}
}

// --- Audio transcription tests ---

func TestLogAudioFakeTranscriberLandsNote(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = false

	// Write a minimal placeholder audio file (content doesn't matter — fake transcriber).
	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "fixed the caching bug today", DurationSec: 15}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "logged:") {
		t.Errorf("expected 'logged:' in output, got: %q", out.String())
	}

	// File should exist in logs dir.
	entries, err := os.ReadDir(cfg.LogAbsPath())
	if err != nil || len(entries) == 0 {
		t.Fatal("no files in logs dir after audio log")
	}

	// Landed file should have source: voice and transcriber name.
	content, err := os.ReadFile(filepath.Join(cfg.LogAbsPath(), entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "source: voice") {
		t.Error("landed file missing 'source: voice'")
	}
	if !strings.Contains(string(content), "fake/base.en") {
		t.Errorf("landed file missing transcriber name, got:\n%s", string(content))
	}
	if !strings.Contains(string(content), "duration_sec: 15") {
		t.Errorf("landed file missing duration_sec: 15, got:\n%s", string(content))
	}
}

func TestLogAudioWithShapingLandsShapedNote(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = true
	cfg.Log.Shaping.KeepRawTranscript = true

	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "deployed the new caching layer", DurationSec: 8}
	validReply := `{"title":"Caching Deploy","summary":"Deployed new caching.","body":"Deployed the new caching layer successfully.","tags":["deploy","cache"],"markers":[]}`
	fc := &synth.Fake{Reply: validReply}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	err := runLogAudio(context.Background(), cfg, fake, ft, fc, audioPath, &out)
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(cfg.LogAbsPath())
	if len(entries) == 0 {
		t.Fatal("no files in logs dir")
	}
	content, _ := os.ReadFile(filepath.Join(cfg.LogAbsPath(), entries[0].Name()))
	if !strings.Contains(string(content), "Caching Deploy") {
		t.Errorf("shaped title not in landed file: %q", string(content))
	}
}

func TestLogAudioTranscriptionErrorIsRetryable(t *testing.T) {
	cfg := testRepo(t, nil)
	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{ForcedErr: errors.New("model not found at \"/models/base.en.bin\": run `journal models pull`")}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, &out)
	if err == nil {
		t.Fatal("expected error for transcription failure")
	}
	// Audio file must still exist (not deleted).
	if _, statErr := os.Stat(audioPath); statErr != nil {
		t.Errorf("audio file was deleted on transcription error: %v", statErr)
	}
	// Error should be retryable (contains context about retrying).
	if !strings.Contains(err.Error(), "retryable") {
		t.Errorf("error should mention retryable, got: %v", err)
	}
	// No file should have landed.
	entries, _ := os.ReadDir(cfg.LogAbsPath())
	if len(entries) != 0 {
		t.Error("a note was landed despite transcription error")
	}
}

func TestLogAudioEmptyTranscriptSkipsPipeline(t *testing.T) {
	cfg := testRepo(t, nil)
	audioPath := filepath.Join(t.TempDir(), "silence.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Empty reply simulates a silent recording.
	ft := &jlog.FakeTranscriber{Reply: "   "}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, &out)
	if err != nil {
		t.Errorf("empty transcript should not error, got: %v", err)
	}
	if !strings.Contains(out.String(), "empty transcript") {
		t.Errorf("expected empty-transcript notice, got: %q", out.String())
	}
	// No file should have landed.
	entries, _ := os.ReadDir(cfg.LogAbsPath())
	if len(entries) != 0 {
		t.Error("a note was landed for an empty transcript")
	}
}

func TestLogAudioMissingFileReturnsError(t *testing.T) {
	cfg := testRepo(t, nil)
	ft := &jlog.FakeTranscriber{Reply: "text"}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	err := runLogAudio(context.Background(), cfg, fake, ft, nil, "/nonexistent/audio.wav", &out)
	if err == nil {
		t.Fatal("expected error for missing audio file")
	}
}

func TestLogAudioIndexedByVoiceSource(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = false

	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "audio voice source search test"}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	if err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, &out); err != nil {
		t.Fatal(err)
	}

	results, err := runSearch(context.Background(), cfg, fake, "audio voice source search test", 5,
		store.Filter{Sources: []string{store.SourceVoice}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("audio note not found via --source voice search")
	}
}
