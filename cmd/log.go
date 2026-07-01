package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ericmann/journal/internal/audio"
	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	jlog "github.com/ericmann/journal/internal/log"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/synth"
	"github.com/spf13/cobra"
)

var (
	logText   string
	logStart  bool
	logStop   bool
	logCancel bool
	logStatus bool
	// logKeepWAV and logScratch are internal, hidden pass-through flags used
	// only by the daemon's re-invocation of `journal log <wavPath>` — never
	// set directly by a user. logScratch marks the WAV as a recorder-produced
	// scratch file eligible for cleanup/`audio:` frontmatter; a WAV passed
	// directly on the command line is never touched.
	logKeepWAV bool
	logScratch bool
	// logDaemonWAV is the hidden internal entry point for the background
	// recorder process spawned by runLogStartRecording.
	logDaemonWAV string
)

var logCmd = &cobra.Command{
	Use:   "log [audio.wav]",
	Short: "Capture a voice note (bare command toggles mic recording; --text for typed input; pass an audio file to transcribe)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()

		switch {
		case logDaemonWAV != "":
			// Deliberately context.Background(), not cmd.Context(): the root
			// command wires SIGINT/SIGTERM to a cancellable context (see
			// root.go) so long-running commands abort promptly. The daemon
			// intercepts those same signals itself to finalize the recording
			// gracefully (see runLogDaemon) — if it also inherited a
			// signal-cancelled context, ctx cancellation would race the
			// daemon's own graceful stop and SIGKILL the ffmpeg process via
			// exec.CommandContext's default Cancel behavior, corrupting the
			// WAV on an ordinary stop press.
			return runLogDaemon(context.Background(), cfg, newRecorder(cfg), logDaemonWAV, nil, out)
		case logStart:
			return runLogStartRecording(cfg, out)
		case logStop:
			return runLogStopRecording(cfg, out)
		case logCancel:
			return runLogCancelRecording(cfg, out)
		case logStatus:
			return runLogStatus(cfg, out)
		}

		if len(args) == 1 {
			// Audio file path provided — transcribe then land. logScratch/
			// logKeepWAV are only ever true when the daemon re-invokes us.
			return runLogAudio(cmd.Context(), cfg, newEmbedder(cfg), newTranscriber(cfg), nil, args[0], logScratch, logKeepWAV, out)
		}

		if strings.TrimSpace(logText) == "" {
			// No text, no audio file, no explicit flag: toggle recording.
			return runLogToggle(cfg, out)
		}
		return runLogText(cmd.Context(), cfg, newEmbedder(cfg), nil, logText, out)
	},
}

// newRecorder builds the live Recorder from cfg. Tests inject an
// audio.FakeRecorder directly into runLogDaemon instead of calling this.
func newRecorder(cfg *config.Config) audio.Recorder {
	return audio.FfmpegRecorder{SilenceAutostop: cfg.Log.Audio.SilenceAutostop}
}

// checkFfmpegAvailable reports whether ffmpeg can be found. Injectable so
// tests never depend on (or require the absence of) a real ffmpeg install.
var checkFfmpegAvailable = func() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found in PATH; install it (e.g. `brew install ffmpeg`) before recording: %w", err)
	}
	return nil
}

// spawnDetached re-invokes the current binary with args as a background
// process that survives after the current process exits (a new session, and
// stdio redirected to /dev/null).
func spawnDetached(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable: %w", err)
	}
	c := exec.Command(exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		c.Stdin, c.Stdout, c.Stderr = devnull, devnull, devnull
	}
	return c.Start()
}

// spawnDaemon starts `journal log --_daemon <wavPath>` as a detached
// background process. Injectable for tests.
var spawnDaemon = func(wavPath string) error {
	return spawnDetached("log", "--_daemon", wavPath)
}

// spawnPipeline hands a finalized recording off to `journal log <wavPath>` as
// a detached process, so the daemon can exit immediately after signaling it.
// Injectable for tests.
var spawnPipeline = func(wavPath string, keepWAV bool) error {
	args := []string{"log", "--_scratch", wavPath}
	if keepWAV {
		args = append(args, "--keep-wav")
	}
	return spawnDetached(args...)
}

// randHex returns n random bytes hex-encoded, used to make scratch WAV
// filenames collision-resistant.
func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// runLogToggle implements the bare `journal log` UX: start a recording if
// idle, stop one if active, or recover from a stale (dead-PID) lock.
func runLogToggle(cfg *config.Config, out io.Writer) error {
	lock, err := audio.ReadLock()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return runLogStartRecording(cfg, out)
		}
		return fmt.Errorf("reading recording lock: %w", err)
	}
	if audio.PIDAlive(lock.PID) {
		return runLogStopRecording(cfg, out)
	}
	// Stale-lock cleanup + notice happens inside runLogStartRecording.
	return runLogStartRecording(cfg, out)
}

// runLogStartRecording starts a new recording: it spawns the background
// daemon (via spawnDaemon) and returns as soon as the lockfile appears (or a
// short timeout elapses), so the hotkey press returns immediately. A stale
// (dead-PID) lock is detected and cleaned up before starting.
func runLogStartRecording(cfg *config.Config, out io.Writer) error {
	if lock, err := audio.ReadLock(); err == nil {
		if audio.PIDAlive(lock.PID) {
			fmt.Fprintln(out, "already recording")
			return nil
		}
		fmt.Fprintf(out, "stale lock found (recorder pid %d is no longer running) — cleaning up\n", lock.PID)
		if err := audio.RemoveLock(); err != nil {
			return fmt.Errorf("removing stale recording lock: %w", err)
		}
	}
	if err := checkFfmpegAvailable(); err != nil {
		return err
	}

	tmpDir, err := cfg.LogAudioTmpDirAbs()
	if err != nil {
		return err
	}
	wavPath := filepath.Join(tmpDir, fmt.Sprintf("%s-%s.wav", now().UTC().Format("20060102T150405"), randHex(3)))

	if err := spawnDaemon(wavPath); err != nil {
		return fmt.Errorf("starting recorder: %w", err)
	}

	// Brief poll for the lockfile to appear — closes the race where a second
	// press arrives before the daemon has written it.
	for i := 0; i < 50; i++ {
		if _, err := audio.ReadLock(); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	fmt.Fprintln(out, "● recording")
	return nil
}

// activeLockPID reads the lockfile and reports the recorder's pid when a
// recording is genuinely active. It handles the two "nothing to signal"
// cases itself — no lock, or a stale (dead-PID) lock, which it cleans up —
// printing the matching notice and returning active=false.
func activeLockPID(out io.Writer) (pid int, active bool, err error) {
	lock, err := audio.ReadLock()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(out, "no active recording")
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("reading recording lock: %w", err)
	}
	if !audio.PIDAlive(lock.PID) {
		fmt.Fprintf(out, "stale lock found (recorder pid %d is no longer running) — cleaning up\n", lock.PID)
		if err := audio.RemoveLock(); err != nil {
			return 0, false, fmt.Errorf("removing stale recording lock: %w", err)
		}
		fmt.Fprintln(out, "no active recording")
		return 0, false, nil
	}
	return lock.PID, true, nil
}

// runLogStopRecording signals the active recorder (SIGINT) to finalize the
// WAV and hand off to the pipeline asynchronously. It returns as soon as the
// signal is sent — the pipeline itself runs in the (already backgrounded)
// daemon process.
func runLogStopRecording(cfg *config.Config, out io.Writer) error {
	pid, active, err := activeLockPID(out)
	if err != nil || !active {
		return err
	}
	if err := signalProcess(pid, syscall.SIGINT); err != nil {
		return fmt.Errorf("stopping recording: %w", err)
	}
	fmt.Fprintln(out, "stopping recording…")
	return nil
}

// runLogCancelRecording signals the active recorder (SIGUSR1) to finalize and
// discard the WAV — no note is produced.
func runLogCancelRecording(cfg *config.Config, out io.Writer) error {
	pid, active, err := activeLockPID(out)
	if err != nil || !active {
		return err
	}
	if err := signalProcess(pid, syscall.SIGUSR1); err != nil {
		return fmt.Errorf("cancelling recording: %w", err)
	}
	fmt.Fprintln(out, "recording cancelled")
	return nil
}

// runLogStatus reports whether a recording is active, its elapsed time, and
// the scratch WAV path.
func runLogStatus(cfg *config.Config, out io.Writer) error {
	lock, err := audio.ReadLock()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(out, "idle")
			return nil
		}
		return fmt.Errorf("reading recording lock: %w", err)
	}
	if !audio.PIDAlive(lock.PID) {
		fmt.Fprintf(out, "stale lock (recorder pid %d is no longer running); run `journal log --stop` to clean up\n", lock.PID)
		return nil
	}
	elapsed := now().Sub(lock.StartedAt).Round(time.Second)
	fmt.Fprintf(out, "recording (%s) — %s\n", elapsed, lock.WAVPath)
	return nil
}

// signalProcess sends sig to pid. Injectable for tests.
var signalProcess = func(pid int, sig syscall.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}

// runLogDaemon is the background recorder process: it writes the lockfile,
// runs rec until stopped/cancelled/capped, then either hands off to the
// pipeline (detached) or discards the WAV. sigCh is injectable for tests; a
// nil value sets up real OS signal delivery (SIGINT/SIGTERM/SIGUSR1).
func runLogDaemon(ctx context.Context, cfg *config.Config, rec audio.Recorder, wavPath string, sigCh chan os.Signal, out io.Writer) error {
	if sigCh == nil {
		sigCh = make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
		defer signal.Stop(sigCh)
	}

	if err := audio.WriteLock(audio.LockState{PID: os.Getpid(), WAVPath: wavPath, StartedAt: now()}); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}
	defer func() { _ = audio.RemoveLock() }()

	maxDuration := time.Duration(cfg.Log.Audio.MaxDuration) * time.Second
	errCh, stop, cancel := rec.Record(ctx, wavPath, cfg.Log.Audio.Device, cfg.Log.Audio.SampleRate, cfg.Log.Audio.Channels, maxDuration)

	if maxDuration > 0 {
		timer := time.AfterFunc(maxDuration, func() { _ = stop() })
		defer timer.Stop()
	}

	select {
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(out, "recording failed: %v\n", err)
			return err
		}
		return handoffPipeline(cfg, wavPath, out)

	case sig := <-sigCh:
		if sig == syscall.SIGUSR1 {
			_ = cancel()
			<-errCh
			if rmErr := os.Remove(wavPath); rmErr != nil && !os.IsNotExist(rmErr) {
				fmt.Fprintf(out, "  (wav cleanup failed: %v)\n", rmErr)
			}
			fmt.Fprintln(out, "recording cancelled")
			return nil
		}

		_ = stop()
		if err := <-errCh; err != nil {
			fmt.Fprintf(out, "recording failed: %v\n", err)
			return err
		}
		return handoffPipeline(cfg, wavPath, out)
	}
}

// handoffPipeline spawns the detached transcribe→shape→land→index pipeline
// for a finalized recording, so the daemon can exit immediately.
func handoffPipeline(cfg *config.Config, wavPath string, out io.Writer) error {
	if err := spawnPipeline(wavPath, cfg.Log.Audio.KeepWAV); err != nil {
		return fmt.Errorf("handing off to pipeline: %w", err)
	}
	fmt.Fprintln(out, "processing…")
	return nil
}

// newTranscriber builds the live Transcriber from cfg. Tests inject a
// FakeTranscriber directly into runLogAudio instead of calling this.
func newTranscriber(cfg *config.Config) jlog.Transcriber {
	return jlog.NewWhisperCPP(cfg.LogTranscriberModelDirAbs(), cfg.Log.Transcriber.Model)
}

// runLogAudio orchestrates the transcribe→shape→assemble→land→index pipeline
// for an audio file argument. tr is injectable for tests (pass nil to build
// from cfg). client is injectable for synthesis shaping (pass nil to build
// from cfg). scratch marks audioPath as a recorder-produced temp file
// (eligible for cleanup / `audio:` frontmatter via keepWAV) — a WAV passed
// directly by the user on the command line is never deleted.
func runLogAudio(ctx context.Context, cfg *config.Config, e embed.Embedder, tr jlog.Transcriber, client synth.Client, audioPath string, scratch, keepWAV bool, out io.Writer) error {
	// Validate the audio file exists.
	if _, err := os.Stat(audioPath); err != nil {
		return fmt.Errorf("audio file %q not found: %w", audioPath, err)
	}

	capturedAt := now()

	// Transcribe. On error: notify, keep the WAV, return so the user can retry.
	text, durationSec, err := tr.Transcribe(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("transcription failed (audio kept, retryable): %w", err)
	}

	// Empty/silent transcript — skip the pipeline and discard a scratch WAV
	// (nothing was learned from it, so there's nothing to keep).
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(out, "  (empty transcript — nothing to log)")
		if scratch {
			if rmErr := os.Remove(audioPath); rmErr != nil && !os.IsNotExist(rmErr) {
				fmt.Fprintf(out, "  (wav cleanup failed: %v)\n", rmErr)
			}
		}
		return nil
	}

	transcriberName := tr.Name()

	// Build synthesis client for shaping (unless disabled or already provided).
	var shaped bool
	var sr jlog.ShapeResult

	if cfg.Log.Shaping.Enabled {
		if client == nil {
			if c, err := synthClient(cfg); err == nil {
				client = c
			}
		}
		voiceProfile := readVoiceProfile(cfg)
		var err error
		sr, shaped, err = jlog.Shape(ctx, client, cfg.ActiveSynthModel(), cfg.SynthMaxTokens,
			text, voiceProfile, cfg.LocalOnly)
		if err != nil {
			fmt.Fprintf(out, "  (shaping failed: %v; landing raw transcript)\n", err)
		}
	}

	// Assemble the voice note document.
	in := jlog.AssembleInput{
		RawText:           text,
		DurationSec:       durationSec,
		Transcriber:       transcriberName,
		KeepRawTranscript: cfg.Log.Shaping.KeepRawTranscript,
		CapturedAt:        capturedAt,
	}
	if shaped {
		in.Title = sr.Title
		in.Summary = sr.Summary
		in.Body = sr.Body
		in.Tags = sr.Tags
		in.Markers = sr.Markers
	}
	// A retained scratch recording is recorded in the note's frontmatter.
	if scratch && keepWAV {
		in.AudioPath = audioPath
	}
	doc := jlog.Assemble(in)

	// Compute filename and paths.
	filename := jlog.Filename(capturedAt, in.Title, text)
	absDir := cfg.LogAbsPath()
	relPath := filepath.ToSlash(filepath.Join(cfg.LogRelPath(), filename))

	// Land the note.
	absPath, err := jlog.Land(absDir, filename, []byte(doc))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "logged: %s\n", relPath)

	// The note is safely landed — a scratch recording can now be cleaned up
	// unless the caller asked to keep it.
	if scratch && !keepWAV {
		if rmErr := os.Remove(audioPath); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(out, "  (wav cleanup failed: %v)\n", rmErr)
		}
	}

	// Optional daily backlink.
	if cfg.Log.Landing.BacklinkDaily {
		dailyPath := note.DailyPath(cfg.Root(), capturedAt)
		if berr := jlog.AppendBacklink(dailyPath, relPath, capturedAt); berr != nil {
			fmt.Fprintf(out, "  (backlink failed: %v)\n", berr)
		}
	}

	// Index the note (non-fatal on failure — note is already landed).
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		fmt.Fprintf(out, "  (index skipped: %v — run `journal index` to index later)\n", err)
		return nil
	}
	defer s.Close()

	mtime := capturedAt
	if fi, err := os.Stat(absPath); err == nil {
		mtime = fi.ModTime()
	}
	ix := index.NewIndexer(s, e)
	st, err := jlog.IndexVoice(ctx, ix, relPath, doc, mtime)
	if err != nil {
		fmt.Fprintf(out, "  (index failed: %v — run `journal index` to index later)\n", err)
		return nil
	}
	fmt.Fprintf(out, "  indexed: %d chunk(s) embedded\n", st.Embedded)
	return nil
}

// runLogText orchestrates the shape→assemble→land→index pipeline for --text input.
// When client is nil and shaping is enabled, a synthesis client is built from cfg.
func runLogText(ctx context.Context, cfg *config.Config, e embed.Embedder, client synth.Client, rawText string, out io.Writer) error {
	capturedAt := now()

	// Build synthesis client for shaping (unless disabled or already provided).
	var shaped bool
	var sr jlog.ShapeResult

	if cfg.Log.Shaping.Enabled {
		if client == nil {
			// Attempt to build a synthesis client; failure is non-fatal (raw fallback).
			if c, err := synthClient(cfg); err == nil {
				client = c
			}
		}
		voiceProfile := readVoiceProfile(cfg)
		var err error
		sr, shaped, err = jlog.Shape(ctx, client, cfg.ActiveSynthModel(), cfg.SynthMaxTokens,
			rawText, voiceProfile, cfg.LocalOnly)
		if err != nil {
			// Non-fatal: warn and fall back to raw land.
			fmt.Fprintf(out, "  (shaping failed: %v; landing raw text)\n", err)
		}
	}

	// Assemble the voice note document.
	in := jlog.AssembleInput{
		RawText:           rawText,
		DurationSec:       0,
		Transcriber:       "text",
		KeepRawTranscript: cfg.Log.Shaping.KeepRawTranscript,
		CapturedAt:        capturedAt,
	}
	if shaped {
		in.Title = sr.Title
		in.Summary = sr.Summary
		in.Body = sr.Body
		in.Tags = sr.Tags
		in.Markers = sr.Markers
	}
	doc := jlog.Assemble(in)

	// Compute filename and paths.
	filename := jlog.Filename(capturedAt, in.Title, rawText)
	absDir := cfg.LogAbsPath()
	relPath := filepath.ToSlash(filepath.Join(cfg.LogRelPath(), filename))

	// Land the note.
	absPath, err := jlog.Land(absDir, filename, []byte(doc))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "logged: %s\n", relPath)

	// Optional daily backlink.
	if cfg.Log.Landing.BacklinkDaily {
		dailyPath := note.DailyPath(cfg.Root(), capturedAt)
		if berr := jlog.AppendBacklink(dailyPath, relPath, capturedAt); berr != nil {
			fmt.Fprintf(out, "  (backlink failed: %v)\n", berr)
		}
	}

	// Index the note (non-fatal on failure — note is already landed).
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		fmt.Fprintf(out, "  (index skipped: %v — run `journal index` to index later)\n", err)
		return nil
	}
	defer s.Close()

	mtime := capturedAt
	if fi, err := os.Stat(absPath); err == nil {
		mtime = fi.ModTime()
	}
	ix := index.NewIndexer(s, e)
	st, err := jlog.IndexVoice(ctx, ix, relPath, doc, mtime)
	if err != nil {
		fmt.Fprintf(out, "  (index failed: %v — run `journal index` to index later)\n", err)
		return nil
	}
	fmt.Fprintf(out, "  indexed: %d chunk(s) embedded\n", st.Embedded)
	return nil
}

func init() {
	logCmd.Flags().StringVar(&logText, "text", "", "typed text to capture as a voice note")
	logCmd.Flags().BoolVar(&logStart, "start", false, "start recording (no-op if already recording)")
	logCmd.Flags().BoolVar(&logStop, "stop", false, "stop the active recording and process it")
	logCmd.Flags().BoolVar(&logCancel, "cancel", false, "cancel the active recording and discard the audio")
	logCmd.Flags().BoolVar(&logStatus, "status", false, "report whether a recording is active")

	// Internal flags used only by the daemon's self re-invocation.
	logCmd.Flags().StringVar(&logDaemonWAV, "_daemon", "", "internal: run as the background recorder for the given wav path")
	logCmd.Flags().BoolVar(&logKeepWAV, "keep-wav", false, "internal: retain the wav after processing and record it in frontmatter")
	logCmd.Flags().BoolVar(&logScratch, "_scratch", false, "internal: audioPath is a recorder-produced scratch file eligible for cleanup")
	_ = logCmd.Flags().MarkHidden("_daemon")
	_ = logCmd.Flags().MarkHidden("keep-wav")
	_ = logCmd.Flags().MarkHidden("_scratch")

	rootCmd.AddCommand(logCmd)
}
