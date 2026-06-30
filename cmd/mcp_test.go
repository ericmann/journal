package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestMCPTodayIncludesDecisions(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00 @decision\nchose sqlite-vec\n",
	})
	out, err := mcpToday(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var rep todayReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rep.Decisions) != 1 {
		t.Errorf("decisions = %d, want 1", len(rep.Decisions))
	}
	if !strings.Contains(rep.Decisions[0].Snippet, "chose sqlite-vec") {
		t.Errorf("decision snippet = %q, want 'chose sqlite-vec'", rep.Decisions[0].Snippet)
	}
}

func TestMCPDoneWithResolution(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{"daily/2026/06/2026-06-01.md": todoFixture})
	ctx := context.Background()

	dout, err := mcpDone(ctx, cfg, embed.NewFake(cfg.EmbedDim), doneInput{
		Ref:        "janus SOW",
		Resolution: "sent draft to client",
	})
	if err != nil {
		t.Fatal(err)
	}
	var denv struct {
		Completed map[string]any `json:"completed"`
	}
	if err := json.Unmarshal([]byte(dout), &denv); err != nil {
		t.Fatalf("invalid done JSON: %v\n%s", err, dout)
	}
	if denv.Completed["path"] == "" {
		t.Errorf("done result missing path: %s", dout)
	}

	// Resolution line must appear in the file.
	abs := filepath.Join(cfg.Root(), "daily", "2026", "06", "2026-06-01.md")
	data, _ := os.ReadFile(abs)
	if !strings.Contains(string(data), "Resolution: sent draft to client") {
		t.Errorf("resolution line missing from file:\n%s", string(data))
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

// --- Prompt unit tests ---

func TestPromptWeeklyReflectionAssemblesContext(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00 #cabot\ndebugged the pipeline\n",
	})
	res, err := promptWeeklyReflection(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Description == "" {
		t.Error("description must not be empty")
	}
	if len(res.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(res.Messages))
	}
	msg := res.Messages[0]
	if msg.Role != "user" {
		t.Errorf("role = %q, want user", msg.Role)
	}
	tc, ok := msg.Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", msg.Content)
	}
	if !strings.Contains(tc.Text, "weekly") && !strings.Contains(tc.Text, "Week") {
		t.Errorf("prompt should mention weekly context, got: %q", tc.Text[:min(200, len(tc.Text))])
	}
	if !strings.Contains(tc.Text, "debugged the pipeline") {
		t.Errorf("prompt should include note body, got: %q", tc.Text[:min(200, len(tc.Text))])
	}
}

func TestPromptWeeklyReflectionEmptyRepo(t *testing.T) {
	cfg := testRepo(t, nil)
	res, err := promptWeeklyReflection(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(res.Messages))
	}
	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", res.Messages[0].Content)
	}
	if strings.TrimSpace(tc.Text) == "" {
		t.Error("prompt text must not be empty even with no notes")
	}
}

func TestPromptDecisionsReviewAssemblesContext(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00 @decision\nadopted pure-go driver\n\n## 10:00 @todo\nwrite docs\n",
	})
	res, err := promptDecisionsReview(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(res.Messages))
	}
	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", res.Messages[0].Content)
	}
	if !strings.Contains(tc.Text, "adopted pure-go driver") {
		t.Errorf("prompt should include decision body, got: %q", tc.Text[:min(300, len(tc.Text))])
	}
	if strings.Contains(tc.Text, "write docs") {
		t.Errorf("non-decision (@todo) should not appear in decisions prompt")
	}
}

func TestPromptDecisionsReviewScopedToProject(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"projects/cabot/notes/2026-06-01.md": "# 2026-06-01\n\n## 09:00 @decision\ncabot chose go\n",
		"projects/other/notes/2026-06-01.md": "# 2026-06-01\n\n## 09:00 @decision\nother chose rust\n",
	})
	res, err := promptDecisionsReview(context.Background(), cfg, "cabot")
	if err != nil {
		t.Fatal(err)
	}
	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", res.Messages[0].Content)
	}
	if !strings.Contains(tc.Text, "cabot chose go") {
		t.Errorf("prompt should include cabot decision, got: %q", tc.Text[:min(300, len(tc.Text))])
	}
	if strings.Contains(tc.Text, "other chose rust") {
		t.Errorf("non-cabot decision should not appear when scoped to cabot")
	}
}

func TestPromptProjectStatusAssemblesContext(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	noteRel := "projects/canton/notes/" + today + ".md"
	noteContent := "# " + today + "\n\n## 09:00 #canton\nset up the workspace\n"
	cfg, _ := indexedRepo(t, map[string]string{
		noteRel: noteContent,
	})
	res, err := promptProjectStatus(context.Background(), cfg, "canton", "4w")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(res.Messages))
	}
	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", res.Messages[0].Content)
	}
	if !strings.Contains(tc.Text, "set up the workspace") {
		t.Errorf("prompt should include project note, got: %q", tc.Text[:min(300, len(tc.Text))])
	}
	if !strings.Contains(res.Description, "canton") {
		t.Errorf("description should mention project slug, got: %q", res.Description)
	}
}

func TestPromptProjectStatusMissingProject(t *testing.T) {
	cfg := testRepo(t, nil)
	_, err := promptProjectStatus(context.Background(), cfg, "", "2w")
	if err == nil {
		t.Error("expected error for empty project slug")
	}
}

func TestPromptProjectStatusInvalidSince(t *testing.T) {
	cfg := testRepo(t, nil)
	_, err := promptProjectStatus(context.Background(), cfg, "canton", "bogus")
	if err == nil {
		t.Error("expected error for invalid since")
	}
}

// TestMCPPromptsRoundTrip exercises prompts/list and prompts/get over the MCP
// in-memory transport — same protocol layer as stdio but without I/O.
func TestMCPPromptsRoundTrip(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel:                                   "# " + today + "\n\n## 09:00 @decision\ndecided to use go\n\n## 10:00 #canton\nset up canton project\n",
		"projects/canton/notes/2026-06-01.md": "# 2026-06-01\n\n## 09:00\ncanton work\n",
	})

	s := mcp.NewServer(&mcp.Implementation{Name: "journal-test", Version: "0"}, nil)
	addPrompts(s, cfg)

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

	// prompts/list must include all three prompts.
	wantPrompts := map[string]bool{
		"weekly-reflection": true,
		"decisions-review":  true,
		"project-status":    true,
	}
	for p, err := range cs.Prompts(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		delete(wantPrompts, p.Name)
	}
	for name := range wantPrompts {
		t.Errorf("prompts/list missing %q", name)
	}

	// prompts/get: weekly-reflection returns assembled context.
	weekRes, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "weekly-reflection"})
	if err != nil {
		t.Fatalf("GetPrompt(weekly-reflection): %v", err)
	}
	if len(weekRes.Messages) == 0 {
		t.Fatal("weekly-reflection: no messages")
	}
	if tc, ok := weekRes.Messages[0].Content.(*mcp.TextContent); !ok || tc.Text == "" {
		t.Error("weekly-reflection: message content must be non-empty text")
	}

	// prompts/get: decisions-review returns assembled context.
	decRes, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "decisions-review"})
	if err != nil {
		t.Fatalf("GetPrompt(decisions-review): %v", err)
	}
	if len(decRes.Messages) == 0 {
		t.Fatal("decisions-review: no messages")
	}

	// prompts/get: project-status with required project argument.
	projRes, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      "project-status",
		Arguments: map[string]string{"project": "canton"},
	})
	if err != nil {
		t.Fatalf("GetPrompt(project-status): %v", err)
	}
	if len(projRes.Messages) == 0 {
		t.Fatal("project-status: no messages")
	}

	// prompts/get: project-status without project argument should return an error.
	_, err = cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "project-status"})
	if err == nil {
		t.Error("project-status without project arg should return an error")
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

// --- synth tool tests ---

func TestMCPSynthWeeklyReturnsText(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00 #cabot\nshipped the indexer\n",
	})
	fakeClient := &synth.Fake{Reply: "## Highlights\n- shipped the indexer\n"}
	out, err := mcpSynth(context.Background(), cfg, fakeClient, synthMCPInput{Kind: "weekly"})
	if err != nil {
		t.Fatal(err)
	}
	var got synthMCPResponse
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Kind != "weekly" {
		t.Errorf("kind = %q, want weekly", got.Kind)
	}
	if !strings.Contains(got.Text, "Highlights") {
		t.Errorf("text = %q, want highlights content", got.Text)
	}
	if fakeClient.CallCount != 1 {
		t.Errorf("synth client called %d times, want 1", fakeClient.CallCount)
	}
}

func TestMCPSynthDefaultKindIsWeekly(t *testing.T) {
	cfg := testRepo(t, nil)
	fakeClient := &synth.Fake{Reply: "weekly draft"}
	out, err := mcpSynth(context.Background(), cfg, fakeClient, synthMCPInput{})
	if err != nil {
		t.Fatal(err)
	}
	var got synthMCPResponse
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Kind != "weekly" {
		t.Errorf("kind = %q, want weekly (default)", got.Kind)
	}
}

func TestMCPSynthPersistFalseWritesNothing(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00\nsome work\n",
	})
	fakeClient := &synth.Fake{Reply: "draft text"}
	out, err := mcpSynth(context.Background(), cfg, fakeClient, synthMCPInput{Kind: "weekly", Persist: false})
	if err != nil {
		t.Fatal(err)
	}
	var got synthMCPResponse
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	// API is called (text returned) but no file is written.
	if got.Text == "" {
		t.Error("text must not be empty: API should have been called")
	}
	if got.Wrote {
		t.Error("wrote=true but persist=false: no file should be written")
	}
	if fakeClient.CallCount != 1 {
		t.Errorf("synth client called %d times, want 1", fakeClient.CallCount)
	}
}

func TestMCPSynthPersistTrueWritesFile(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00\nsome work\n",
	})
	fakeClient := &synth.Fake{Reply: "## My weekly draft\n"}
	out, err := mcpSynth(context.Background(), cfg, fakeClient, synthMCPInput{Kind: "weekly", Persist: true})
	if err != nil {
		t.Fatal(err)
	}
	var got synthMCPResponse
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !got.Wrote {
		t.Error("wrote=false but persist=true: file should have been written")
	}
	if got.OutputPath == "" {
		t.Error("output_path must not be empty when wrote=true")
	}
}

func TestMCPSynthInvalidKindErrors(t *testing.T) {
	cfg := testRepo(t, nil)
	fakeClient := &synth.Fake{}
	_, err := mcpSynth(context.Background(), cfg, fakeClient, synthMCPInput{Kind: "bogus"})
	if err == nil {
		t.Fatal("expected error for invalid kind")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should mention the invalid kind, got: %v", err)
	}
}

func TestMCPSynthUnavailableReturnsCleanError(t *testing.T) {
	cfg := testRepo(t, nil)
	boom := errors.New("synthesis unavailable: no provider configured")
	fakeClient := &synth.Fake{ForcedErr: boom}
	_, err := mcpSynth(context.Background(), cfg, fakeClient, synthMCPInput{Kind: "weekly"})
	if err == nil {
		t.Fatal("expected error when synth client fails")
	}
	if !strings.Contains(err.Error(), "synth weekly") {
		t.Errorf("error should mention synth weekly context, got: %v", err)
	}
}

func TestMCPSynthAvailableClientRejectsLocalOnly(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.LocalOnly = true
	_, available, reason := synthAvailableClient(cfg)
	if available {
		t.Error("synthAvailableClient should report unavailable under local_only")
	}
	if reason == nil || !strings.Contains(reason.Error(), "local_only") {
		t.Errorf("reason should mention local_only, got: %v", reason)
	}
}

// TestMCPDocCoverage is a guardrail: every tool, resource, and prompt registered
// in cmd/mcp.go must appear in the README and integration docs. If you add new
// MCP surface, add its name/URI to the lists below AND update the docs.
func TestMCPDocCoverage(t *testing.T) {
	// Keep these lists in sync with mcp.AddTool / addResources / addPrompts in mcp.go.
	wantTools := []string{
		"search", "recent", "decisions", "threads", "show", "capture",
		"meetings", "todos", "done", "stats", "today", "ask", "synth",
	}
	wantResourceURIs := []string{
		"journal://today", "journal://recent", "journal://projects/{slug}/index",
	}
	wantPrompts := []string{
		"weekly-reflection", "decisions-review", "project-status",
	}

	docPaths := []string{
		"../README.md",
		"../docs/INTEGRATIONS.md",
		"../book/src/integrations/claude-desktop.md",
	}
	var sb strings.Builder
	for _, p := range docPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		sb.WriteString(string(data))
	}
	combined := sb.String()

	for _, name := range wantTools {
		if !strings.Contains(combined, name) {
			t.Errorf("MCP tool %q not found in docs (README.md, docs/INTEGRATIONS.md, book/src/integrations/claude-desktop.md); update docs when adding new tools", name)
		}
	}
	for _, uri := range wantResourceURIs {
		if !strings.Contains(combined, uri) {
			t.Errorf("MCP resource URI %q not found in docs; update docs/INTEGRATIONS.md and book/src/integrations/claude-desktop.md", uri)
		}
	}
	for _, name := range wantPrompts {
		if !strings.Contains(combined, name) {
			t.Errorf("MCP prompt %q not found in docs; update docs/INTEGRATIONS.md and book/src/integrations/claude-desktop.md", name)
		}
	}
}
