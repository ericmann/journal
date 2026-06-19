package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
)

func TestDecisionsFilterReturnsOnlyDecisions(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n" +
			"## 09:00 @decision\nchose pure-go sqlite driver\n\n" +
			"## 10:00 @question\nis the dev fund taxable?\n\n" +
			"## 11:00\nplain note no marker\n",
		"projects/canton/_index.md": "# 2026-06-01\n\n## 09:00 @decision\ndeclare as income\n",
	})

	// Decisions across all projects.
	all, err := listFromStore(context.Background(), cfg,
		store.Filter{Markers: []string{note.MarkerDecision}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d decisions, want 2", len(all))
	}
	for _, r := range all {
		if !strings.Contains(r.Snippet, "chose") && !strings.Contains(r.Snippet, "declare") {
			t.Errorf("non-decision leaked: %q", r.Snippet)
		}
	}

	// Decisions scoped to the canton project.
	canton, err := listFromStore(context.Background(), cfg,
		store.Filter{Project: "canton", Markers: []string{note.MarkerDecision}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(canton) != 1 || !strings.HasPrefix(canton[0].Path, "projects/canton/") {
		t.Errorf("canton decisions = %+v, want 1 under projects/canton/", canton)
	}
}

func TestRecentReturnsNewestFirst(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00\nearlier note\n\n## 17:00\nlater note\n",
	})
	results, err := listFromStore(context.Background(), cfg, store.Filter{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d, want 2", len(results))
	}
	if !strings.Contains(results[0].Snippet, "later") {
		t.Errorf("recent[0] = %q, want the later note first", results[0].Snippet)
	}
}

func TestDecisionsStatement(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"use sqlite-vec for vectors", "use sqlite-vec for vectors"},
		{"use sqlite-vec for vectors Rationale: Pure-Go, no cgo", "use sqlite-vec for vectors"},
		{"  trimmed  ", "trimmed"},
		{"", ""},
	}
	for _, tc := range tests {
		got := decisionsStatement(tc.input)
		if got != tc.want {
			t.Errorf("decisionsStatement(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDecisionsRationale(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"use sqlite-vec Rationale: Pure-Go, no cgo", "Rationale: Pure-Go, no cgo"},
		{"use sqlite-vec", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := decisionsRationale(tc.input)
		if got != tc.want {
			t.Errorf("decisionsRationale(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDateFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"daily/2026/06/2026-06-15.md", "2026-06-15"},
		{"projects/canton/notes/2026-06-01.md", "2026-06-01"},
		{"projects/canton/_index.md", ""},
		{"daily/2026/06/d.md", ""},
	}
	for _, tc := range tests {
		got := dateFromPath(tc.path)
		if got != tc.want {
			t.Errorf("dateFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestRenderDecisionsTextMode(t *testing.T) {
	results := []Result{
		{
			Path:      "daily/2026/06/2026-06-15.md",
			LineStart: 3,
			LineEnd:   5,
			Snippet:   "use sqlite-vec for vectors Rationale: Pure-Go, no cgo needed",
		},
		{
			Path:      "daily/2026/06/2026-06-10.md",
			LineStart: 5,
			LineEnd:   7,
			Snippet:   "adopt conventional commits",
		},
	}
	var buf bytes.Buffer
	if err := renderDecisions(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "2026-06-15") {
		t.Errorf("date missing: %s", out)
	}
	if !strings.Contains(out, "use sqlite-vec for vectors") {
		t.Errorf("statement missing: %s", out)
	}
	if !strings.Contains(out, "Rationale: Pure-Go, no cgo needed") {
		t.Errorf("rationale missing: %s", out)
	}
	if !strings.Contains(out, "2026-06-10") {
		t.Errorf("second date missing: %s", out)
	}
	if !strings.Contains(out, "adopt conventional commits") {
		t.Errorf("second statement missing: %s", out)
	}
}

func TestRenderDecisionsEmptyState(t *testing.T) {
	var buf bytes.Buffer
	if err := renderDecisions(&buf, nil, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no decisions") {
		t.Errorf("empty state hint missing: %s", buf.String())
	}
}

func TestRenderDecisionsJSONMode(t *testing.T) {
	results := []Result{
		{Path: "daily/2026/06/2026-06-15.md", LineStart: 3, LineEnd: 5, Snippet: "use sqlite-vec"},
	}
	var buf bytes.Buffer
	if err := renderDecisions(&buf, results, true); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(env.Results) != 1 {
		t.Errorf("json results = %d, want 1", len(env.Results))
	}
}

// TestDecisionsLegacyOpaqueBlock verifies that old-style multi-line blocks
// still surface in journal decisions output with their first line as the statement.
func TestDecisionsLegacyOpaqueBlock(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": "# 2026-06-01\n\n" +
			"## 09:00 @decision\nWe had a long discussion today about the database choice and\n" +
			"ultimately settled on using sqlite-vec because it avoids cgo.\n" +
			"This was the right call.\n",
	})
	results, err := listFromStore(context.Background(), cfg,
		store.Filter{Markers: []string{note.MarkerDecision}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	stmt := decisionsStatement(results[0].Snippet)
	if !strings.HasPrefix(stmt, "We had a long discussion") {
		t.Errorf("legacy statement = %q, want beginning of block body", stmt)
	}
}
