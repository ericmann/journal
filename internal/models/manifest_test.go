package models

import (
	"strings"
	"testing"
)

func TestGenerateMDEmpty(t *testing.T) {
	got := GenerateMD(nil)
	if !strings.Contains(got, "No models installed") {
		t.Errorf("empty GenerateMD missing placeholder: %q", got)
	}
}

func TestGenerateMDTable(t *testing.T) {
	manifests := []Manifest{
		{ModelID: "base.en", Revision: "main", Checksum: "abc123"},
		{ModelID: "Systran/faster-whisper-small.en", Revision: "v1", Checksum: "def456"},
	}
	got := GenerateMD(manifests)
	for _, want := range []string{
		"# Models",
		"journal models pull",
		"| Model ID |",
		"base.en",
		"Systran/faster-whisper-small.en",
		"abc123",
		"def456",
		"| no |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("GenerateMD missing %q in:\n%s", want, got)
		}
	}
}

func TestGenerateMDGatedModelRecordsAcceptURL(t *testing.T) {
	manifests := []Manifest{
		{
			ModelID:   "pyannote/speaker-diarization-3.1",
			Revision:  "main",
			Checksum:  "abc123",
			Gated:     true,
			AcceptURL: "https://huggingface.co/pyannote/speaker-diarization-3.1",
		},
	}
	got := GenerateMD(manifests)
	for _, want := range []string{
		"pyannote/speaker-diarization-3.1",
		"yes",
		"accept terms",
		"https://huggingface.co/pyannote/speaker-diarization-3.1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("GenerateMD missing %q in:\n%s", want, got)
		}
	}
}
