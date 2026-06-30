package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFilename(t *testing.T) {
	ts := time.Date(2026, 6, 30, 14, 35, 0, 0, time.UTC)
	tests := []struct {
		title    string
		fallback string
		want     string
	}{
		{"Deploy Review", "", "2026-06-30-1435-deploy-review.md"},
		{"", "um so I checked deploy logs today", "2026-06-30-1435-um-so-i-checked-deploy-logs-today.md"},
		{"", "one two three four five six seven eight nine ten", "2026-06-30-1435-one-two-three-four-five-six-seven-eight.md"},
		{"", "", "2026-06-30-1435-voice.md"},
	}
	for _, tc := range tests {
		got := Filename(ts, tc.title, tc.fallback)
		if got != tc.want {
			t.Errorf("Filename(%q, %q) = %q, want %q", tc.title, tc.fallback, got, tc.want)
		}
	}
}

func TestLand(t *testing.T) {
	dir := t.TempDir()
	content := []byte("# Voice Note\n\nhello world\n")

	abs, err := Land(dir, "2026-06-30-1435-test.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(abs) {
		t.Errorf("returned path %q is not absolute", abs)
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("file content mismatch: got %q, want %q", got, content)
	}
}

func TestLandCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs", "voice")
	_, err := Land(dir, "note.md", []byte("content"))
	if err != nil {
		t.Fatalf("Land should create directory: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("directory was not created")
	}
}

func TestAppendBacklink(t *testing.T) {
	dir := t.TempDir()
	daily := filepath.Join(dir, "daily.md")
	if err := os.WriteFile(daily, []byte("# 2026-06-30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 6, 30, 14, 35, 0, 0, time.UTC)
	if err := AppendBacklink(daily, "logs/2026-06-30-1435-test.md", ts); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(daily)
	if !strings.Contains(string(data), "Voice note") {
		t.Error("backlink not appended")
	}
	if !strings.Contains(string(data), "14:35") {
		t.Error("backlink missing timestamp")
	}
}

func TestAppendBacklinkNoopOnMissingFile(t *testing.T) {
	err := AppendBacklink("/nonexistent/daily.md", "logs/note.md", time.Now())
	if err != nil {
		t.Errorf("missing daily file should be no-op, got: %v", err)
	}
}

func TestAppendBacklinkNoopOnEmpty(t *testing.T) {
	err := AppendBacklink("", "logs/note.md", time.Now())
	if err != nil {
		t.Errorf("empty path should be no-op, got: %v", err)
	}
}
