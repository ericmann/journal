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
	} {
		if !strings.Contains(got, want) {
			t.Errorf("GenerateMD missing %q in:\n%s", want, got)
		}
	}
}
