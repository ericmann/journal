package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/audio"
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
	_, err := runLogText(context.Background(), cfg, fake, fc, "quick note about deploy logs", &out)
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
	_, err := runLogText(context.Background(), cfg, fake, fc, "I reviewed the deploy logs no anomalies", &out)
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
	_, err := runLogText(context.Background(), cfg, fake, nil, "a note that fails to index", &out)
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
	_, err := runLogText(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), fc, "note text here", &out)
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
	if _, err := runLogText(context.Background(), cfg, fake, nil, "searched by voice source", &out); err != nil {
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
	_, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, false, false, &out)
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

func TestLogAudioNotifiesOnCompletion(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = true

	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "fixed the caching bug today", DurationSec: 15}
	validReply := `{"title":"Caching Fix","summary":"Fixed caching.","body":"Fixed the caching bug.","tags":[],"markers":[]}`
	fc := &synth.Fake{Reply: validReply}
	fake := embed.NewFake(cfg.EmbedDim)
	notifier := stubNewNotifier(t)

	var out bytes.Buffer
	if _, err := runLogAudio(context.Background(), cfg, fake, ft, fc, audioPath, false, false, &out); err != nil {
		t.Fatal(err)
	}

	if len(notifier.Calls) != 1 {
		t.Fatalf("expected 1 finish notification, got %d: %+v", len(notifier.Calls), notifier.Calls)
	}
	if !strings.Contains(notifier.Calls[0].Title, "Caching Fix") {
		t.Errorf("notification title = %q, want it to contain the note title", notifier.Calls[0].Title)
	}
	if !strings.HasPrefix(notifier.Calls[0].Title, "✓ logged:") {
		t.Errorf("notification title = %q, want prefix %q", notifier.Calls[0].Title, "✓ logged:")
	}
	if !strings.Contains(notifier.Calls[0].Message, "logs/") {
		t.Errorf("notification message = %q, want it to contain the note path", notifier.Calls[0].Message)
	}
}

func TestLogAudioUnshapedNotifiesWithDefaultTitle(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = false

	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "fixed the caching bug today", DurationSec: 15}
	fake := embed.NewFake(cfg.EmbedDim)
	notifier := stubNewNotifier(t)

	var out bytes.Buffer
	if _, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, false, false, &out); err != nil {
		t.Fatal(err)
	}

	if len(notifier.Calls) != 1 {
		t.Fatalf("expected 1 finish notification, got %d", len(notifier.Calls))
	}
	if notifier.Calls[0].Title != "✓ logged: Voice Note" {
		t.Errorf("notification title = %q, want %q", notifier.Calls[0].Title, "✓ logged: Voice Note")
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
	_, err := runLogAudio(context.Background(), cfg, fake, ft, fc, audioPath, false, false, &out)
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
	notifier := stubNewNotifier(t)

	var out bytes.Buffer
	_, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, false, false, &out)
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
	// The hotkey/mic-toggle flow is silent unless a desktop notification fires —
	// the user must be told the note failed and the audio was kept for retry.
	if len(notifier.Calls) != 1 {
		t.Fatalf("expected 1 failure notification, got %d: %+v", len(notifier.Calls), notifier.Calls)
	}
	if !strings.HasPrefix(notifier.Calls[0].Title, "✕") {
		t.Errorf("failure notification title = %q, want a ✕ failure marker", notifier.Calls[0].Title)
	}
	if !strings.Contains(notifier.Calls[0].Message, audioPath) {
		t.Errorf("failure notification message = %q, want the retryable audio path %q", notifier.Calls[0].Message, audioPath)
	}
}

func TestLogAudioLandFailureNotifies(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = false

	// Point the landing dir at a path that can never be created: a regular
	// file sits where a required directory component must go, so
	// os.MkdirAll (and thus jlog.Land) fails deterministically regardless of
	// the test process's privileges (unlike a permission-bit failure, which
	// root ignores).
	blocker := filepath.Join(cfg.Root(), "logs-blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Log.Landing.Dir = filepath.ToSlash(filepath.Join("logs-blocker", "sub"))

	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "fixed the caching bug today", DurationSec: 15}
	fake := embed.NewFake(cfg.EmbedDim)
	notifier := stubNewNotifier(t)

	var out bytes.Buffer
	_, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, false, false, &out)
	if err == nil {
		t.Fatal("expected error for land failure")
	}

	// The hotkey/mic-toggle flow is silent unless a desktop notification
	// fires — the user must be told the note failed to land.
	if len(notifier.Calls) != 1 {
		t.Fatalf("expected 1 failure notification, got %d: %+v", len(notifier.Calls), notifier.Calls)
	}
	if notifier.Calls[0].Title != "✕ journal log failed" {
		t.Errorf("notification title = %q, want %q", notifier.Calls[0].Title, "✕ journal log failed")
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
	notifier := stubNewNotifier(t)

	var out bytes.Buffer
	_, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, false, false, &out)
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
	// A silent recording still needs closure in the hotkey flow — no success
	// notification will come, so tell the user nothing was captured.
	if len(notifier.Calls) != 1 {
		t.Fatalf("expected 1 empty-recording notification, got %d: %+v", len(notifier.Calls), notifier.Calls)
	}
	if !strings.Contains(notifier.Calls[0].Message, "nothing to log") {
		t.Errorf("empty-recording notification message = %q, want it to say nothing was logged", notifier.Calls[0].Message)
	}
}

func TestLogAudioMissingFileReturnsError(t *testing.T) {
	cfg := testRepo(t, nil)
	ft := &jlog.FakeTranscriber{Reply: "text"}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	_, err := runLogAudio(context.Background(), cfg, fake, ft, nil, "/nonexistent/audio.wav", false, false, &out)
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
	if _, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, false, false, &out); err != nil {
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

// --- Recording toggle / lockfile tests ---

// isolateLock points the audio lockfile at a private temp dir so recording
// tests never collide with each other or a real journal-log session.
func isolateLock(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
}

// stubFfmpegAvailable overrides the ffmpeg-presence preflight check so start
// tests never depend on (or require the absence of) a real ffmpeg install.
func stubFfmpegAvailable(t *testing.T, err error) {
	t.Helper()
	orig := checkFfmpegAvailable
	checkFfmpegAvailable = func() error { return err }
	t.Cleanup(func() { checkFfmpegAvailable = orig })
}

// stubTranscriberAvailable overrides the transcriber-toolchain preflight
// check so start tests never depend on (or require the absence of) a real
// whisper.cpp install/model.
func stubTranscriberAvailable(t *testing.T, err error) {
	t.Helper()
	orig := checkTranscriberAvailable
	checkTranscriberAvailable = func(*config.Config) error { return err }
	t.Cleanup(func() { checkTranscriberAvailable = orig })
}

// stubSpawnDaemon overrides spawnDaemon to record its call instead of
// forking a real background process. It writes the lockfile itself,
// simulating the daemon's first action, unless writeLock is false.
func stubSpawnDaemon(t *testing.T, writeLock bool) *string {
	t.Helper()
	var gotWAVPath string
	orig := spawnDaemon
	spawnDaemon = func(wavPath string) error {
		gotWAVPath = wavPath
		if writeLock {
			return audio.WriteLock(audio.LockState{PID: os.Getpid(), WAVPath: wavPath, StartedAt: time.Now()})
		}
		return nil
	}
	t.Cleanup(func() { spawnDaemon = orig })
	return &gotWAVPath
}

// stubSpawnPipeline overrides spawnPipeline to record its call instead of
// forking a real detached process.
func stubSpawnPipeline(t *testing.T) *struct {
	wavPath string
	keepWAV bool
	calls   int
} {
	t.Helper()
	got := &struct {
		wavPath string
		keepWAV bool
		calls   int
	}{}
	orig := spawnPipeline
	spawnPipeline = func(wavPath string, keepWAV bool) error {
		got.wavPath, got.keepWAV = wavPath, keepWAV
		got.calls++
		return nil
	}
	t.Cleanup(func() { spawnPipeline = orig })
	return got
}

// stubNewNotifier overrides newNotifier to return a fake Notifier instead of
// the live osascript/terminal-notifier one, so tests never pop a real OS
// notification and can assert on what was sent.
func stubNewNotifier(t *testing.T) *audio.FakeNotifier {
	t.Helper()
	fake := &audio.FakeNotifier{}
	orig := newNotifier
	newNotifier = func(*config.Config) audio.Notifier { return fake }
	t.Cleanup(func() { newNotifier = orig })
	return fake
}

// stubSignalProcess overrides signalProcess to record calls instead of
// sending a real OS signal.
func stubSignalProcess(t *testing.T) *struct {
	pid int
	sig syscall.Signal
} {
	t.Helper()
	got := &struct {
		pid int
		sig syscall.Signal
	}{}
	orig := signalProcess
	signalProcess = func(pid int, sig syscall.Signal) error {
		got.pid, got.sig = pid, sig
		return nil
	}
	t.Cleanup(func() { signalProcess = orig })
	return got
}

// deadPID spawns and waits on a trivial process, returning a pid guaranteed
// to no longer be running.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("could not run a throwaway process: %v", err)
	}
	return cmd.Process.Pid
}

func TestLogToggleStartsWhenIdle(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	stubFfmpegAvailable(t, nil)
	stubTranscriberAvailable(t, nil)
	gotWAV := stubSpawnDaemon(t, true)

	var out bytes.Buffer
	if err := runLogToggle(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "● recording") {
		t.Errorf("expected recording notice, got: %q", out.String())
	}
	if *gotWAV == "" || !strings.HasSuffix(*gotWAV, ".wav") {
		t.Errorf("spawnDaemon called with unexpected wav path: %q", *gotWAV)
	}
}

func TestLogStartRecordingNotifiesOnStart(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	stubFfmpegAvailable(t, nil)
	stubTranscriberAvailable(t, nil)
	stubSpawnDaemon(t, true)
	notifier := stubNewNotifier(t)

	var out bytes.Buffer
	if err := runLogStartRecording(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if len(notifier.Calls) != 1 {
		t.Fatalf("expected 1 start notification, got %d: %+v", len(notifier.Calls), notifier.Calls)
	}
	if notifier.Calls[0].Message != "● recording" {
		t.Errorf("notification message = %q, want %q", notifier.Calls[0].Message, "● recording")
	}
}

func TestLogToggleStopsWhenActive(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	if err := audio.WriteLock(audio.LockState{PID: os.Getpid(), WAVPath: "/tmp/x.wav", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got := stubSignalProcess(t)

	var out bytes.Buffer
	if err := runLogToggle(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if got.pid != os.Getpid() || got.sig != syscall.SIGINT {
		t.Errorf("signalProcess called with (%d, %v), want (%d, SIGINT)", got.pid, got.sig, os.Getpid())
	}
	if !strings.Contains(out.String(), "stopping recording") {
		t.Errorf("expected stopping notice, got: %q", out.String())
	}
}

func TestLogToggleRecoversStaleLock(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	pid := deadPID(t)
	if err := audio.WriteLock(audio.LockState{PID: pid, WAVPath: "/tmp/x.wav", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	stubFfmpegAvailable(t, nil)
	stubTranscriberAvailable(t, nil)
	gotWAV := stubSpawnDaemon(t, true)

	var out bytes.Buffer
	if err := runLogToggle(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stale lock") {
		t.Errorf("expected stale-lock notice, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "● recording") {
		t.Errorf("expected fresh recording after stale-lock cleanup, got: %q", out.String())
	}
	if *gotWAV == "" {
		t.Error("spawnDaemon was not called after stale-lock cleanup")
	}
}

func TestLogStartRecordingAlreadyRecordingIsNoOp(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	if err := audio.WriteLock(audio.LockState{PID: os.Getpid(), WAVPath: "/tmp/x.wav", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	spawnCalled := false
	orig := spawnDaemon
	spawnDaemon = func(string) error { spawnCalled = true; return nil }
	t.Cleanup(func() { spawnDaemon = orig })
	notifier := stubNewNotifier(t)

	var out bytes.Buffer
	if err := runLogStartRecording(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already recording") {
		t.Errorf("expected already-recording notice, got: %q", out.String())
	}
	if spawnCalled {
		t.Error("spawnDaemon should not be called when already recording")
	}
	if len(notifier.Calls) != 0 {
		t.Errorf("expected no start notification when already recording, got %d", len(notifier.Calls))
	}
}

func TestNewRecorderThreadsSilenceConfig(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Audio.SilenceAutostop = true
	cfg.Log.Audio.SilenceDuration = 12
	cfg.Log.Audio.SilenceNoiseDB = -20

	rec, ok := newRecorder(cfg).(audio.FfmpegRecorder)
	if !ok {
		t.Fatalf("newRecorder() = %T, want audio.FfmpegRecorder", newRecorder(cfg))
	}
	if !rec.SilenceAutostop {
		t.Error("SilenceAutostop = false, want true")
	}
	if rec.SilenceDuration != 12*time.Second {
		t.Errorf("SilenceDuration = %v, want 12s", rec.SilenceDuration)
	}
	if rec.SilenceNoiseDB != -20 {
		t.Errorf("SilenceNoiseDB = %d, want -20", rec.SilenceNoiseDB)
	}
}

func TestLogStartRecordingFfmpegMissingFailsFast(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	wantErr := errors.New("ffmpeg not found in PATH")
	stubFfmpegAvailable(t, wantErr)
	spawnCalled := false
	orig := spawnDaemon
	spawnDaemon = func(string) error { spawnCalled = true; return nil }
	t.Cleanup(func() { spawnDaemon = orig })

	var out bytes.Buffer
	err := runLogStartRecording(cfg, &out)
	if err == nil {
		t.Fatal("expected an error when ffmpeg is unavailable")
	}
	if spawnCalled {
		t.Error("spawnDaemon should not be called when the ffmpeg preflight check fails")
	}
	if _, lockErr := audio.ReadLock(); !errors.Is(lockErr, os.ErrNotExist) {
		t.Error("no lockfile should be written when the ffmpeg preflight check fails")
	}
}

func TestLogStartRecordingTranscriberPreflight(t *testing.T) {
	tests := []struct {
		name           string
		transcriberErr error
	}{
		{name: "missing binary", transcriberErr: errors.New("whisper.cpp binary not found in PATH")},
		{name: "missing model", transcriberErr: errors.New(`model file not found at "x.bin": run "journal models pull"`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testRepo(t, nil)
			isolateLock(t)
			stubFfmpegAvailable(t, nil)
			stubTranscriberAvailable(t, tt.transcriberErr)
			notifier := stubNewNotifier(t)
			spawnCalled := false
			orig := spawnDaemon
			spawnDaemon = func(string) error { spawnCalled = true; return nil }
			t.Cleanup(func() { spawnDaemon = orig })

			var out bytes.Buffer
			err := runLogStartRecording(cfg, &out)
			if err == nil {
				t.Fatal("expected an error when the transcriber toolchain is unavailable")
			}
			if !errors.Is(err, tt.transcriberErr) {
				t.Errorf("err = %v, want %v", err, tt.transcriberErr)
			}
			if spawnCalled {
				t.Error("spawnDaemon should not be called when the transcriber preflight check fails")
			}
			if _, lockErr := audio.ReadLock(); !errors.Is(lockErr, os.ErrNotExist) {
				t.Error("no lockfile should be written when the transcriber preflight check fails")
			}
			if len(notifier.Calls) != 1 {
				t.Fatalf("expected 1 failure notification, got %d: %+v", len(notifier.Calls), notifier.Calls)
			}
			if notifier.Calls[0].Title != "✕ journal log failed" {
				t.Errorf("notification title = %q, want %q", notifier.Calls[0].Title, "✕ journal log failed")
			}
			if notifier.Calls[0].Message != tt.transcriberErr.Error() {
				t.Errorf("notification message = %q, want %q", notifier.Calls[0].Message, tt.transcriberErr.Error())
			}
		})
	}
}

func TestLogStartRecordingTranscriberAvailablePassesThrough(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	stubFfmpegAvailable(t, nil)
	stubTranscriberAvailable(t, nil)
	gotWAV := stubSpawnDaemon(t, true)

	var out bytes.Buffer
	if err := runLogStartRecording(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if *gotWAV == "" {
		t.Error("spawnDaemon should be called when the transcriber preflight check passes")
	}
}

func TestLogStopRecordingNoActiveRecording(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)

	var out bytes.Buffer
	if err := runLogStopRecording(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no active recording") {
		t.Errorf("expected no-active-recording notice, got: %q", out.String())
	}
}

func TestLogStopRecordingStaleLockIsCleaned(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	pid := deadPID(t)
	if err := audio.WriteLock(audio.LockState{PID: pid, WAVPath: "/tmp/x.wav", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runLogStopRecording(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stale lock") || !strings.Contains(out.String(), "no active recording") {
		t.Errorf("expected stale-lock cleanup + no-active-recording notice, got: %q", out.String())
	}
	if _, err := audio.ReadLock(); !errors.Is(err, os.ErrNotExist) {
		t.Error("stale lock should have been removed")
	}
}

func TestLogCancelRecordingSendsSIGUSR1(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	if err := audio.WriteLock(audio.LockState{PID: os.Getpid(), WAVPath: "/tmp/x.wav", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got := stubSignalProcess(t)

	var out bytes.Buffer
	if err := runLogCancelRecording(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if got.pid != os.Getpid() || got.sig != syscall.SIGUSR1 {
		t.Errorf("signalProcess called with (%d, %v), want (%d, SIGUSR1)", got.pid, got.sig, os.Getpid())
	}
	if !strings.Contains(out.String(), "recording cancelled") {
		t.Errorf("expected cancellation notice, got: %q", out.String())
	}
}

func TestLogCancelRecordingNoActiveRecording(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)

	var out bytes.Buffer
	if err := runLogCancelRecording(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no active recording") {
		t.Errorf("expected no-active-recording notice, got: %q", out.String())
	}
}

func TestLogStatusIdle(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)

	var out bytes.Buffer
	if err := runLogStatus(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "idle" {
		t.Errorf("expected idle, got: %q", out.String())
	}
}

func TestLogStatusRecording(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	if err := audio.WriteLock(audio.LockState{PID: os.Getpid(), WAVPath: "/tmp/x.wav", StartedAt: time.Now().Add(-5 * time.Second)}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runLogStatus(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "recording") || !strings.Contains(out.String(), "/tmp/x.wav") {
		t.Errorf("expected recording status with wav path, got: %q", out.String())
	}
}

func TestLogStatusStaleLock(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	pid := deadPID(t)
	if err := audio.WriteLock(audio.LockState{PID: pid, WAVPath: "/tmp/x.wav", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runLogStatus(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stale lock") {
		t.Errorf("expected stale-lock notice, got: %q", out.String())
	}
}

// --- Daemon lifecycle tests ---

func waitDaemon(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runLogDaemon")
		return nil
	}
}

func TestLogDaemonSIGINTHandsOffToPipeline(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	pipeline := stubSpawnPipeline(t)

	fr := &audio.FakeRecorder{}
	sigCh := make(chan os.Signal, 1)
	sigCh <- syscall.SIGINT

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runLogDaemon(context.Background(), cfg, fr, "/tmp/rec.wav", sigCh, &out)
	}()

	if err := waitDaemon(t, done); err != nil {
		t.Fatal(err)
	}
	if !fr.StopCalled {
		t.Error("expected Stop() to be called on SIGINT")
	}
	if pipeline.calls != 1 || pipeline.wavPath != "/tmp/rec.wav" {
		t.Errorf("expected pipeline hand-off for /tmp/rec.wav, got calls=%d wavPath=%q", pipeline.calls, pipeline.wavPath)
	}
	if _, err := audio.ReadLock(); !errors.Is(err, os.ErrNotExist) {
		t.Error("lockfile should be removed after the daemon exits")
	}
}

func TestLogDaemonSIGUSR1CancelsAndDeletesWAV(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	pipeline := stubSpawnPipeline(t)

	wavPath := filepath.Join(t.TempDir(), "rec.wav")
	if err := os.WriteFile(wavPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	fr := &audio.FakeRecorder{}
	sigCh := make(chan os.Signal, 1)
	sigCh <- syscall.SIGUSR1

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runLogDaemon(context.Background(), cfg, fr, wavPath, sigCh, &out)
	}()

	if err := waitDaemon(t, done); err != nil {
		t.Fatal(err)
	}
	if !fr.CancelCalled {
		t.Error("expected Cancel() to be called on SIGUSR1")
	}
	if pipeline.calls != 0 {
		t.Error("pipeline should not run on cancel")
	}
	if _, err := os.Stat(wavPath); !os.IsNotExist(err) {
		t.Error("wav file should be deleted on cancel")
	}
	if !strings.Contains(out.String(), "recording cancelled") {
		t.Errorf("expected cancellation notice, got: %q", out.String())
	}
}

func TestLogDaemonNaturalEndHandsOffToPipeline(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	pipeline := stubSpawnPipeline(t)

	waitCh := make(chan struct{})
	close(waitCh) // simulates the recorder ending on its own (e.g. duration cap)
	fr := &audio.FakeRecorder{WaitCh: waitCh}
	sigCh := make(chan os.Signal, 1)

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runLogDaemon(context.Background(), cfg, fr, "/tmp/rec.wav", sigCh, &out)
	}()

	if err := waitDaemon(t, done); err != nil {
		t.Fatal(err)
	}
	if pipeline.calls != 1 {
		t.Errorf("expected pipeline hand-off on natural end, got %d calls", pipeline.calls)
	}
}

func TestLogDaemonRecordFailureSkipsPipeline(t *testing.T) {
	cfg := testRepo(t, nil)
	isolateLock(t)
	pipeline := stubSpawnPipeline(t)

	waitCh := make(chan struct{})
	close(waitCh)
	fr := &audio.FakeRecorder{WaitCh: waitCh, Err: errors.New("mic permission denied")}
	sigCh := make(chan os.Signal, 1)

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runLogDaemon(context.Background(), cfg, fr, "/tmp/rec.wav", sigCh, &out)
	}()

	err := waitDaemon(t, done)
	if err == nil {
		t.Fatal("expected an error to propagate from a failed recording")
	}
	if pipeline.calls != 0 {
		t.Error("pipeline should not run when the recorder failed")
	}
	if _, lockErr := audio.ReadLock(); !errors.Is(lockErr, os.ErrNotExist) {
		t.Error("lockfile should still be removed after a recording failure")
	}
}

func TestLogDaemonKeepWAVPassedToPipeline(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Audio.KeepWAV = true
	isolateLock(t)
	pipeline := stubSpawnPipeline(t)

	fr := &audio.FakeRecorder{}
	sigCh := make(chan os.Signal, 1)
	sigCh <- syscall.SIGINT

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runLogDaemon(context.Background(), cfg, fr, "/tmp/rec.wav", sigCh, &out)
	}()

	if err := waitDaemon(t, done); err != nil {
		t.Fatal(err)
	}
	if !pipeline.keepWAV {
		t.Error("expected keepWAV=true to be passed to spawnPipeline when log.audio.keep_wav is set")
	}
}

// --- WAV retention / frontmatter (scratch pipeline) tests ---

func TestLogAudioScratchDeletesWAVOnSuccessByDefault(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = false

	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "scratch recording cleanup test"}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	if _, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, true, false, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(audioPath); !os.IsNotExist(err) {
		t.Error("scratch wav should be deleted after a successful pipeline run")
	}
}

func TestLogAudioNonScratchNeverDeletesWAV(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = false

	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "manually supplied wav is never deleted"}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	// scratch=false: this mirrors a user directly running `journal log <file>.wav`.
	if _, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, false, false, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(audioPath); err != nil {
		t.Errorf("a directly-supplied wav must never be auto-deleted: %v", err)
	}
}

func TestLogAudioScratchKeepWAVRetainsFileAndAddsFrontmatter(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Log.Shaping.Enabled = false

	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "keep wav test"}
	fake := embed.NewFake(cfg.EmbedDim)

	var out bytes.Buffer
	if _, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, true, true, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(audioPath); err != nil {
		t.Errorf("wav should be retained when keepWAV=true: %v", err)
	}

	entries, _ := os.ReadDir(cfg.LogAbsPath())
	if len(entries) == 0 {
		t.Fatal("no files in logs dir")
	}
	content, err := os.ReadFile(filepath.Join(cfg.LogAbsPath(), entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "audio: \""+audioPath+"\"") {
		t.Errorf("expected audio: frontmatter with wav path, got:\n%s", string(content))
	}
}

func TestLogAudioScratchEmptyTranscriptDiscardsWAV(t *testing.T) {
	cfg := testRepo(t, nil)

	audioPath := filepath.Join(t.TempDir(), "silence.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{Reply: "   "}
	fake := embed.NewFake(cfg.EmbedDim)
	stubNewNotifier(t)

	var out bytes.Buffer
	// Even with keepWAV=true, an empty/silent transcript discards the scratch wav.
	if _, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, true, true, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(audioPath); !os.IsNotExist(err) {
		t.Error("scratch wav should be discarded for an empty transcript")
	}
}

func TestLogAudioScratchTranscriptionErrorKeepsWAV(t *testing.T) {
	cfg := testRepo(t, nil)

	audioPath := filepath.Join(t.TempDir(), "note.wav")
	if err := os.WriteFile(audioPath, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &jlog.FakeTranscriber{ForcedErr: errors.New("transcription backend unavailable")}
	fake := embed.NewFake(cfg.EmbedDim)
	stubNewNotifier(t)

	var out bytes.Buffer
	_, err := runLogAudio(context.Background(), cfg, fake, ft, nil, audioPath, true, false, &out)
	if err == nil {
		t.Fatal("expected transcription error")
	}
	if _, statErr := os.Stat(audioPath); statErr != nil {
		t.Errorf("wav must be kept on transcription error even for a scratch recording: %v", statErr)
	}
}
