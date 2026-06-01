package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/synth"
)

func TestRunSynthDryRunNoKeyNoWrite(t *testing.T) {
	// Ensure no key is needed for dry-run.
	t.Setenv("ANTHROPIC_API_KEY", "")
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:14 @decision\nchose the pure-go driver\n",
	})
	var buf bytes.Buffer
	err := runSynth(context.Background(), cfg, synth.Options{Kind: synth.KindWeekly, DryRun: true}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "intended output:") {
		t.Errorf("dry-run output missing header:\n%s", out)
	}
	if !strings.Contains(out, "--write") {
		t.Errorf("dry-run should hint at --write:\n%s", out)
	}
	if !strings.Contains(out, "weekly reflection") {
		t.Errorf("dry-run should print the assembled prompt:\n%s", out)
	}
	// No reflections file should have been written.
	if entries, _ := os.ReadDir(filepath.Join(cfg.Root(), "reflections")); len(entries) != 0 {
		t.Errorf("dry-run wrote files: %v", entries)
	}
}

func TestRunSynthWriteRequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:14\nnote\n",
	})
	err := runSynth(context.Background(), cfg, synth.Options{Kind: synth.KindWeekly, Write: true}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is unset and --write is used")
	}
	if strings.Contains(err.Error(), "panic") {
		t.Errorf("unexpected error shape: %v", err)
	}
}
