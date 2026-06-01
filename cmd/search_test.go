package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/store"
)

// indexedRepo creates a repo, indexes it with the fake embedder, and returns the
// config + fake so search can reuse the same deterministic embedder.
func indexedRepo(t *testing.T, files map[string]string) (*config.Config, *embed.Fake) {
	t.Helper()
	cfg := testRepo(t, files)
	fake := embed.NewFake(cfg.EmbedDim)
	if _, err := runIndex(context.Background(), cfg, fake, indexOptions{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	return cfg, fake
}

func TestSearchReturnsRelevantCitation(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": "# 2026-06-01\n\n" +
			"## 09:14 #cabot\nlitellm fallback routing is broken when qwen ooms\n\n" +
			"## 10:00 #taxes\nfiled the quarterly estimated taxes today\n\n" +
			"## 11:00 #displace\nset up the displace workspace clone\n",
	})
	results, err := runSearch(context.Background(), cfg, fake, "litellm fallback routing", 3, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	top := results[0]
	if !strings.Contains(top.Snippet, "litellm fallback routing") {
		t.Errorf("top result not the relevant chunk: %q", top.Snippet)
	}
	if top.Score <= 0 {
		t.Errorf("top score = %v, want > 0 (rerank overlap)", top.Score)
	}
	// Citation must be path:line_start-line_end.
	if !strings.HasPrefix(top.Citation(), "daily/2026/06/2026-06-01.md:") || !strings.Contains(top.Citation(), "-") {
		t.Errorf("citation malformed: %q", top.Citation())
	}
}

func TestSearchHonorsK(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00\nalpha\n\n## 09:01\nbeta\n\n## 09:02\ngamma\n",
	})
	results, err := runSearch(context.Background(), cfg, fake, "alpha", 1, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("got %d results, want 1 (k=1)", len(results))
	}
}

func TestSearchProjectFilter(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md":        "# 2026-06-01\n\n## 09:00 #tax\ntax note in daily\n",
		"projects/canton/_index.md": "# 2026-06-01\n\n## 09:00 #tax\ntax note in canton project\n",
	})
	results, err := runSearch(context.Background(), cfg, fake, "tax note", 10, store.Filter{Project: "canton"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if !strings.HasPrefix(r.Path, "projects/canton/") {
			t.Errorf("project filter leaked: %s", r.Path)
		}
	}
	if len(results) == 0 {
		t.Error("expected at least one canton result")
	}
}

func TestSearchJSONSchema(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:14 #cabot @decision\nlitellm fallback note\n",
	})
	results, err := runSearch(context.Background(), cfg, fake, "litellm fallback", 5, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := renderResults(&buf, results, true); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(env.Results) == 0 {
		t.Fatal("no results in JSON")
	}
	r := env.Results[0]
	for _, key := range []string{"path", "line_start", "line_end", "heading", "snippet", "score", "tags", "markers"} {
		if _, ok := r[key]; !ok {
			t.Errorf("JSON result missing key %q", key)
		}
	}
}

func TestRenderErrorJSONEnvelope(t *testing.T) {
	var buf bytes.Buffer
	err := renderError(&buf, context.DeadlineExceeded, true)
	if err != errSilent {
		t.Errorf("json error should return errSilent, got %v", err)
	}
	var env map[string]string
	if jerr := json.Unmarshal(buf.Bytes(), &env); jerr != nil {
		t.Fatalf("invalid JSON error: %v", jerr)
	}
	if env["error"] == "" {
		t.Errorf("expected error field, got %v", env)
	}
}

func TestRenderErrorTextPassThrough(t *testing.T) {
	var buf bytes.Buffer
	err := renderError(&buf, context.DeadlineExceeded, false)
	if err != context.DeadlineExceeded {
		t.Errorf("text mode should return original error, got %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("text mode should not write to out, got %q", buf.String())
	}
}

func TestEmptySearchIsNotError(t *testing.T) {
	cfg := testRepo(t, nil) // no notes, index never run
	fake := embed.NewFake(cfg.EmbedDim)
	// Open creates an empty store; KNN returns nothing.
	results, err := runSearch(context.Background(), cfg, fake, "anything", 5, store.Filter{})
	if err != nil {
		t.Fatalf("empty search should not error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
	var buf bytes.Buffer
	if err := renderResults(&buf, results, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"results": []`) {
		t.Errorf("empty JSON should have empty results array, got %s", buf.String())
	}
}

func TestSnippetTruncates(t *testing.T) {
	long := strings.Repeat("word ", 100)
	s := snippet(long, 20)
	if len([]rune(s)) > 21 { // 20 + ellipsis
		t.Errorf("snippet too long: %d runes", len([]rune(s)))
	}
	if !strings.HasSuffix(s, "…") {
		t.Errorf("expected ellipsis, got %q", s)
	}
}

func TestRerankFallbackOnError(t *testing.T) {
	// failingReranker errors on Rerank; search must fall back to distance order.
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00\nalpha beta\n\n## 09:01\ngamma delta\n",
	})
	fr := &failingReranker{Fake: embed.NewFake(cfg.EmbedDim)}
	results, err := runSearch(context.Background(), cfg, fr, "alpha", 5, store.Filter{})
	if err != nil {
		t.Fatalf("search should survive rerank failure: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results on fallback")
	}
	for _, r := range results {
		if r.Score <= 0 {
			t.Errorf("fallback score should be distance-derived (>0), got %v", r.Score)
		}
	}
}

type failingReranker struct{ *embed.Fake }

func (f *failingReranker) Rerank(context.Context, string, []string) ([]float32, error) {
	return nil, context.Canceled
}

func TestBuildThreadsStaleLogic(t *testing.T) {
	ref := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	infos := []store.ProjectInfo{
		{Slug: "active", LastActivity: ref.AddDate(0, 0, -2), Chunks: 5, OpenQuestions: 1},
		{Slug: "stale", LastActivity: ref.AddDate(0, 0, -30), Chunks: 3, OpenQuestions: 2},
		{Slug: "unknown", Chunks: 1},
	}
	all := buildThreads(infos, false, 14, ref)
	if len(all) != 3 {
		t.Fatalf("got %d threads, want 3", len(all))
	}
	byName := map[string]Thread{}
	for _, th := range all {
		byName[th.Project] = th
	}
	if byName["active"].Stale {
		t.Error("active thread marked stale")
	}
	if !byName["stale"].Stale || !byName["unknown"].Stale {
		t.Error("stale/unknown threads should be stale")
	}
	if byName["active"].DaysSince != 2 {
		t.Errorf("active days_since = %d, want 2", byName["active"].DaysSince)
	}

	staleOnly := buildThreads(infos, true, 14, ref)
	if len(staleOnly) != 2 {
		t.Errorf("stale-only = %d threads, want 2", len(staleOnly))
	}
}
