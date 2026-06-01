package cmd

import (
	"context"
	"errors"
	"testing"
)

type fakeLister struct {
	tags []string
	err  error
}

func (f fakeLister) Tags(context.Context) ([]string, error) { return f.tags, f.err }

func findCheck(rep doctorReport, name string) (check, bool) {
	for _, c := range rep.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return check{}, false
}

func TestDoctorAllHealthy(t *testing.T) {
	cfg := testRepo(t, nil)
	lister := fakeLister{tags: []string{"qwen3-embedding:4b", "qwen3-reranker:latest"}}
	rep := runDoctor(context.Background(), cfg, lister)
	if !rep.OK {
		t.Errorf("expected healthy, got: %+v", rep.Checks)
	}
	if c, _ := findCheck(rep, "reranker"); !c.OK {
		t.Errorf("reranker check should pass with :latest tolerance: %+v", c)
	}
}

func TestDoctorOllamaDownFails(t *testing.T) {
	cfg := testRepo(t, nil)
	lister := fakeLister{err: errors.New("connection refused")}
	rep := runDoctor(context.Background(), cfg, lister)
	if rep.OK {
		t.Error("expected failure when Ollama is down")
	}
	c, ok := findCheck(rep, "ollama")
	if !ok || c.OK {
		t.Errorf("ollama check should fail: %+v", c)
	}
	// Model checks are skipped when Ollama is unreachable.
	if _, ok := findCheck(rep, "embed_model"); ok {
		t.Error("model checks should be skipped when Ollama is down")
	}
}

func TestDoctorMissingModelFails(t *testing.T) {
	cfg := testRepo(t, nil)
	lister := fakeLister{tags: []string{"qwen3-embedding:4b"}} // reranker missing
	rep := runDoctor(context.Background(), cfg, lister)
	if rep.OK {
		t.Error("expected failure when reranker model missing")
	}
	c, _ := findCheck(rep, "reranker")
	if c.OK {
		t.Errorf("reranker check should fail: %+v", c)
	}
}

func TestDoctorReportsChunkCount(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00\nnote\n",
	})
	lister := fakeLister{tags: []string{"qwen3-embedding:4b", "qwen3-reranker"}}
	rep := runDoctor(context.Background(), cfg, lister)
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
