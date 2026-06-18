package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/embed"
)

func TestMCPSearchReturnsResultsJSON(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:14 #cabot\nlitellm fallback routing is broken\n",
	})
	out, err := mcpSearch(context.Background(), cfg, fake, searchInput{Query: "litellm fallback", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(env.Results) == 0 {
		t.Fatal("no results")
	}
	for _, k := range []string{"path", "line_start", "line_end", "score"} {
		if _, ok := env.Results[0][k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	// Search results must carry the full chunk body, not just the snippet —
	// MCP clients read content through this field.
	if body, _ := env.Results[0]["body"].(string); body != "litellm fallback routing is broken" {
		t.Errorf("body = %q, want full chunk body", body)
	}
}

func TestMCPShowReturnsFullContent(t *testing.T) {
	content := "# 2026-06-01\n\n## 09:14\n" + strings.Repeat("a long line of note text. ", 30) + "\n"
	cfg := testRepo(t, map[string]string{"projects/canton/notes/2026-06-01.md": content})

	for _, ref := range []string{
		"projects/canton/notes/2026-06-01.md",
		"projects/canton/notes/2026-06-01.md:3-12", // citation form
	} {
		out, err := mcpShow(cfg, showInput{Path: ref})
		if err != nil {
			t.Fatalf("show(%q): %v", ref, err)
		}
		var got struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if got.Content != content {
			t.Errorf("show(%q) content truncated or altered: %d bytes, want %d", ref, len(got.Content), len(content))
		}
	}
}

func TestMCPShowMissingAndEscapingPaths(t *testing.T) {
	cfg := testRepo(t, nil)
	if _, err := mcpShow(cfg, showInput{Path: "projects/nope/notes/2026-01-01.md"}); err == nil {
		t.Error("expected error for missing note")
	}
	if _, err := mcpShow(cfg, showInput{Path: "../outside.md"}); err == nil {
		t.Error("expected error for path outside the repo")
	}
}

func TestMCPSearchEmptyQueryErrors(t *testing.T) {
	cfg := testRepo(t, nil)
	if _, err := mcpSearch(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), searchInput{Query: "  "}); err == nil {
		t.Error("expected error on empty query")
	}
}

func TestMCPDecisionsFilters(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00 @decision\nchose pure-go driver\n\n## 10:00 @question\nis it taxable?\n",
	})
	out, err := mcpDecisions(context.Background(), cfg, decisionsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "chose pure-go driver") {
		t.Errorf("decision missing: %s", out)
	}
	if strings.Contains(out, "is it taxable") {
		t.Errorf("non-decision leaked: %s", out)
	}
}

func TestMCPThreadsJSON(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"projects/canton/_index.md": "# 2026-06-01\n\n## 09:00 @question\nopen q\n",
	})
	out, err := mcpThreads(context.Background(), cfg, threadsInput{})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid threads JSON: %v\n%s", err, out)
	}
	if len(env.Threads) != 1 || env.Threads[0]["project"] != "canton" {
		t.Errorf("threads = %v", env.Threads)
	}
}

func TestMCPCaptureWritesAndReturnsPath(t *testing.T) {
	cfg := testRepo(t, nil)
	out, err := mcpCapture(cfg, captureInput{Text: "a new note #x", Marker: "todo"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got["captured"], "daily/") {
		t.Errorf("captured path = %q", got["captured"])
	}
}

func TestMCPCaptureBadMarkerErrors(t *testing.T) {
	cfg := testRepo(t, nil)
	if _, err := mcpCapture(cfg, captureInput{Text: "x", Marker: "bogus"}); err == nil {
		t.Error("expected error on invalid marker")
	}
}

func TestErrorJSONShape(t *testing.T) {
	s := errorJSON(context.DeadlineExceeded)
	var env map[string]string
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		t.Fatal(err)
	}
	if env["error"] == "" {
		t.Errorf("errorJSON missing error field: %s", s)
	}
}

func TestMCPStatsReturnsJSON(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00 #cabot @todo\nfix the router\n\n## 10:00 @decision\nchose pure go\n",
	})
	out, err := mcpStats(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var rep StatsReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if rep.NoteChunks == 0 {
		t.Error("expected note_chunks > 0")
	}
	if rep.OpenTodos != 1 {
		t.Errorf("open_todos = %d, want 1", rep.OpenTodos)
	}
	if rep.Decisions != 1 {
		t.Errorf("decisions = %d, want 1", rep.Decisions)
	}
}

func TestMCPTodayReturnsJSON(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00 @todo\nfinish the dashboard\n",
	})
	out, err := mcpToday(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var rep todayReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if rep.Date != today {
		t.Errorf("date = %q, want %q", rep.Date, today)
	}
	if !rep.Notes {
		t.Error("notes = false, want true (today's file exists)")
	}
	if len(rep.Todos) != 1 {
		t.Errorf("todos = %d, want 1", len(rep.Todos))
	}
}
