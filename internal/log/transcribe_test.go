package log

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFakeTranscriberReturnsCannedText(t *testing.T) {
	ft := &FakeTranscriber{Reply: "reviewed the deploy logs", DurationSec: 12}
	text, dur, err := ft.Transcribe(context.Background(), "audio.wav")
	if err != nil {
		t.Fatal(err)
	}
	if text != "reviewed the deploy logs" {
		t.Errorf("text = %q, want canned reply", text)
	}
	if dur != 12 {
		t.Errorf("dur = %d, want 12", dur)
	}
	if ft.CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", ft.CallCount)
	}
	if ft.LastPath != "audio.wav" {
		t.Errorf("LastPath = %q, want \"audio.wav\"", ft.LastPath)
	}
}

func TestFakeTranscriberReturnsError(t *testing.T) {
	ft := &FakeTranscriber{ForcedErr: errors.New("asr failed")}
	_, _, err := ft.Transcribe(context.Background(), "audio.wav")
	if err == nil || !strings.Contains(err.Error(), "asr failed") {
		t.Errorf("expected ForcedErr, got: %v", err)
	}
}

func TestFakeTranscriberName(t *testing.T) {
	ft := &FakeTranscriber{}
	if got := ft.Name(); got != "fake/base.en" {
		t.Errorf("Name() = %q, want \"fake/base.en\"", got)
	}
}

func TestWhisperCPPMissingModelFailsFast(t *testing.T) {
	w := NewWhisperCPP(t.TempDir(), "base.en")
	_, _, err := w.Transcribe(context.Background(), "audio.wav")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if !strings.Contains(err.Error(), "journal models pull") {
		t.Errorf("error should hint at `journal models pull`, got: %v", err)
	}
}

// writeFakeWhisperBin creates an executable file named whisper-cli inside a
// fresh temp dir and points PATH at it, so findWhisperBin resolves without
// depending on a real whisper.cpp install.
func writeFakeWhisperBin(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "whisper-cli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestCheckAvailable(t *testing.T) {
	writeModel := func(t *testing.T, modelDir, model string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(modelDir, model+".bin"), []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name      string
		setup     func(t *testing.T, modelDir, model string)
		wantErr   bool
		wantMatch string
	}{
		{
			name: "present",
			setup: func(t *testing.T, modelDir, model string) {
				writeModel(t, modelDir, model)
				writeFakeWhisperBin(t)
			},
			wantErr: false,
		},
		{
			name: "missing binary",
			setup: func(t *testing.T, modelDir, model string) {
				writeModel(t, modelDir, model)
				t.Setenv("PATH", t.TempDir())
			},
			wantErr:   true,
			wantMatch: "whisper.cpp binary not found",
		},
		{
			name: "missing model",
			setup: func(t *testing.T, modelDir, model string) {
				writeFakeWhisperBin(t)
			},
			wantErr:   true,
			wantMatch: "journal models pull",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modelDir := t.TempDir()
			tt.setup(t, modelDir, "base.en")

			err := CheckAvailable(modelDir, "base.en")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantMatch) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestWhisperCPPName(t *testing.T) {
	w := NewWhisperCPP("/tmp", "base.en")
	if got := w.Name(); got != "whisper.cpp/base.en" {
		t.Errorf("Name() = %q, want \"whisper.cpp/base.en\"", got)
	}
	w2 := NewWhisperCPP("/tmp", "")
	if got := w2.Name(); got != "whisper.cpp" {
		t.Errorf("Name() (empty model) = %q, want \"whisper.cpp\"", got)
	}
}

func TestWavDurationSec(t *testing.T) {
	// Build a minimal valid WAV header with known parameters.
	// 16 kHz mono 16-bit PCM, 16000 samples → 1 second of audio.
	const (
		sampleRate  = 16000
		numChannels = 1
		bitDepth    = 16
		byteRate    = sampleRate * numChannels * (bitDepth / 8) // 32000
		numSamples  = sampleRate                                // 1 second
		dataSize    = numSamples * numChannels * (bitDepth / 8) // 32000
	)

	tmp := filepath.Join(t.TempDir(), "test.wav")
	f, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+dataSize))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(hdr[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], numChannels)
	binary.LittleEndian.PutUint32(hdr[24:28], sampleRate)
	binary.LittleEndian.PutUint32(hdr[28:32], byteRate)
	binary.LittleEndian.PutUint16(hdr[32:34], numChannels*(bitDepth/8)) // block align
	binary.LittleEndian.PutUint16(hdr[34:36], bitDepth)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], dataSize)
	if _, err := f.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	// Write minimal PCM data so the file is not just a header.
	zeros := make([]byte, dataSize)
	if _, err := f.Write(zeros); err != nil {
		t.Fatal(err)
	}
	f.Close()

	dur, err := wavDurationSec(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if dur != 1 {
		t.Errorf("wavDurationSec = %d, want 1", dur)
	}
}

func TestWavDurationSecNonWavReturnsZero(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "notawav.bin")
	if err := os.WriteFile(tmp, []byte("not a wav file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	dur, _ := wavDurationSec(tmp)
	if dur != 0 {
		t.Errorf("non-WAV file should return 0, got %d", dur)
	}
}
