package note

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestParseTags(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"plain text no tags", nil},
		{"routing #cabot fallback #litellm broke", []string{"cabot", "litellm"}},
		{"dedupe #Cabot and #cabot", []string{"cabot"}}, // case-folded + deduped
		{"trailing punct #canton, and #displace.", []string{"canton", "displace"}},
		{"not#atag midword", nil}, // must follow boundary
		{"email a@b#c should not eat", nil},
		{"#hyphen-tag and #under_score ok", []string{"hyphen-tag", "under_score"}},
	}
	for _, c := range cases {
		got := ParseTags(c.in)
		if !eq(got, c.want) {
			t.Errorf("ParseTags(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseMarkers(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"no markers here", nil},
		{"this is a @decision finally", []string{"decision"}},
		{"open @question and a @todo item", []string{"question", "todo"}},
		{"@notamarker is ignored", nil},        // only the known set
		{"dupe @todo @todo", []string{"todo"}}, // deduped
		{"email user@todo.com is not a marker", nil},
	}
	for _, c := range cases {
		got := ParseMarkers(c.in)
		if !eq(got, c.want) {
			t.Errorf("ParseMarkers(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValidMarker(t *testing.T) {
	for _, m := range []string{"decision", "question", "todo"} {
		if !ValidMarker(m) {
			t.Errorf("ValidMarker(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"", "idea", "note", "Decision"} {
		if ValidMarker(m) {
			t.Errorf("ValidMarker(%q) = true, want false", m)
		}
	}
}

func TestFormatBlock(t *testing.T) {
	b := Block{
		Time:    mustTime(t, "2026-06-01 09:14"),
		Tags:    []string{"cabot", "litellm"},
		Markers: []string{"decision"},
		Body:    "Routing fallback isn't triggering when Qwen OOMs.",
	}
	got := FormatBlock(b)
	want := "## 09:14 #cabot #litellm @decision\nRouting fallback isn't triggering when Qwen OOMs.\n"
	if got != want {
		t.Errorf("FormatBlock =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatBlockNoTagsNoMarkers(t *testing.T) {
	b := Block{Time: mustTime(t, "2026-06-01 14:02"), Body: "Just a thought."}
	got := FormatBlock(b)
	want := "## 14:02\nJust a thought.\n"
	if got != want {
		t.Errorf("FormatBlock = %q, want %q", got, want)
	}
}

func TestParseBlockHeadingRoundTrip(t *testing.T) {
	b := Block{
		Time:    mustTime(t, "2026-06-01 09:14"),
		Tags:    []string{"cabot", "litellm"},
		Markers: []string{"decision", "todo"},
		Body:    "body",
	}
	header := strings.SplitN(FormatBlock(b), "\n", 2)[0]
	hm, tags, markers, ok := ParseBlockHeading(header)
	if !ok {
		t.Fatalf("ParseBlockHeading(%q) not ok", header)
	}
	if hm != "09:14" {
		t.Errorf("time = %q, want 09:14", hm)
	}
	if !eq(tags, []string{"cabot", "litellm"}) {
		t.Errorf("tags = %v", tags)
	}
	if !eq(markers, []string{"decision", "todo"}) {
		t.Errorf("markers = %v", markers)
	}
}

func TestParseBlockHeadingNonBlock(t *testing.T) {
	for _, line := range []string{"# 2026-06-01", "plain text", "### too deep", "##nospace"} {
		if _, _, _, ok := ParseBlockHeading(line); ok {
			t.Errorf("ParseBlockHeading(%q) ok = true, want false", line)
		}
	}
}

func TestDailyPath(t *testing.T) {
	got := DailyPath("/repo", mustTime(t, "2026-06-01 09:14"))
	want := filepath.Join("/repo", "daily", "2026", "06", "2026-06-01.md")
	if got != want {
		t.Errorf("DailyPath = %q, want %q", got, want)
	}
}

func TestProjectNotesPath(t *testing.T) {
	got := ProjectNotesPath("/repo", "canton", mustTime(t, "2026-06-01 09:14"))
	want := filepath.Join("/repo", "projects", "canton", "notes", "2026-06-01.md")
	if got != want {
		t.Errorf("ProjectNotesPath = %q, want %q", got, want)
	}
}

func TestAppendDailyCreatesH1ThenAppends(t *testing.T) {
	root := t.TempDir()
	day := mustTime(t, "2026-06-01 09:14")

	path, err := AppendDaily(root, Block{Time: day, Tags: []string{"cabot"}, Body: "first note"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	wantPrefix := "# 2026-06-01\n\n## 09:14 #cabot\nfirst note\n"
	if got != wantPrefix {
		t.Fatalf("after first append =\n%q\nwant\n%q", got, wantPrefix)
	}

	// Second append the same day must not duplicate the H1 and must keep the
	// first block verbatim (append-only).
	day2 := mustTime(t, "2026-06-01 14:02")
	if _, err := AppendDaily(root, Block{Time: day2, Markers: []string{"decision"}, Body: "second note"}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	want := "# 2026-06-01\n\n## 09:14 #cabot\nfirst note\n\n## 14:02 @decision\nsecond note\n"
	if got != want {
		t.Fatalf("after second append =\n%q\nwant\n%q", got, want)
	}
	if strings.Count(got, "# 2026-06-01\n") != 1 {
		t.Errorf("H1 appears more than once:\n%s", got)
	}
}

func TestAppendDailyPreservesExistingBodyExactly(t *testing.T) {
	root := t.TempDir()
	day := mustTime(t, "2026-06-01 09:14")
	path := DailyPath(root, day)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing file with unusual spacing the tool must not normalize.
	original := "# 2026-06-01\n\n## 08:00\nhand-written   body with  spaces\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := AppendDaily(root, Block{Time: mustTime(t, "2026-06-01 10:00"), Body: "appended"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), original) {
		t.Errorf("existing body was mutated; file =\n%q", data)
	}
}

func TestAppendProjectWritesUnderProjects(t *testing.T) {
	root := t.TempDir()
	day := mustTime(t, "2026-06-01 09:14")
	path, err := AppendProject(root, "canton", Block{Time: day, Markers: []string{"decision"}, Body: "declared as income"})
	if err != nil {
		t.Fatal(err)
	}
	want := ProjectNotesPath(root, "canton", day)
	if path != want {
		t.Errorf("AppendProject path = %q, want %q", path, want)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "## 09:14 @decision\ndeclared as income\n") {
		t.Errorf("project note missing block:\n%s", data)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Canton":            "canton",
		"My Project":        "my-project",
		"weird__Name!!":     "weird-name",
		"  trim me  ":       "trim-me",
		"already-good-slug": "already-good-slug",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
