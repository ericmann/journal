package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/synth"
)

const transcribeJSON = `{"segments":[
  {"start":3.0,"end":6.0,"text":"We decided to ship the local-only feature.","speaker":"SPEAKER_00"},
  {"start":6.5,"end":9.0,"text":"Agreed, ship it Friday.","speaker":"SPEAKER_01"}
]}`

func writeTempJSON(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "2026-06-02-acme-q2-planning.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunTranscribeWritesSummaryAndIndexes(t *testing.T) {
	cfg := testRepo(t, nil)
	jsonPath := writeTempJSON(t, transcribeJSON)
	date := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	fakeSynth := &synth.Fake{Reply: "OVERVIEW: shipped local-only.\n- Decision: ship Friday."}

	var out bytes.Buffer
	err := runTranscribe(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), fakeSynth,
		transcribeOptions{jsonPath: jsonPath, title: "Acme Q2 Planning", date: date}, &out)
	if err != nil {
		t.Fatal(err)
	}

	// File landed in the transcripts zone with the dated, slugged name.
	abs := filepath.Join(cfg.TranscriptsAbsPath(), "2026-06-02-acme-q2-planning.md")
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("transcript not written: %v", err)
	}
	md := string(data)
	for _, want := range []string{"source: whisperx", "## Notes\n\nOVERVIEW: shipped local-only.", "## Transcript", "SPEAKER_00", "SPEAKER_01"} {
		if !strings.Contains(md, want) {
			t.Errorf("transcript missing %q", want)
		}
	}
	// The summary prompt actually received the transcript text.
	if !strings.Contains(fakeSynth.LastReq.Prompt, "ship the local-only feature") {
		t.Errorf("summary prompt didn't include transcript: %q", fakeSynth.LastReq.Prompt)
	}
	// File is dated to the meeting, and indexing reported chunks + summary.
	if info, _ := os.Stat(abs); info.ModTime().UTC().Format("2006-01-02") != "2026-06-02" {
		t.Errorf("transcript mtime not set to meeting date")
	}
	if !strings.Contains(out.String(), "chunks embedded") || !strings.Contains(out.String(), "summary:") {
		t.Errorf("unexpected report: %s", out.String())
	}
	// It's actually retrievable now.
	results, err := runSearch(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), "ship local-only feature Friday", 3, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("indexed transcript not found by search")
	}
}

func TestRunTranscribeNoSummaryStillIngests(t *testing.T) {
	cfg := testRepo(t, nil)
	jsonPath := writeTempJSON(t, transcribeJSON)

	var out bytes.Buffer
	// client == nil → no summary, but the transcript must still land + index.
	err := runTranscribe(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), nil,
		transcribeOptions{jsonPath: jsonPath, title: "No Summary", date: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)}, &out)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(cfg.TranscriptsAbsPath(), "2026-06-02-no-summary.md")
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("transcript not written: %v", err)
	}
	if strings.Contains(string(data), "## Notes") {
		t.Errorf("expected no ## Notes without a summary client")
	}
	if !strings.Contains(out.String(), "summary: none") {
		t.Errorf("expected 'summary: none' in report: %s", out.String())
	}
}
