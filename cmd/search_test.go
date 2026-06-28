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
	// Enable reranking so search tests exercise the rerank path (the Fake
	// reranks by lexical overlap regardless of the model name).
	cfg.Reranker = "rerank-model"
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

// TestIndexThenSearchEndToEnd_Issue2 is a regression guard for
// https://github.com/ericmann/journal/issues/2: `journal index` crashed with a
// SIGSEGV inside wazero-JIT-compiled SQLite Wasm (a bad `runtime.memmove` call)
// under Go 1.26 + bundled wazero v1.8.2 on linux/amd64. It drives the exact
// faulting path end to end — chunk → embed → real sqlite-vec store upsert
// (wazero) → KNN search (wazero) — using a realistic multi-KB body so the
// SQLite blob movement the fault rode on is genuinely exercised, not a one-line
// fixture. On a supported Go+wazero combination it must index and retrieve
// cleanly; a regressed toolchain/wazero pairing faults here (most visibly in CI
// on linux/amd64).
func TestIndexThenSearchEndToEnd_Issue2(t *testing.T) {
	// ~8 KB body — a plausible journal entry / transcript chunk.
	body := strings.Repeat("Worked through the FDE triage queue and the local-only synthesis design. ", 110)
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-17.md": "# 2026-06-17\n\n## 10:00 #fde\n" + body + "\n",
	})
	results, err := runSearch(context.Background(), cfg, fake, "FDE triage local-only synthesis", 3, store.Filter{})
	if err != nil {
		t.Fatalf("index→search through wazero-backed SQLite failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results — the wazero/SQLite round-trip returned nothing")
	}
	if !strings.HasPrefix(results[0].Citation(), "daily/2026/06/2026-06-17.md:") {
		t.Errorf("unexpected top citation: %q", results[0].Citation())
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
	for _, key := range []string{"path", "line_start", "line_end", "heading", "snippet", "score", "tags", "markers", "source"} {
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

func TestSearchWithRerankerDisabledUsesVectorOrder(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00\nalpha beta\n\n## 09:01\ngamma delta\n",
	})
	cfg.Reranker = "" // disabled: no rerank call, vector-distance order
	results, err := runSearch(context.Background(), cfg, fake, "alpha", 5, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	for _, r := range results {
		if r.Score <= 0 {
			t.Errorf("disabled-rerank score should be distance-derived (>0), got %v", r.Score)
		}
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

func TestSearchSourceFilterNote(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00 #cabot\ncabot note content\n",
	})
	// Source filter "note" should return the daily note result.
	results, err := runSearch(context.Background(), cfg, fake, "cabot note", 5, store.Filter{Sources: []string{store.SourceNote}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results with source=note filter")
	}
	for _, r := range results {
		if r.Source != store.SourceNote {
			t.Errorf("source filter leaked: got source=%q, want %q", r.Source, store.SourceNote)
		}
	}
}

func TestSearchSourceLabelInResults(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00\nsource label test\n",
	})
	results, err := runSearch(context.Background(), cfg, fake, "source label test", 5, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Source == "" {
		t.Error("result.Source is empty, want a non-empty source label")
	}
	// Text render must include the source label.
	var buf bytes.Buffer
	if err := renderResults(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "["+results[0].Source+"]") {
		t.Errorf("text render missing source label [%s]: %q", results[0].Source, buf.String())
	}
}

func TestParseSourceFilter(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want []string
		err  bool
	}{
		{nil, nil, false},
		{[]string{"all"}, nil, false},
		{[]string{"notes"}, []string{store.SourceNote}, false},
		{[]string{"note"}, []string{store.SourceNote}, false},
		{[]string{"transcript"}, []string{store.SourceTranscript}, false},
		{[]string{"meetings"}, []string{store.SourceTranscript}, false},
		{[]string{"notes", "transcript"}, []string{store.SourceNote, store.SourceTranscript}, false},
		{[]string{"notes", "meetings"}, []string{store.SourceNote, store.SourceTranscript}, false},
		{[]string{"invalid"}, nil, true},
	} {
		got, err := parseSourceFilter(tc.in)
		if tc.err && err == nil {
			t.Errorf("parseSourceFilter(%v): want error, got nil", tc.in)
		}
		if !tc.err && err != nil {
			t.Errorf("parseSourceFilter(%v): unexpected error: %v", tc.in, err)
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseSourceFilter(%v) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseSourceFilter(%v)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
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
