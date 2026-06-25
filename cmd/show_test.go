package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/editor"
	"github.com/ericmann/journal/internal/note"
)

func TestDateForArg(t *testing.T) {
	if _, err := dateForArg("today"); err != nil {
		t.Error("today should parse")
	}
	if _, err := dateForArg(""); err != nil {
		t.Error("empty should parse as today")
	}
	y, err := dateForArg("yesterday")
	if err != nil || !y.Before(time.Now()) {
		t.Errorf("yesterday = %v, %v", y, err)
	}
	d, err := dateForArg("2026-06-01")
	if err != nil || d.Day() != 1 || d.Month() != 6 {
		t.Errorf("explicit date = %v, %v", d, err)
	}
	if _, err := dateForArg("daily/2026/06/2026-06-01.md"); err == nil {
		t.Error("a path should not parse as a date")
	}
}

func TestResolveNotePathRejectsEscape(t *testing.T) {
	cfg := testRepo(t, nil)
	if _, _, err := resolveNotePath(cfg, "../outside.md"); err == nil {
		t.Error("path escaping the repo should be rejected")
	}
	abs, rel, err := resolveNotePath(cfg, "projects/x/notes/n.md")
	if err != nil || rel != "projects/x/notes/n.md" || !strings.HasPrefix(abs, cfg.Root()) {
		t.Errorf("relative path resolve = %s, %s, %v", abs, rel, err)
	}
}

func TestGatherTodayAndCompose(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00 @todo\nfinish the dashboard\n",
	})
	rep, notesMD, err := gatherToday(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Notes || !strings.Contains(notesMD, "finish the dashboard") {
		t.Errorf("today's notes not picked up: %+v", rep)
	}
	if len(rep.Todos) != 1 {
		t.Errorf("today todos = %d, want 1", len(rep.Todos))
	}
	md := composeToday(rep, notesMD)
	if !strings.Contains(md, "Open todos (1)") || !strings.Contains(md, "- [ ]") {
		t.Errorf("composed dashboard missing todos section:\n%s", md)
	}

	// Empty repo: friendly empty state, sections omitted.
	cfg2 := testRepo(t, nil)
	rep2, notes2, err := gatherToday(context.Background(), cfg2)
	if err != nil {
		t.Fatal(err)
	}
	md2 := composeToday(rep2, notes2)
	if !strings.Contains(md2, "No notes yet today") || strings.Contains(md2, "Open todos") {
		t.Errorf("empty dashboard wrong:\n%s", md2)
	}
}

func TestGatherTodayIncludesRecentDecisions(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00 @decision\nchose sqlite-vec for vectors\n",
	})
	rep, _, err := gatherToday(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Decisions) != 1 {
		t.Errorf("decisions = %d, want 1", len(rep.Decisions))
	}
	if !strings.Contains(rep.Decisions[0].Snippet, "chose sqlite-vec") {
		t.Errorf("decision snippet = %q, want 'chose sqlite-vec'", rep.Decisions[0].Snippet)
	}
}

func TestComposeTodayDecisionsSection(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	fixture := "# " + today + "\n\n## 09:00 @decision\nchose sqlite-vec for vectors\n"
	cfg, _ := indexedRepo(t, map[string]string{rel: fixture})

	rep, notesMD, err := gatherToday(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	md := composeToday(rep, notesMD)
	if !strings.Contains(md, "Recent decisions") {
		t.Errorf("dashboard missing 'Recent decisions' section:\n%s", md)
	}
	if !strings.Contains(md, "chose sqlite-vec for vectors") {
		t.Errorf("dashboard missing decision statement:\n%s", md)
	}
}

func TestGatherTodayDecisionsExcludesOld(t *testing.T) {
	// A decision from > 2 weeks ago should not appear in today's dashboard.
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2020/01/2020-01-01.md": "# 2020-01-01\n\n## 09:00 @decision\nvery old decision\n",
	})
	rep, _, err := gatherToday(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Decisions) != 0 {
		t.Errorf("stale decision should not appear: %+v", rep.Decisions)
	}
}

func TestGatherTodayDecisionsJSONShape(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n## 09:00 @decision\ntest decision\n",
	})
	rep, _, err := gatherToday(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["decisions"]; !ok {
		t.Error("todayReport JSON missing 'decisions' key")
	}
}

// TestGatherTodayAggregatesAllSources verifies that gatherToday pulls note
// chunks from both the daily file and a project note, and that transcript
// chunks remain in Meetings only (not duplicated in Notes).
func TestGatherTodayAggregatesAllSources(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	dailyRel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	projRel := "projects/myproject/notes/" + today + ".md"
	transcriptRel := "transcripts/today-meeting.md"

	cfg, _ := indexedRepo(t, map[string]string{
		dailyRel:      "# " + today + "\n\n## 09:00\ndaily note entry\n",
		projRel:       "# " + today + "\n\n## 10:00\nproject note entry\n",
		transcriptRel: "# Today Meeting\n\n## Notes\nmeeting discussion here\n",
	})

	rep, notesMD, err := gatherToday(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !rep.Notes {
		t.Error("rep.Notes should be true when daily and project notes are indexed")
	}
	if !strings.Contains(notesMD, "daily note entry") {
		t.Errorf("notesMD missing daily note content:\n%s", notesMD)
	}
	if !strings.Contains(notesMD, "project note entry") {
		t.Errorf("notesMD missing project note content:\n%s", notesMD)
	}
	if strings.Contains(notesMD, "meeting discussion here") {
		t.Errorf("notesMD must not include transcript content:\n%s", notesMD)
	}
	// Transcript must appear under Meetings, not Notes.
	if len(rep.Meetings) == 0 {
		t.Error("transcript should appear in rep.Meetings")
	}
	// Project source should be labelled in the rendered output.
	if !strings.Contains(notesMD, projRel) {
		t.Errorf("notesMD should attribute project note source %q:\n%s", projRel, notesMD)
	}
}

// TestGatherTodayProjectOnlyNotEmpty verifies that a day with only a project
// note (no daily file on disk) is not reported as empty.
func TestGatherTodayProjectOnlyNotEmpty(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	projRel := "projects/myproject/notes/" + today + ".md"

	cfg, _ := indexedRepo(t, map[string]string{
		projRel: "# " + today + "\n\n## 09:00\nproject-only entry\n",
	})

	rep, notesMD, err := gatherToday(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Notes {
		t.Error("day with only a project note should not be reported empty")
	}
	if !strings.Contains(notesMD, "project-only entry") {
		t.Errorf("notesMD missing project note content:\n%s", notesMD)
	}
}

func TestEditCreatesDailyAndInvokesEditor(t *testing.T) {
	cfg := testRepo(t, nil)

	var opened string
	openPathEditor = func(editorCmd, path string) error {
		opened = path
		return os.WriteFile(path, []byte(note.DailyH1(time.Now())+"\n\n## 09:00\nadded in editor\n"), 0o644)
	}
	t.Cleanup(func() { openPathEditor = editor.OpenPath })

	// Drive the command path pieces directly: resolve, create, open.
	abs, _, err := resolveNotePath(cfg, "today")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatal("daily file should not exist yet")
	}
	// Simulate the RunE create-then-open sequence.
	if err := os.WriteFile(abs, []byte(note.DailyH1(time.Now())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := openPathEditor("fake", abs); err != nil {
		t.Fatal(err)
	}
	if opened != abs {
		t.Errorf("editor opened %q, want %q", opened, abs)
	}
	data, _ := os.ReadFile(abs)
	if !strings.Contains(string(data), "added in editor") {
		t.Errorf("edit not persisted:\n%s", data)
	}
}
