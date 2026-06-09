package cmd

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStreaks(t *testing.T) {
	ref := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	days := func(ds ...string) map[string]bool {
		m := map[string]bool{}
		for _, d := range ds {
			m[d] = true
		}
		return m
	}

	// Today + 2 prior days = current 3; an older 4-day run is the longest.
	cur, longest := streaks(days(
		"2026-06-09", "2026-06-08", "2026-06-07",
		"2026-05-01", "2026-05-02", "2026-05-03", "2026-05-04",
	), ref)
	if cur != 3 || longest != 4 {
		t.Errorf("streaks = %d/%d, want 3/4", cur, longest)
	}

	// Nothing today, but yesterday counts (in-progress day doesn't break it).
	cur, _ = streaks(days("2026-06-08", "2026-06-07"), ref)
	if cur != 2 {
		t.Errorf("yesterday-anchored streak = %d, want 2", cur)
	}

	// Gap before yesterday → streak 0.
	cur, _ = streaks(days("2026-06-05"), ref)
	if cur != 0 {
		t.Errorf("broken streak = %d, want 0", cur)
	}

	if c, l := streaks(nil, ref); c != 0 || l != 0 {
		t.Errorf("empty = %d/%d", c, l)
	}
}

func TestGatherStatsAggregates(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rel := "daily/" + time.Now().Format("2006/01") + "/" + today + ".md"
	cfg, _ := indexedRepo(t, map[string]string{
		rel: "# " + today + "\n\n" +
			"## 09:00 #cabot @todo\nfix the router\n\n" +
			"## 10:00 #cabot @decision\nchose pure go\n\n" +
			"## 11:00 @done 2026-06-08\nshipped it\n",
		"projects/janus/notes/" + today + ".md": "# " + today + "\n\n## 12:00 #janus @question\nis the scope fixed?\n",
	})
	rep, err := gatherStats(context.Background(), cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.NoteChunks != 4 {
		t.Errorf("note chunks = %d, want 4", rep.NoteChunks)
	}
	if rep.OpenTodos != 1 || rep.DoneTodos != 1 || rep.Decisions != 1 || rep.Questions != 1 {
		t.Errorf("markers = todo:%d done:%d dec:%d q:%d, want 1 each",
			rep.OpenTodos, rep.DoneTodos, rep.Decisions, rep.Questions)
	}
	if rep.Projects != 1 {
		t.Errorf("projects = %d, want 1", rep.Projects)
	}
	if rep.CurrentStreak < 1 {
		t.Errorf("current streak = %d, want ≥1 (notes exist today)", rep.CurrentStreak)
	}
	if len(rep.TopTags) == 0 || rep.TopTags[0].Tag != "cabot" || rep.TopTags[0].Count != 2 {
		t.Errorf("top tags = %+v, want cabot×2 first", rep.TopTags)
	}

	txt := statsText(rep)
	for _, want := range []string{"current streak", "open todos", "#cabot"} {
		if !strings.Contains(txt, want) {
			t.Errorf("stats text missing %q:\n%s", want, txt)
		}
	}
}
