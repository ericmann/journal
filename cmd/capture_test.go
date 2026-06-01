package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestCaptureWritesDailyBlock(t *testing.T) {
	root := t.TempDir()
	path, err := capture(root, ts(t, "2026-06-01 09:14"), "fallback broke on #cabot", []string{"litellm"}, "", "decision")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	// flag tag litellm + inline #cabot, flag marker decision.
	if !strings.Contains(got, "## 09:14 #litellm #cabot @decision\n") {
		t.Errorf("header wrong:\n%s", got)
	}
	if !strings.Contains(got, "fallback broke on #cabot\n") {
		t.Errorf("body missing:\n%s", got)
	}
	if !strings.HasSuffix(path, filepath.Join("daily", "2026", "06", "2026-06-01.md")) {
		t.Errorf("path = %s", path)
	}
}

func TestCaptureMergesAndDedupesTags(t *testing.T) {
	root := t.TempDir()
	// flag "#Cabot,litellm" plus inline #cabot should dedupe to cabot,litellm.
	path, err := capture(root, ts(t, "2026-06-01 09:14"), "note #cabot", []string{"#Cabot,litellm"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "## 09:14 #cabot #litellm\n") {
		t.Errorf("tags not merged/deduped:\n%s", data)
	}
}

func TestCaptureProjectWritesUnderProjects(t *testing.T) {
	root := t.TempDir()
	path, err := capture(root, ts(t, "2026-06-01 09:14"), "declared income", nil, "Canton COI", "decision")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, filepath.Join("projects", "canton-coi", "notes")) {
		t.Errorf("project path not slugified: %s", path)
	}
}

func TestCaptureRejectsEmptyText(t *testing.T) {
	if _, err := capture(t.TempDir(), time.Now(), "   ", nil, "", ""); err == nil {
		t.Error("expected error on empty text")
	}
}

func TestCaptureRejectsBadMarker(t *testing.T) {
	if _, err := capture(t.TempDir(), time.Now(), "x", nil, "", "idea"); err == nil {
		t.Error("expected error on invalid marker")
	}
}

func TestCaptureIsFast(t *testing.T) {
	root := t.TempDir()
	start := time.Now()
	if _, err := capture(root, time.Now(), "quick note", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("capture took %v, want < 2s", d)
	}
}

func TestInitRepoCreatesSkeletonAndConfig(t *testing.T) {
	root := t.TempDir()
	created, err := initRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("expected created=true on fresh repo")
	}
	for _, p := range []string{
		filepath.Join(".journal", "config.yaml"),
		filepath.Join(".journal", "index"),
		"daily", "projects", "reflections", ".gitignore",
	} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if !strings.Contains(string(gi), ".journal/index/") {
		t.Errorf("gitignore missing index entry:\n%s", gi)
	}
}

func TestInitRepoDoesNotClobberConfig(t *testing.T) {
	root := t.TempDir()
	if _, err := initRepo(root); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, ".journal", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("embed_dim: 999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := initRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("expected created=false when config already exists")
	}
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "999") {
		t.Errorf("existing config was clobbered:\n%s", data)
	}
}

func TestInitGitignoreNotDuplicated(t *testing.T) {
	root := t.TempDir()
	if _, err := initRepo(root); err != nil {
		t.Fatal(err)
	}
	if _, err := initRepo(root); err != nil {
		t.Fatal(err)
	}
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if n := strings.Count(string(gi), ".journal/index/"); n != 1 {
		t.Errorf(".journal/index/ appears %d times, want 1:\n%s", n, gi)
	}
}
