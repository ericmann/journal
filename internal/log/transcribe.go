package log

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Transcriber converts an audio file to plain text.
// It is an injectable boundary so tests never exec a real binary.
type Transcriber interface {
	// Transcribe runs ASR on audioPath and returns the raw text and the audio
	// duration in whole seconds (0 when unknown). On error the audio file is
	// untouched; callers should notify the user and let them retry.
	Transcribe(ctx context.Context, audioPath string) (text string, durationSec int, err error)
	// Name returns the backend/model string written to the transcriber frontmatter
	// field (e.g. "whisper.cpp/base.en").
	Name() string
}

// FakeTranscriber is a deterministic Transcriber for tests.
type FakeTranscriber struct {
	Reply       string
	DurationSec int
	ForcedErr   error
	CallCount   int
	LastPath    string
}

// Transcribe records the call and returns canned data (or ForcedErr).
func (f *FakeTranscriber) Transcribe(_ context.Context, audioPath string) (string, int, error) {
	f.CallCount++
	f.LastPath = audioPath
	if f.ForcedErr != nil {
		return "", 0, f.ForcedErr
	}
	return f.Reply, f.DurationSec, nil
}

// Name returns the fake backend identifier.
func (f *FakeTranscriber) Name() string { return "fake/base.en" }

// WhisperCPP is a Transcriber backed by the whisper.cpp CLI binary.
// The model file must be provisioned first via `journal models pull`.
type WhisperCPP struct {
	modelDir string
	model    string
}

// NewWhisperCPP returns a WhisperCPP Transcriber.
// modelDir is the directory containing model files; model is the model name
// without the .bin extension (e.g. "base.en").
func NewWhisperCPP(modelDir, model string) *WhisperCPP {
	return &WhisperCPP{modelDir: modelDir, model: model}
}

// Name returns "whisper.cpp/<model>".
func (w *WhisperCPP) Name() string {
	if w.model == "" {
		return "whisper.cpp"
	}
	return "whisper.cpp/" + w.model
}

// Transcribe runs whisper.cpp on audioPath.
// Returns an error (with "run `journal models pull`" hint) when the model file
// is missing — it never downloads anything itself.
func (w *WhisperCPP) Transcribe(ctx context.Context, audioPath string) (string, int, error) {
	modelPath := filepath.Join(w.modelDir, w.model+".bin")
	if _, err := os.Stat(modelPath); errors.Is(err, os.ErrNotExist) {
		return "", 0, fmt.Errorf("model file not found at %q: run `journal models pull`", modelPath)
	}

	dur, _ := wavDurationSec(audioPath)

	bin, err := findWhisperBin()
	if err != nil {
		return "", dur, err
	}

	// -nt: no timestamps; -l en: language hint; stderr gets progress/debug.
	out, err := exec.CommandContext(ctx, bin,
		"-m", modelPath,
		"-f", audioPath,
		"-l", "en",
		"-nt",
	).Output()
	if err != nil {
		return "", dur, fmt.Errorf("whisper.cpp transcription failed: %w", err)
	}
	return strings.TrimSpace(string(out)), dur, nil
}

// CheckAvailable verifies the whisper.cpp binary resolves on PATH and the
// configured model file exists, without running transcription. It performs
// the same checks Transcribe does lazily, so callers can preflight the
// toolchain (e.g. before starting a recording) and fail fast with the same
// actionable message rather than discovering the problem only after
// transcription is attempted.
func CheckAvailable(modelDir, model string) error {
	modelPath := filepath.Join(modelDir, model+".bin")
	if _, err := os.Stat(modelPath); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("model file not found at %q: run `journal models pull`", modelPath)
	}
	if _, err := findWhisperBin(); err != nil {
		return err
	}
	return nil
}

// findWhisperBin looks for the whisper.cpp binary in PATH, trying common names.
func findWhisperBin() (string, error) {
	for _, name := range []string{"whisper-cli", "whisper-cpp", "whisper"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("whisper.cpp binary not found in PATH " +
		"(tried whisper-cli, whisper-cpp, whisper); " +
		"install whisper.cpp and ensure the binary is in your PATH")
}

// wavDurationSec reads the WAV RIFF header and returns the audio length in
// whole seconds. Returns 0 (no error) when the duration cannot be determined
// — duration_sec is informational and a zero value is safe to land.
func wavDurationSec(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// Minimum WAV header: RIFF(4) + size(4) + WAVE(4) + "fmt "(4) + fmtSize(4)
	// + audioFmt(2) + channels(2) + sampleRate(4) + byteRate(4) + blockAlign(2)
	// + bitsPerSample(2) + "data"(4) + dataSize(4) = 44 bytes.
	var buf [44]byte
	if _, err := io.ReadFull(f, buf[:]); err != nil {
		return 0, nil // too short — not a valid WAV, treat as unknown duration
	}
	if string(buf[0:4]) != "RIFF" || string(buf[8:12]) != "WAVE" {
		return 0, nil
	}
	byteRate := binary.LittleEndian.Uint32(buf[28:32])
	dataSize := binary.LittleEndian.Uint32(buf[40:44])
	if byteRate == 0 {
		return 0, nil
	}
	return int(dataSize / byteRate), nil
}
