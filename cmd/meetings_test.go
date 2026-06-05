package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	"github.com/ericmann/journal/internal/store"
)

// indexTranscript writes a transcript file under transcripts/ and indexes it as
// a transcript source, returning the loaded config.
func indexedTranscriptRepo(t *testing.T, rel, content string) *config.Config {
	t.Helper()
	cfg := testRepo(t, nil)
	fake := embed.NewFake(cfg.EmbedDim)
	abs := filepath.Join(cfg.Root(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := index.NewIndexer(s, fake).IndexTranscript(context.Background(), rel, content, time.Now(), "meeting"); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMeetingsListsTranscripts(t *testing.T) {
	cfg := indexedTranscriptRepo(t, "transcripts/2026-06-05-sync.md",
		"---\ntitle: \"Weekly Sync\"\n---\n\n# Weekly Sync\n\nWe discussed the roadmap and shipped the cron.\n")
	ms, err := recentMeetings(context.Background(), cfg, time.Time{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("got %d meetings, want 1", len(ms))
	}
	if ms[0].Title != "Weekly Sync" {
		t.Errorf("title = %q, want from first H1", ms[0].Title)
	}
	if ms[0].Filename != "2026-06-05-sync.md" {
		t.Errorf("filename = %q", ms[0].Filename)
	}
	if !strings.Contains(ms[0].Snippet, "roadmap") {
		t.Errorf("snippet = %q", ms[0].Snippet)
	}
	// JSON shape.
	var buf bytes.Buffer
	if err := renderMeetings(&buf, ms, true); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Meetings []map[string]any `json:"meetings"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid meetings JSON: %v\n%s", err, buf.String())
	}
	for _, k := range []string{"filename", "timestamp", "title", "snippet"} {
		if _, ok := env.Meetings[0][k]; !ok {
			t.Errorf("meetings JSON missing %q", k)
		}
	}
}

func TestSearchSourceFilterParsing(t *testing.T) {
	cases := map[string]string{
		"": "", "all": "", "notes": store.SourceNote, "transcript": store.SourceTranscript,
	}
	for in, want := range cases {
		got, err := parseSourceFilter(in)
		if err != nil || got != want {
			t.Errorf("parseSourceFilter(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseSourceFilter("bogus"); err == nil {
		t.Error("expected error for invalid source")
	}
}

func TestMCPMeetingsJSON(t *testing.T) {
	cfg := indexedTranscriptRepo(t, "transcripts/m.md", "# Standup\n\nAlice gave an update.\n")
	out, err := mcpMeetings(context.Background(), cfg, meetingsInput{K: 10})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Meetings []map[string]any `json:"meetings"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(env.Meetings) != 1 || env.Meetings[0]["title"] != "Standup" {
		t.Errorf("meetings = %v", env.Meetings)
	}
}
