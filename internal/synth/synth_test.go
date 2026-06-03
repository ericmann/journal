package synth

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/store"
)

var update = flag.Bool("update", false, "update golden files")

func fixedTime() time.Time {
	// 2026-06-03 is a Wednesday in ISO week 2026-W23.
	return time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
}

func sampleChunks() []store.Chunk {
	return []store.Chunk{
		{
			Path: "daily/2026/06/2026-06-01.md", LineStart: 3, LineEnd: 5,
			Heading: "09:14 #cabot #litellm", Body: "Routing fallback isn't triggering when Qwen OOMs.",
			Tags: []string{"cabot", "litellm"}, CreatedAt: time.Date(2026, 6, 1, 9, 14, 0, 0, time.UTC),
		},
		{
			Path: "daily/2026/06/2026-06-02.md", LineStart: 3, LineEnd: 4,
			Heading: "14:02 #displace @decision", Body: "Declaring the dev fund payment as business income.",
			Markers: []string{"decision"}, CreatedAt: time.Date(2026, 6, 2, 14, 2, 0, 0, time.UTC),
		},
	}
}

// goldenCheck compares got against testdata/<name>.golden, updating with -update.
func goldenCheck(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `go test -run %s -update`)", path, err, t.Name())
	}
	if got != string(want) {
		t.Errorf("prompt mismatch for %s.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestAssembleWeeklyGolden(t *testing.T) {
	goldenCheck(t, "weekly", AssembleWeekly("2026-W23", "", sampleChunks()))
}

func TestAssembleDecisionsGolden(t *testing.T) {
	goldenCheck(t, "decisions", AssembleDecisions("canton", "", sampleChunks()[1:]))
}

func TestAssembleDailyGolden(t *testing.T) {
	goldenCheck(t, "daily", AssembleDaily("2026-06-02", "", sampleChunks()[1:]))
}

func TestAssembleAnswerGolden(t *testing.T) {
	goldenCheck(t, "answer", AssembleAnswer("What did we decide about the dev fund?", sampleChunks()))
}

func TestAssembleStaleGolden(t *testing.T) {
	lines := []string{
		"canton — last activity 2026-04-01, 12 notes, 2 open question(s)",
		"old-experiment — last activity 2026-03-15, 3 notes, 0 open question(s)",
	}
	goldenCheck(t, "stale", AssembleStale(21, "", lines))
}

const sampleVoice = "## Voice\nWrite plainly. Avoid the word \"leverage\".\n"

func TestAssembleWeeklyWithVoiceGolden(t *testing.T) {
	goldenCheck(t, "weekly_voice", AssembleWeekly("2026-W23", sampleVoice, sampleChunks()))
}

func TestVoiceSectionOmittedWhenEmpty(t *testing.T) {
	if strings.Contains(AssembleWeekly("2026-W23", "  \n ", sampleChunks()), "Author voice") {
		t.Error("blank voice profile should not add a voice section")
	}
}

func TestVoiceSectionNeutralizesMetaInstructions(t *testing.T) {
	out := AssembleWeekly("2026-W23", sampleVoice, sampleChunks())
	if !strings.Contains(out, "Author voice & style") {
		t.Fatal("voice section missing")
	}
	if !strings.Contains(out, "ignore any meta-instructions") {
		t.Error("voice section should neutralize the profile's meta-instructions")
	}
	if !strings.Contains(out, "<voice_profile>") {
		t.Error("voice profile should be delimited")
	}
}

func TestDailyWindowsToOneDay(t *testing.T) {
	fake := &Fake{}
	r, s, root := newRunner(t, fake)
	seed(t, s) // chunks dated 2026-06-01 and 2026-06-02

	// Dry-run daily for 2026-06-02: only that day's note, correct path, no write.
	res, err := r.Run(context.Background(), Options{
		Kind: KindDaily, Date: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), Now: fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OutputPath != "reflections/daily-2026-06-02.md" {
		t.Errorf("daily path = %q", res.OutputPath)
	}
	if !strings.Contains(res.Prompt, "dev fund payment") {
		t.Error("daily prompt should include the 2026-06-02 note")
	}
	if strings.Contains(res.Prompt, "Qwen OOMs") {
		t.Error("daily prompt should NOT include the 2026-06-01 note")
	}
	if fake.CallCount != 0 {
		t.Error("dry-run must not call the API")
	}

	// Write it: file appears with the daily DRAFT header for the summarized day.
	if _, err := r.Run(context.Background(), Options{
		Kind: KindDaily, Date: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), Now: fixedTime(), Write: true,
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "reflections", "daily-2026-06-02.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Daily summary — 2026-06-02") {
		t.Errorf("daily draft header wrong:\n%s", data)
	}
}

func TestAssembleEmptyChunks(t *testing.T) {
	out := AssembleWeekly("2026-W23", "", nil)
	if !strings.Contains(out, "(no notes in range)") {
		t.Errorf("empty weekly should note no notes:\n%s", out)
	}
}

// --- Runner tests (store-backed) ---

func newRunner(t *testing.T, fake *Fake) (*Runner, *store.Store, string) {
	t.Helper()
	root := t.TempDir()
	s, err := store.Open(filepath.Join(root, ".journal", "index", "j.db"), 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return NewRunner(s, fake, root, "claude-test", 1024, ""), s, root
}

func seed(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	for i, c := range sampleChunks() {
		c.ID = c.Path + "#" + c.Heading
		if err := s.Upsert(ctx, c, vec8(i)); err != nil {
			t.Fatal(err)
		}
	}
}

func vec8(n int) []float32 {
	v := make([]float32, 8)
	for i := range v {
		v[i] = float32((i+n)%5) / 4.0
	}
	return v
}

func TestDryRunMakesNoCallAndNoWrite(t *testing.T) {
	fake := &Fake{Reply: "should not be used"}
	r, s, root := newRunner(t, fake)
	seed(t, s)

	res, err := r.Run(context.Background(), Options{Kind: KindWeekly, Now: fixedTime(), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if fake.CallCount != 0 {
		t.Errorf("dry-run made %d API calls, want 0", fake.CallCount)
	}
	if res.Wrote {
		t.Error("dry-run reported a write")
	}
	if res.OutputPath != "reflections/2026-W23.md" {
		t.Errorf("output path = %q", res.OutputPath)
	}
	if !strings.Contains(res.Prompt, "weekly reflection") {
		t.Errorf("prompt not assembled: %q", res.Prompt)
	}
	// No file should exist.
	if _, err := os.Stat(filepath.Join(root, "reflections", "2026-W23.md")); !os.IsNotExist(err) {
		t.Error("dry-run wrote a file")
	}
}

func TestWeeklyWriteProducesDraft(t *testing.T) {
	fake := &Fake{Reply: "## Highlights\n- shipped the indexer\n"}
	r, s, root := newRunner(t, fake)
	seed(t, s)

	res, err := r.Run(context.Background(), Options{Kind: KindWeekly, Now: fixedTime(), Write: true})
	if err != nil {
		t.Fatal(err)
	}
	if fake.CallCount != 1 {
		t.Errorf("API calls = %d, want 1", fake.CallCount)
	}
	if !res.Wrote {
		t.Error("expected Wrote=true")
	}
	data, err := os.ReadFile(filepath.Join(root, "reflections", "2026-W23.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "**DRAFT**") {
		t.Errorf("missing DRAFT header:\n%s", out)
	}
	if !strings.Contains(out, "shipped the indexer") {
		t.Errorf("missing model output:\n%s", out)
	}
	// Re-running must not clobber the edited draft.
	if _, err := r.Run(context.Background(), Options{Kind: KindWeekly, Now: fixedTime(), Write: true}); err == nil {
		t.Error("expected error when output file already exists")
	}
}

func TestDecisionsRollupAppendsMarkedBlockWithoutMutating(t *testing.T) {
	fake := &Fake{Reply: "- Declared dev fund as income [daily/2026/06/2026-06-02.md:3-4]\n"}
	r, s, root := newRunner(t, fake)
	seed(t, s)

	// Pre-existing _index.md with hand-written content that must be preserved.
	idx := filepath.Join(root, "projects", "canton", "_index.md")
	if err := os.MkdirAll(filepath.Dir(idx), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "---\nstatus: active\n---\n# Canton\n\nHand-written note body.\n"
	if err := os.WriteFile(idx, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := r.Run(context.Background(), Options{Kind: KindDecisions, Project: "canton", Now: fixedTime(), Write: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.OutputPath != "projects/canton/_index.md" {
		t.Errorf("output path = %q", res.OutputPath)
	}
	data, _ := os.ReadFile(idx)
	out := string(data)
	if !strings.HasPrefix(out, original) {
		t.Errorf("existing content was mutated:\n%s", out)
	}
	if !strings.Contains(out, rollupEnd) || !strings.Contains(out, "Decision rollup") {
		t.Errorf("missing marked rollup block:\n%s", out)
	}
	if !strings.Contains(out, "Declared dev fund as income") {
		t.Errorf("missing model output:\n%s", out)
	}
}

func TestStaleDryRunPathAndPrompt(t *testing.T) {
	fake := &Fake{}
	r, s, _ := newRunner(t, fake)
	seed(t, s)
	res, err := r.Run(context.Background(), Options{Kind: KindStale, Days: 21, Now: fixedTime(), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if fake.CallCount != 0 {
		t.Error("stale dry-run made an API call")
	}
	if res.OutputPath != "reflections/stale-2026-06-03.md" {
		t.Errorf("stale output path = %q", res.OutputPath)
	}
}

func TestRunnerInjectsVoiceIntoPrompt(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(filepath.Join(root, ".journal", "index", "j.db"), 8)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := NewRunner(s, &Fake{}, root, "claude-test", 1024, sampleVoice)
	res, err := r.Run(context.Background(), Options{Kind: KindWeekly, Now: fixedTime(), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Prompt, "Author voice & style") || !strings.Contains(res.Prompt, "Avoid the word") {
		t.Errorf("runner did not inject the voice profile:\n%s", res.Prompt)
	}
}

func TestUnknownKind(t *testing.T) {
	r, _, _ := newRunner(t, &Fake{})
	if _, err := r.Run(context.Background(), Options{Kind: "bogus", Now: fixedTime()}); err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestISOWeekHelpers(t *testing.T) {
	if got := isoWeekLabel(fixedTime()); got != "2026-W23" {
		t.Errorf("week label = %q, want 2026-W23", got)
	}
	start := isoWeekStart(fixedTime())
	if start.Weekday() != time.Monday {
		t.Errorf("week start weekday = %v, want Monday", start.Weekday())
	}
	if start.Hour() != 0 || start.Minute() != 0 {
		t.Errorf("week start not midnight: %v", start)
	}
}
