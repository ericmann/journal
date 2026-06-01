package cmd

import (
	"context"
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
