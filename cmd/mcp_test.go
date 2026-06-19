package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/synth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestMCPAskReturnsAnswerWithCitations(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:14 #cabot\nlitellm fallback routing is broken\n",
	})
	fakeClient := &synth.Fake{Reply: "litellm fallback is broken [daily/2026/06/d.md:3-4]"}
	out, err := mcpAsk(context.Background(), cfg, fake, fakeClient, askInput{Query: "litellm fallback", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	var got askResponse
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Answer == "" {
		t.Error("answer must not be empty")
	}
	if len(got.Citations) == 0 {
		t.Error("citations must not be empty")
	}
	for _, c := range got.Citations {
		if !strings.Contains(c, ":") {
			t.Errorf("citation %q missing colon (want path:line-line form)", c)
		}
	}
	if fakeClient.CallCount != 1 {
		t.Errorf("synth client called %d times, want 1", fakeClient.CallCount)
	}
}

func TestMCPAskEmptyQueryErrors(t *testing.T) {
	cfg := testRepo(t, nil)
	if _, err := mcpAsk(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), &synth.Fake{}, askInput{Query: "  "}); err == nil {
		t.Error("expected error on empty query")
	}
}

func TestMCPAskSynthErrorPropagates(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:14\nlitellm fallback routing\n",
	})
	boom := errors.New("synth unavailable")
	fakeClient := &synth.Fake{ForcedErr: boom}
	_, err := mcpAsk(context.Background(), cfg, fake, fakeClient, askInput{Query: "litellm fallback"})
	if err == nil {
		t.Fatal("expected error from failing synth client")
	}
	if !strings.Contains(err.Error(), "synthesis") {
		t.Errorf("error should mention synthesis, got: %v", err)
	}
}

func TestMCPAskNoResultsReturnsEmptyCitations(t *testing.T) {
	// Empty repo: no notes indexed, so no chunks found.
	cfg := testRepo(t, nil)
	fakeClient := &synth.Fake{}
	out, err := mcpAsk(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), fakeClient, askInput{Query: "anything"})
	if err != nil {
		t.Fatal(err)
	}
	var got askResponse
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(got.Citations) != 0 {
		t.Errorf("citations = %v, want empty", got.Citations)
	}
	// Synth must not be called when there are no chunks to ground the answer.
	if fakeClient.CallCount != 0 {
		t.Errorf("synth client called %d times, want 0 (no chunks)", fakeClient.CallCount)
	}
}

// --- Resource handler unit tests ---

func TestReadTodayResourceWithNote(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	content := "# " + today + "\n\n## 09:00\nhello world\n"
	cfg := testRepo(t, map[string]string{rel: content})

	res, err := readTodayResource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("want 1 content block, got %d", len(res.Contents))
	}
	if res.Contents[0].URI != "journal://today" {
		t.Errorf("URI = %q, want journal://today", res.Contents[0].URI)
	}
	if res.Contents[0].MIMEType != "text/markdown" {
		t.Errorf("MIMEType = %q, want text/markdown", res.Contents[0].MIMEType)
	}
	if res.Contents[0].Text != content {
		t.Errorf("content mismatch: got %q, want %q", res.Contents[0].Text, content)
	}
}

func TestReadTodayResourceNoNotePlaceholder(t *testing.T) {
	cfg := testRepo(t, nil) // no daily file

	res, err := readTodayResource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 || res.Contents[0].Text == "" {
		t.Fatal("expected non-empty placeholder content")
	}
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(res.Contents[0].Text, today) {
		t.Errorf("placeholder should contain today's date %q, got: %q", today, res.Contents[0].Text)
	}
}

func TestReadRecentResourceEmpty(t *testing.T) {
	cfg := testRepo(t, nil)

	res, err := readRecentResource(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("want 1 content block, got %d", len(res.Contents))
	}
	if res.Contents[0].URI != "journal://recent" {
		t.Errorf("URI = %q, want journal://recent", res.Contents[0].URI)
	}
	if !strings.Contains(res.Contents[0].Text, "# Recent Notes") {
		t.Errorf("expected header, got: %q", res.Contents[0].Text)
	}
}

func TestReadRecentResourceWithNotes(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": "# 2026-06-01\n\n## 09:00\nsome work done\n",
	})

	res, err := readRecentResource(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("want 1 content block, got %d", len(res.Contents))
	}
	if !strings.Contains(res.Contents[0].Text, "some work done") {
		t.Errorf("recent listing missing note body: %q", res.Contents[0].Text)
	}
}

func TestReadProjectIndexResourceFound(t *testing.T) {
	content := "# canton project\n\nsome notes here\n"
	cfg := testRepo(t, map[string]string{
		"projects/canton/_index.md": content,
	})

	res, err := readProjectIndexResource(cfg, "journal://projects/canton/index")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("want 1 content block, got %d", len(res.Contents))
	}
	if res.Contents[0].URI != "journal://projects/canton/index" {
		t.Errorf("URI = %q, want journal://projects/canton/index", res.Contents[0].URI)
	}
	if res.Contents[0].Text != content {
		t.Errorf("content mismatch: got %q, want %q", res.Contents[0].Text, content)
	}
}

func TestReadProjectIndexResourceMissing(t *testing.T) {
	cfg := testRepo(t, nil)

	_, err := readProjectIndexResource(cfg, "journal://projects/nope/index")
	if err == nil {
		t.Error("expected ResourceNotFound error for missing project index")
	}
}

func TestExtractProjectSlug(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"journal://projects/canton/index", "canton"},
		{"journal://projects/my-project/index", "my-project"},
		{"journal://projects//index", ""},
		{"journal://projects/../index", ""},
		{"journal://today", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := extractProjectSlug(tc.uri)
		if got != tc.want {
			t.Errorf("extractProjectSlug(%q) = %q, want %q", tc.uri, got, tc.want)
		}
	}
}

// TestMCPResourcesRoundTrip exercises resources/list and resources/read over
// the MCP in-memory transport — same protocol layer as stdio but without I/O.
func TestMCPResourcesRoundTrip(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	noteContent := "# " + today + "\n\n## 09:00\nround trip test\n"
	idxContent := "# canton\n\nproject index\n"
	cfg := testRepo(t, map[string]string{
		rel:                         noteContent,
		"projects/canton/_index.md": idxContent,
	})

	s := mcp.NewServer(&mcp.Implementation{Name: "journal-test", Version: "0"}, nil)
	addResources(s, cfg)

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := s.Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := c.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	// resources/list must include today and recent.
	wantURIs := map[string]bool{"journal://today": true, "journal://recent": true}
	for r, err := range cs.Resources(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		delete(wantURIs, r.URI)
	}
	for uri := range wantURIs {
		t.Errorf("resources/list missing %q", uri)
	}

	// resources/templates/list must include the project index template.
	wantTemplates := map[string]bool{"journal://projects/{slug}/index": true}
	for r, err := range cs.ResourceTemplates(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		delete(wantTemplates, r.URITemplate)
	}
	for tpl := range wantTemplates {
		t.Errorf("resources/templates/list missing %q", tpl)
	}

	// resources/read: today's note.
	todayRes, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "journal://today"})
	if err != nil {
		t.Fatalf("ReadResource(today): %v", err)
	}
	if len(todayRes.Contents) == 0 || !strings.Contains(todayRes.Contents[0].Text, today) {
		t.Errorf("today resource content = %q, want today date %q", todayRes.Contents[0].Text, today)
	}

	// resources/read: project index via template.
	idxRes, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "journal://projects/canton/index"})
	if err != nil {
		t.Fatalf("ReadResource(canton index): %v", err)
	}
	if len(idxRes.Contents) == 0 || !strings.Contains(idxRes.Contents[0].Text, "project index") {
		t.Errorf("canton index content = %q, want 'project index'", idxRes.Contents[0].Text)
	}
}
