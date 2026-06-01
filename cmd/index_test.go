package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
)

// testRepo creates an initialized journal repo with the given files and returns
// its loaded config.
func testRepo(t *testing.T, files map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
	if _, err := initRepo(root); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRunIndexEmbedsThenNoopReindexIsFast(t *testing.T) {
	cfg := testRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": "# 2026-06-01\n\n## 09:14 #cabot\nfallback note\n\n## 14:02 @decision\nbusiness income\n",
	})
	fake := embed.NewFake(cfg.EmbedDim)
	ctx := context.Background()

	st, err := runIndex(ctx, cfg, fake, indexOptions{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Embedded != 2 {
		t.Errorf("first index embedded = %d, want 2", st.Embedded)
	}
	before := fake.EmbedTexts

	start := time.Now()
	st2, err := runIndex(ctx, cfg, fake, indexOptions{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if fake.EmbedTexts != before {
		t.Errorf("no-op reindex made %d embed calls, want 0", fake.EmbedTexts-before)
	}
	if st2.Embedded != 0 {
		t.Errorf("no-op reindex embedded = %d, want 0", st2.Embedded)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("no-op reindex took %v, want < 2s", d)
	}
}

func TestRunIndexExcludesReflectionsAndJournal(t *testing.T) {
	cfg := testRepo(t, map[string]string{
		"daily/2026/06/d.md":      "# 2026-06-01\n\n## 09:14\nkept\n",
		"reflections/2026-W23.md": "# 2026-06-01\n\n## 09:14\nignored synthesis output\n",
	})
	fake := embed.NewFake(cfg.EmbedDim)
	st, err := runIndex(context.Background(), cfg, fake, indexOptions{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	// Only the daily file's single chunk should be embedded.
	if st.Embedded != 1 {
		t.Errorf("embedded = %d, want 1 (reflections excluded)", st.Embedded)
	}
}

func TestRunIndexRebuildReembeds(t *testing.T) {
	cfg := testRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:14\nnote\n",
	})
	fake := embed.NewFake(cfg.EmbedDim)
	ctx := context.Background()
	if _, err := runIndex(ctx, cfg, fake, indexOptions{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	before := fake.EmbedTexts
	if _, err := runIndex(ctx, cfg, fake, indexOptions{rebuild: true}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if fake.EmbedTexts-before != 1 {
		t.Errorf("rebuild re-embedded %d, want 1", fake.EmbedTexts-before)
	}
}

func TestParseSince(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"", 0, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"14d", 14 * 24 * time.Hour, false},
		{"36h", 36 * time.Hour, false},
		{"90m", 90 * time.Minute, false},
		{"banana", 0, true},
		{"2x", 0, true},
	}
	for _, c := range cases {
		got, err := parseSince(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseSince(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSince(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseSince(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
