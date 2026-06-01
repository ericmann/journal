package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/ericmann/journal/internal/embed"
)

// fakeChecker satisfies ollamaChecker: canned tags + a fake embedder whose
// vector length is `dim` (so embed_dim probing can be exercised).
type fakeChecker struct {
	tags []string
	err  error
	dim  int
}

func (f fakeChecker) Tags(context.Context) ([]string, error) { return f.tags, f.err }

func (f fakeChecker) Embed(ctx context.Context, texts []string, instruction string) ([][]float32, error) {
	d := f.dim
	if d == 0 {
		d = 2560 // default-config dimension
	}
	return embed.NewFake(d).Embed(ctx, texts, instruction)
}

func findCheck(rep doctorReport, name string) (check, bool) {
	for _, c := range rep.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return check{}, false
}

func TestDoctorAllHealthy(t *testing.T) {
	cfg := testRepo(t, nil) // default config: reranker disabled, embed_dim 2560
	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)
	if !rep.OK {
		t.Errorf("expected healthy, got: %+v", rep.Checks)
	}
	// Reranker disabled by default -> OK (informational).
	if c, _ := findCheck(rep, "reranker"); !c.OK {
		t.Errorf("disabled reranker should be OK: %+v", c)
	}
	if c, _ := findCheck(rep, "embed_dim"); !c.OK {
		t.Errorf("embed_dim should match: %+v", c)
	}
}

func TestDoctorOllamaDownFails(t *testing.T) {
	cfg := testRepo(t, nil)
	checker := fakeChecker{err: errors.New("connection refused")}
	rep := runDoctor(context.Background(), cfg, checker)
	if rep.OK {
		t.Error("expected failure when Ollama is down")
	}
	c, ok := findCheck(rep, "ollama")
	if !ok || c.OK {
		t.Errorf("ollama check should fail: %+v", c)
	}
	if _, ok := findCheck(rep, "embed_model"); ok {
		t.Error("model checks should be skipped when Ollama is down")
	}
}

func TestDoctorMissingEmbedModelFails(t *testing.T) {
	cfg := testRepo(t, nil)
	checker := fakeChecker{tags: []string{"some-other-model"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)
	if rep.OK {
		t.Error("expected failure when the embed model is missing")
	}
	if c, _ := findCheck(rep, "embed_model"); c.OK {
		t.Errorf("embed_model check should fail: %+v", c)
	}
}

func TestDoctorEmbedDimMismatchFails(t *testing.T) {
	cfg := testRepo(t, nil) // embed_dim 2560
	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: 1024}
	rep := runDoctor(context.Background(), cfg, checker)
	if rep.OK {
		t.Error("expected failure when probed dimension != embed_dim")
	}
	c, _ := findCheck(rep, "embed_dim")
	if c.OK || !contains(c.Detail, "1024") {
		t.Errorf("embed_dim check should report actual dim: %+v", c)
	}
}

func TestDoctorRerankerMissingIsNotFatal(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Reranker = "qwen3:4b" // set but not pulled
	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)
	c, _ := findCheck(rep, "reranker")
	if !c.OK {
		t.Errorf("a missing-but-optional reranker must not fail the verdict: %+v", c)
	}
}

func TestDoctorReportsChunkCount(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00\nnote\n",
	})
	checker := fakeChecker{tags: []string{"qwen3-embedding:4b"}, dim: cfg.EmbedDim}
	rep := runDoctor(context.Background(), cfg, checker)
	c, _ := findCheck(rep, "index")
	if !c.OK {
		t.Errorf("index check should pass: %+v", c)
	}
	if !contains(c.Detail, "1 chunks") {
		t.Errorf("index detail = %q, want chunk count", c.Detail)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
