package audio

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func waitErr(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for errCh")
		return nil
	}
}

func TestFakeRecorderWaitChUnblocksRecord(t *testing.T) {
	waitCh := make(chan struct{})
	fr := &FakeRecorder{WaitCh: waitCh}

	errCh, _, _ := fr.Record(context.Background(), "note.wav", "default", 16000, 1, 0)
	close(waitCh)

	if err := waitErr(t, errCh); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestFakeRecorderStopUnblocksRecordAndSetsFlag(t *testing.T) {
	fr := &FakeRecorder{}
	errCh, stop, _ := fr.Record(context.Background(), "note.wav", "default", 16000, 1, 0)

	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if !fr.StopCalled {
		t.Error("StopCalled = false after Stop()")
	}
	if fr.CancelCalled {
		t.Error("CancelCalled should remain false")
	}
	if err := waitErr(t, errCh); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestFakeRecorderCancelUnblocksRecordAndSetsFlag(t *testing.T) {
	fr := &FakeRecorder{}
	errCh, _, cancel := fr.Record(context.Background(), "note.wav", "default", 16000, 1, 0)

	if err := cancel(); err != nil {
		t.Fatal(err)
	}
	if !fr.CancelCalled {
		t.Error("CancelCalled = false after Cancel()")
	}
	if fr.StopCalled {
		t.Error("StopCalled should remain false")
	}
	if err := waitErr(t, errCh); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestFakeRecorderReturnsConfiguredError(t *testing.T) {
	wantErr := errors.New("mic permission denied")
	fr := &FakeRecorder{Err: wantErr}
	errCh, stop, _ := fr.Record(context.Background(), "note.wav", "default", 16000, 1, 0)
	_ = stop()

	if err := waitErr(t, errCh); !errors.Is(err, wantErr) {
		t.Errorf("errCh = %v, want %v", err, wantErr)
	}
}

func TestFakeRecorderContextCancellationUnblocksRecord(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fr := &FakeRecorder{}
	errCh, _, _ := fr.Record(ctx, "note.wav", "default", 16000, 1, 0)

	cancel()

	err := waitErr(t, errCh)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errCh = %v, want context.Canceled", err)
	}
}

func TestWatchSilenceStopsOnceThresholdReached(t *testing.T) {
	// A realistic ffmpeg silencedetect transcript: a short silence interval
	// below threshold, then one at/above it.
	stderr := strings.NewReader(strings.Join([]string{
		"[silencedetect @ 0x0] silence_start: 1.2",
		"[silencedetect @ 0x0] silence_end: 3.4 | silence_duration: 2.2",
		"[silencedetect @ 0x0] silence_start: 10.0",
		"[silencedetect @ 0x0] silence_end: 40.5 | silence_duration: 30.5",
		"[silencedetect @ 0x0] silence_start: 50.0",
	}, "\n"))

	stopCalls := 0
	stop := func() error {
		stopCalls++
		return nil
	}

	watchSilence(stderr, 30*time.Second, stop)

	if stopCalls != 1 {
		t.Errorf("stop called %d times, want exactly 1 (on the first interval >= threshold)", stopCalls)
	}
}

func TestWatchSilenceNeverStopsBelowThreshold(t *testing.T) {
	stderr := strings.NewReader(strings.Join([]string{
		"[silencedetect @ 0x0] silence_start: 1.2",
		"[silencedetect @ 0x0] silence_end: 3.4 | silence_duration: 2.2",
		"[silencedetect @ 0x0] silence_start: 10.0",
		"[silencedetect @ 0x0] silence_end: 15.0 | silence_duration: 5.0",
	}, "\n"))

	stopCalls := 0
	watchSilence(stderr, 30*time.Second, func() error { stopCalls++; return nil })

	if stopCalls != 0 {
		t.Errorf("stop called %d times, want 0 (no interval reached the threshold)", stopCalls)
	}
}

func TestWatchSilenceIgnoresUnrelatedOutput(t *testing.T) {
	stderr := strings.NewReader(strings.Join([]string{
		"ffmpeg version 6.0",
		"Input #0, avfoundation, from ':0':",
		"  Stream #0:0: Audio: pcm_f32le, 44100 Hz, mono",
		"size=     128kB time=00:00:08.00 bitrate= 131.1kbits/s",
	}, "\n"))

	stopCalls := 0
	watchSilence(stderr, 30*time.Second, func() error { stopCalls++; return nil })

	if stopCalls != 0 {
		t.Errorf("stop called %d times on unrelated ffmpeg output, want 0", stopCalls)
	}
}
