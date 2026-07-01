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

// silenceAutostopThreshold is how long a single continuous silence interval
// must be before the silence-autostop safety net finalizes the recording.
const silenceAutostopThreshold = 30 * time.Second

// FfmpegRecorder records via `ffmpeg -f avfoundation` — macOS only at
// runtime. It is never exercised in tests; internal/audio tests use
// FakeRecorder instead.
type FfmpegRecorder struct {
	// SilenceAutostop enables the silencedetect watchdog: after
	// silenceAutostopThreshold of continuous silence, the recording is
	// stopped automatically as a safety net (not the primary stop mechanism).
	SilenceAutostop bool
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

	args := []string{
		"-hide_banner", "-loglevel", "info",
		"-f", "avfoundation",
		"-i", ":" + device,
		"-ar", strconv.Itoa(sampleRate),
		"-ac", strconv.Itoa(channels),
		"-sample_fmt", "s16",
	}
	if maxDuration > 0 {
		args = append(args, "-t", strconv.FormatFloat(maxDuration.Seconds(), 'f', -1, 64))
	}
	if r.SilenceAutostop {
		args = append(args, "-af", fmt.Sprintf("silencedetect=noise=-35dB:d=%d", int(silenceAutostopThreshold.Seconds())))
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
		go watchSilence(stderr, silenceAutostopThreshold, signalFn)
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
