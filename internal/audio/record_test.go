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

func TestFfmpegRecorderBuildArgs(t *testing.T) {
	tests := []struct {
		name        string
		backend     string
		device      string
		maxDuration time.Duration
		wantContain [][]string
		wantAbsent  []string
	}{
		{
			name:        "avfoundation uses colon-prefixed input spec",
			backend:     "avfoundation",
			device:      "0",
			wantContain: [][]string{{"-f", "avfoundation"}, {"-i", ":0"}},
		},
		{
			name:        "pulse uses bare device input spec",
			backend:     "pulse",
			device:      "default",
			wantContain: [][]string{{"-f", "pulse"}, {"-i", "default"}},
			wantAbsent:  []string{":default"},
		},
		{
			name:        "alsa uses bare device input spec",
			backend:     "alsa",
			device:      "hw:0,0",
			wantContain: [][]string{{"-f", "alsa"}, {"-i", "hw:0,0"}},
			wantAbsent:  []string{":hw:0,0"},
		},
		{
			name:        "max duration adds -t cap",
			backend:     "pulse",
			device:      "default",
			maxDuration: 90 * time.Second,
			wantContain: [][]string{{"-t", "90"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := FfmpegRecorder{}
			args := r.buildArgs(tt.backend, tt.device, 16000, 1, tt.maxDuration)

			joined := strings.Join(args, " ")
			for _, pair := range tt.wantContain {
				want := strings.Join(pair, " ")
				if !strings.Contains(joined, want) {
					t.Errorf("buildArgs(%q, %q) = %v, want to contain %q", tt.backend, tt.device, args, want)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(joined, absent) {
					t.Errorf("buildArgs(%q, %q) = %v, must not contain %q", tt.backend, tt.device, args, absent)
				}
			}
			if !strings.Contains(joined, "-ar 16000") || !strings.Contains(joined, "-ac 1") || !strings.Contains(joined, "-sample_fmt s16") {
				t.Errorf("buildArgs(%q, %q) = %v, want sample rate/channels/sample_fmt present", tt.backend, tt.device, args)
			}
		})
	}
}

func TestResolveBackend(t *testing.T) {
	tests := []struct {
		name       string
		cfgBackend string
		goos       string
		want       string
		wantErr    bool
	}{
		{name: "darwin default", cfgBackend: "", goos: "darwin", want: "avfoundation"},
		{name: "linux default", cfgBackend: "", goos: "linux", want: "pulse"},
		{name: "linux explicit alsa override", cfgBackend: "alsa", goos: "linux", want: "alsa"},
		{name: "explicit override wins even if GOOS-mismatched", cfgBackend: "avfoundation", goos: "linux", want: "avfoundation"},
		{name: "unsupported goos errors", cfgBackend: "", goos: "windows", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveBackend(tt.cfgBackend, tt.goos)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveBackend(%q, %q) = %q, nil, want an error", tt.cfgBackend, tt.goos, got)
				}
				if !strings.Contains(err.Error(), "not supported") || !strings.Contains(err.Error(), "darwin, linux") {
					t.Errorf("unexpected error message: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveBackend(%q, %q) unexpected error: %v", tt.cfgBackend, tt.goos, err)
			}
			if got != tt.want {
				t.Errorf("ResolveBackend(%q, %q) = %q, want %q", tt.cfgBackend, tt.goos, got, tt.want)
			}
		})
	}
}

func TestFfmpegRecorderSilencedetectFilter(t *testing.T) {
	tests := []struct {
		name string
		rec  FfmpegRecorder
		want string
	}{
		{
			name: "configured values",
			rec:  FfmpegRecorder{SilenceDuration: 12 * time.Second, SilenceNoiseDB: -20},
			want: "silencedetect=noise=-20dB:d=12",
		},
		{
			name: "zero values fall back to defaults",
			rec:  FfmpegRecorder{},
			want: "silencedetect=noise=-35dB:d=30",
		},
		{
			name: "negative duration falls back to default",
			rec:  FfmpegRecorder{SilenceDuration: -1 * time.Second, SilenceNoiseDB: -40},
			want: "silencedetect=noise=-40dB:d=30",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rec.silencedetectFilter(); got != tt.want {
				t.Errorf("silencedetectFilter() = %q, want %q", got, tt.want)
			}
		})
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
