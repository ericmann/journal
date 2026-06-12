package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const dailySample = `# 2026-06-01

## 09:14 #cabot #litellm
Routing fallback isn't triggering when Qwen OOMs.
Health check passes before the model loads.

## 14:02 #displace @decision
Declaring the dev fund payment as business income.
`

func TestChunkLineRangesAndCount(t *testing.T) {
	chunks := Chunk("daily/2026/06/2026-06-01.md", dailySample)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	c0 := chunks[0]
	if c0.LineStart != 3 || c0.LineEnd != 5 {
		t.Errorf("chunk0 lines = %d-%d, want 3-5", c0.LineStart, c0.LineEnd)
	}
	if c0.Heading != "09:14 #cabot #litellm" {
		t.Errorf("chunk0 heading = %q", c0.Heading)
	}
	c1 := chunks[1]
	if c1.LineStart != 7 || c1.LineEnd != 8 {
		t.Errorf("chunk1 lines = %d-%d, want 7-8", c1.LineStart, c1.LineEnd)
	}
}

func TestChunkTagsMarkersCreatedAt(t *testing.T) {
	chunks := Chunk("daily/2026/06/2026-06-01.md", dailySample)
	c0, c1 := chunks[0], chunks[1]
	if !eq(c0.Tags, []string{"cabot", "litellm"}) {
		t.Errorf("chunk0 tags = %v", c0.Tags)
	}
	if len(c0.Markers) != 0 {
		t.Errorf("chunk0 markers = %v, want none", c0.Markers)
	}
	if !eq(c1.Markers, []string{"decision"}) {
		t.Errorf("chunk1 markers = %v", c1.Markers)
	}
	want := time.Date(2026, 6, 1, 9, 14, 0, 0, time.UTC)
	if !c0.CreatedAt.Equal(want) {
		t.Errorf("chunk0 created = %v, want %v", c0.CreatedAt, want)
	}
}

func TestChunkBodyExcludesHeadingAndTrailingBlanks(t *testing.T) {
	chunks := Chunk("daily/d.md", "# 2026-06-01\n\n## 09:14\nbody line\n\n\n")
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	if chunks[0].Body != "body line" {
		t.Errorf("body = %q, want %q", chunks[0].Body, "body line")
	}
	if chunks[0].LineEnd != 4 {
		t.Errorf("line_end = %d, want 4 (trailing blanks trimmed)", chunks[0].LineEnd)
	}
}

func TestChunkPreambleAndFrontmatterIgnored(t *testing.T) {
	content := `---
status: active
tags: [canton]
---
# Canton tracking

Some intro prose that is preamble.

## 10:00 @question
Is the dev fund taxable?
`
	chunks := Chunk("projects/canton/_index.md", content)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1 (preamble/frontmatter ignored)", len(chunks))
	}
	if chunks[0].Project != "canton" {
		t.Errorf("project = %q, want canton", chunks[0].Project)
	}
	if !eq(chunks[0].Markers, []string{"question"}) {
		t.Errorf("markers = %v", chunks[0].Markers)
	}
}

func TestChunkIDStableAcrossRunsAndWhitespace(t *testing.T) {
	a := ChunkID("daily/d.md", "09:14 #x", "hello world")
	b := ChunkID("daily/d.md", "09:14 #x", "hello world  \n") // trailing ws
	if a != b {
		t.Error("hash should ignore trailing whitespace / blank lines")
	}
	// Body edit changes the hash.
	c := ChunkID("daily/d.md", "09:14 #x", "hello worlds")
	if a == c {
		t.Error("hash should change when body changes")
	}
	// Path and heading participate in identity.
	if ChunkID("daily/e.md", "09:14 #x", "hello world") == a {
		t.Error("hash should change when path changes")
	}
	if ChunkID("daily/d.md", "09:15 #x", "hello world") == a {
		t.Error("hash should change when heading changes")
	}
}

// Moving a block within a file (line numbers change, body unchanged) must not
// change its identity — so re-indexing won't re-embed it.
func TestChunkIDIndependentOfLineNumbers(t *testing.T) {
	first := Chunk("daily/d.md", "# 2026-06-01\n\n## 09:14\nalpha\n")
	shifted := Chunk("daily/d.md", "# 2026-06-01\n\n## 08:00\ninserted\n\n## 09:14\nalpha\n")
	// The 09:14/alpha block is chunk[0] in first and chunk[1] in shifted.
	if first[0].ID != shifted[1].ID {
		t.Error("chunk identity changed due to line shift")
	}
	if first[0].LineStart == shifted[1].LineStart {
		t.Error("test setup wrong: line numbers should differ")
	}
}

func TestProjectForPath(t *testing.T) {
	cases := map[string]string{
		"projects/canton/_index.md":        "canton",
		"projects/canton/notes/2026-06.md": "canton",
		"daily/2026/06/2026-06-01.md":      "",
		"reflections/2026-W23.md":          "",
		"README.md":                        "",
	}
	for in, want := range cases {
		if got := ProjectForPath(in); got != want {
			t.Errorf("ProjectForPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// Regression: a capture block whose content uses its own `# ` headings (pasted
// markdown) must keep that content in the block body. Previously any H1
// terminated the block, leaving an empty chunk and dropping everything after.
func TestChunkNonDateH1IsBodyContent(t *testing.T) {
	content := `# 2026-06-12

## 10:30
# Merck Human Health

Discovery focused engagement.

# Entrepreneur Media

Reduce the Piano footprint.

## 11:00
follow-up note
`
	chunks := Chunk("projects/fde-triage/notes/2026-06-12.md", content)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	c0 := chunks[0]
	if c0.Heading != "10:30" {
		t.Errorf("chunk0 heading = %q", c0.Heading)
	}
	for _, want := range []string{"# Merck Human Health", "Discovery focused engagement.", "# Entrepreneur Media", "Reduce the Piano footprint."} {
		if !strings.Contains(c0.Body, want) {
			t.Errorf("chunk0 body missing %q; body = %q", want, c0.Body)
		}
	}
	if c0.LineStart != 3 || c0.LineEnd != 10 {
		t.Errorf("chunk0 lines = %d-%d, want 3-10 (trailing blank trimmed)", c0.LineStart, c0.LineEnd)
	}
	// A later date H1 still flushes and re-dates subsequent blocks.
	if chunks[1].Heading != "11:00" || chunks[1].Body != "follow-up note" {
		t.Errorf("chunk1 = %q / %q", chunks[1].Heading, chunks[1].Body)
	}
	want := time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC)
	if !c0.CreatedAt.Equal(want) {
		t.Errorf("chunk0 created = %v, want %v", c0.CreatedAt, want)
	}
}

func TestChunkEmptyAndNoBlocks(t *testing.T) {
	if c := Chunk("d.md", ""); len(c) != 0 {
		t.Errorf("empty content -> %d chunks, want 0", len(c))
	}
	if c := Chunk("d.md", "# Title\n\njust prose, no headings\n"); len(c) != 0 {
		t.Errorf("no ## blocks -> %d chunks, want 0", len(c))
	}
}

func TestExcluded(t *testing.T) {
	pats := []string{"reflections/**", ".journal/**", "*.tmp.md"}
	yes := []string{"reflections/2026-W23.md", "reflections", ".journal/index/journal.db", "notes/x.tmp.md"}
	no := []string{"daily/2026/06/x.md", "projects/canton/_index.md", "reflectionsX/y.md"}
	for _, p := range yes {
		if !Excluded(p, pats) {
			t.Errorf("Excluded(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if Excluded(p, pats) {
			t.Errorf("Excluded(%q) = true, want false", p)
		}
	}
}

func TestWalkHonorsExcludesAndSince(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string, mod time.Time) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if !mod.IsZero() {
			_ = os.Chtimes(p, mod, mod)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	write("daily/2026/06/a.md", "## 09:00\nx", time.Time{})
	write("daily/2026/06/old.md", "## 09:00\nx", old)
	write("reflections/r.md", "## 09:00\nx", time.Time{})
	write(".journal/index/note.md", "## 09:00\nx", time.Time{})
	write("notes.txt", "not markdown", time.Time{})

	files, err := Walk(root, []string{"reflections/**", ".journal/**"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.RelPath] = true
	}
	if !got["daily/2026/06/a.md"] || !got["daily/2026/06/old.md"] {
		t.Errorf("missing expected daily files: %v", got)
	}
	if got["reflections/r.md"] || got[".journal/index/note.md"] {
		t.Errorf("excluded files were walked: %v", got)
	}
	if got["notes.txt"] {
		t.Error("non-markdown file was walked")
	}

	// --since filter: only recently modified files.
	recent, err := Walk(root, []string{"reflections/**", ".journal/**"}, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range recent {
		if f.RelPath == "daily/2026/06/old.md" {
			t.Error("--since should have excluded the old file")
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
