package audio

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// StopFn gracefully ends a recording so the WAV finalizes cleanly; the caller
// then hands the file off to the transcription pipeline.
type StopFn func() error

// CancelFn gracefully ends a recording so the WAV finalizes cleanly, but the
// caller is expected to discard the file rather than process it.
type CancelFn func() error

// Recorder starts capturing audio to a WAV file. Implementations are used for
// exactly one recording per Record call.
type Recorder interface {
	// Record starts recording device audio to wavPath at sampleRate/channels
	// (16-bit PCM) and returns immediately. errCh receives the terminal
	// result once recording ends — via Stop, Cancel, maxDuration, or the
	// underlying process exiting on its own — with nil meaning a clean
	// finalize. maxDuration <= 0 means no cap.
	Record(ctx context.Context, wavPath, device string, sampleRate, channels int, maxDuration time.Duration) (errCh <-chan error, stop StopFn, cancel CancelFn)
}

// defaultSilenceDuration and defaultSilenceNoiseDB mirror
// internal/config.Default's log.audio.{silence_duration,silence_noise_db}
// and are used by callers that construct FfmpegRecorder directly (e.g.
// tests) without threading config values through.
const (
	defaultSilenceDuration = 30 * time.Second
	defaultSilenceNoiseDB  = -35
)

// ResolveBackend picks the ffmpeg input backend: cfgBackend verbatim if set
// (already validated by config.Validate), else a GOOS-based default. goos is
// a parameter (not runtime.GOOS read internally) so both branches are
// unit-testable regardless of the OS running the test.
func ResolveBackend(cfgBackend, goos string) (string, error) {
	if cfgBackend != "" {
		return cfgBackend, nil
	}
	switch goos {
	case "darwin":
		return "avfoundation", nil
	case "linux":
		return "pulse", nil
	default:
		return "", fmt.Errorf("recording is not supported on GOOS %q (supported: darwin, linux)", goos)
	}
}

// FfmpegRecorder records via ffmpeg, using the backend named in Backend
// ("avfoundation" on macOS, "pulse"/"alsa" on Linux — see ResolveBackend).
// It is never exercised in tests; internal/audio tests use FakeRecorder
// instead.
type FfmpegRecorder struct {
	// Backend is the ffmpeg input format (e.g. "avfoundation", "pulse",
	// "alsa"). Empty is invalid at this layer — callers must resolve it
	// first via ResolveBackend.
	Backend string
	// SilenceAutostop enables the silencedetect watchdog: after
	// SilenceDuration of continuous silence, the recording is stopped
	// automatically as a safety net (not the primary stop mechanism).
	SilenceAutostop bool
	// SilenceDuration is how long a continuous silence interval must be
	// before the watchdog fires. Zero or negative falls back to
	// defaultSilenceDuration.
	SilenceDuration time.Duration
	// SilenceNoiseDB is the silencedetect noise floor in dB. Zero falls back
	// to defaultSilenceNoiseDB (0dB is not a meaningful noise floor — it
	// would treat only true digital silence as silent).
	SilenceNoiseDB int
}

// silenceThreshold returns the configured silence duration, falling back to
// defaultSilenceDuration when unset.
func (r FfmpegRecorder) silenceThreshold() time.Duration {
	if r.SilenceDuration <= 0 {
		return defaultSilenceDuration
	}
	return r.SilenceDuration
}

// silenceNoiseDB returns the configured noise floor in dB, falling back to
// defaultSilenceNoiseDB when unset (0dB is not a meaningful noise floor — it
// would treat only true digital silence as silent).
func (r FfmpegRecorder) silenceNoiseDB() int {
	if r.SilenceNoiseDB == 0 {
		return defaultSilenceNoiseDB
	}
	return r.SilenceNoiseDB
}

// silencedetectFilter builds the ffmpeg `-af silencedetect=...` filter
// string for the recorder's configured noise floor and minimum silence
// duration.
func (r FfmpegRecorder) silencedetectFilter() string {
	return fmt.Sprintf("silencedetect=noise=%ddB:d=%d", r.silenceNoiseDB(), int(r.silenceThreshold().Seconds()))
}

// buildArgs constructs the ffmpeg argument list for capturing from backend,
// up through the input/format/duration flags shared by every backend. It
// does not append the silencedetect filter or the trailing output path —
// Record adds those itself, since silencedetect is gated on SilenceAutostop
// and the output path is the one arg that must come last.
//
// The only per-backend difference is the input spec: avfoundation addresses
// devices as ":<device>", while pulse/alsa take the device name directly (no
// ":" prefix — that syntax is avfoundation-specific).
func (r FfmpegRecorder) buildArgs(backend, device string, sampleRate, channels int, maxDuration time.Duration) []string {
	input := device
	if backend == "avfoundation" {
		input = ":" + device
	}
	args := []string{
		"-hide_banner", "-loglevel", "info",
		"-f", backend,
		"-i", input,
		"-ar", strconv.Itoa(sampleRate),
		"-ac", strconv.Itoa(channels),
		"-sample_fmt", "s16",
	}
	if maxDuration > 0 {
		args = append(args, "-t", strconv.FormatFloat(maxDuration.Seconds(), 'f', -1, 64))
	}
	return args
}

// Record implements Recorder.
func (r FfmpegRecorder) Record(ctx context.Context, wavPath, device string, sampleRate, channels int, maxDuration time.Duration) (<-chan error, StopFn, CancelFn) {
	errCh := make(chan error, 1)
	noop := func() error { return nil }

	binPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		errCh <- fmt.Errorf("ffmpeg not found in PATH; install it (e.g. `brew install ffmpeg`): %w", err)
		return errCh, noop, noop
	}

	args := r.buildArgs(r.Backend, device, sampleRate, channels, maxDuration)
	if r.SilenceAutostop {
		args = append(args, "-af", r.silencedetectFilter())
	}
	args = append(args, "-y", wavPath)

	cmd := exec.CommandContext(ctx, binPath, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		errCh <- fmt.Errorf("opening ffmpeg stderr: %w", err)
		return errCh, noop, noop
	}
	if err := cmd.Start(); err != nil {
		errCh <- fmt.Errorf("starting ffmpeg: %w", err)
		return errCh, noop, noop
	}

	signalFn := func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(syscall.SIGINT)
	}

	if r.SilenceAutostop {
		go watchSilence(stderr, r.silenceThreshold(), signalFn)
	} else {
		go func() { _, _ = io.Copy(io.Discard, stderr) }()
	}

	go func() { errCh <- cmd.Wait() }()

	return errCh, signalFn, signalFn
}

var silenceDurationRe = regexp.MustCompile(`silence_duration:\s*([0-9.]+)`)

// watchSilence scans ffmpeg's silencedetect stderr output and calls stop once
// a completed silence interval reaches threshold. This is a safety net, not
// the primary stopping mechanism.
func watchSilence(stderr io.Reader, threshold time.Duration, stop StopFn) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		m := silenceDurationRe.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		secs, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		if time.Duration(secs*float64(time.Second)) >= threshold {
			_ = stop()
			return
		}
	}
}

// FakeRecorder is a deterministic Recorder for tests — it never execs
// ffmpeg. Stop/Cancel unblock a pending Record call; WaitCh (if set) also
// unblocks it, simulating the underlying process exiting on its own (e.g. a
// duration cap).
type FakeRecorder struct {
	// Err is delivered on errCh when the recording ends.
	Err error
	// WaitCh, when non-nil, unblocks Record when closed (simulates the
	// recorder exiting on its own). Leave nil to require an explicit
	// Stop/Cancel call.
	WaitCh chan struct{}

	StopCalled   bool
	CancelCalled bool

	stopCh   chan struct{}
	stopOnce sync.Once
}

// Record implements Recorder.
func (f *FakeRecorder) Record(ctx context.Context, _, _ string, _, _ int, _ time.Duration) (<-chan error, StopFn, CancelFn) {
	errCh := make(chan error, 1)
	f.stopCh = make(chan struct{})
	f.stopOnce = sync.Once{}

	go func() {
		select {
		case <-f.WaitCh:
		case <-f.stopCh:
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		}
		errCh <- f.Err
	}()

	stop := func() error {
		f.StopCalled = true
		f.stopOnce.Do(func() { close(f.stopCh) })
		return nil
	}
	cancel := func() error {
		f.CancelCalled = true
		f.stopOnce.Do(func() { close(f.stopCh) })
		return nil
	}
	return errCh, stop, cancel
}
