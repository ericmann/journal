package embed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFakeIsDeterministicAndNormalized(t *testing.T) {
	f := NewFake(16)
	a, err := f.Embed(context.Background(), []string{"hello world"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := f.Embed(context.Background(), []string{"hello world"}, "")
	if len(a[0]) != 16 {
		t.Fatalf("dim = %d, want 16", len(a[0]))
	}
	for i := range a[0] {
		if a[0][i] != b[0][i] {
			t.Fatalf("fake not deterministic at %d", i)
		}
	}
	// Different text -> different vector.
	c, _ := f.Embed(context.Background(), []string{"different"}, "")
	same := true
	for i := range a[0] {
		if a[0][i] != c[0][i] {
			same = false
		}
	}
	if same {
		t.Error("different texts produced identical vectors")
	}
}

func TestFakeCountsEmbedTexts(t *testing.T) {
	f := NewFake(8)
	_, _ = f.Embed(context.Background(), []string{"a", "b", "c"}, "")
	_, _ = f.Embed(context.Background(), []string{"d"}, "")
	if f.EmbedTexts != 4 {
		t.Errorf("EmbedTexts = %d, want 4", f.EmbedTexts)
	}
}

func TestFakeRerankByOverlap(t *testing.T) {
	f := NewFake(8)
	scores, err := f.Rerank(context.Background(), "litellm fallback routing",
		[]string{
			"the litellm fallback routing is broken", // 3/3 tokens
			"unrelated note about taxes",             // 0/3
			"routing works",                          // 1/3
		})
	if err != nil {
		t.Fatal(err)
	}
	if !(scores[0] > scores[2] && scores[2] > scores[1]) {
		t.Errorf("rerank order wrong: %v", scores)
	}
}

func TestOllamaEmbedAppliesInstructionAndParses(t *testing.T) {
	var gotReq embedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotReq)
		_ = json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{{1, 2, 3}, {4, 5, 6}}})
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "embed-model", "rerank-model")
	out, err := c.Embed(context.Background(), []string{"query one", "query two"}, "Represent this query:")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0][0] != 1 || out[1][2] != 6 {
		t.Errorf("embeddings parsed wrong: %v", out)
	}
	if gotReq.Model != "embed-model" {
		t.Errorf("model = %q", gotReq.Model)
	}
	if !strings.HasPrefix(gotReq.Input[0], "Represent this query:\n") {
		t.Errorf("instruction not applied: %q", gotReq.Input[0])
	}
}

func TestOllamaEmbedMismatchCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{{1}}}) // only 1
	}))
	defer srv.Close()
	c := NewOllama(srv.URL, "m", "r")
	if _, err := c.Embed(context.Background(), []string{"a", "b"}, ""); err == nil {
		t.Error("expected mismatch error")
	}
}

func TestOllamaRerankParsesScores(t *testing.T) {
	// Echo a score derived from the document so we can assert parsing/order.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req generateRequest
		_ = json.Unmarshal(body, &req)
		score := "0"
		if strings.Contains(req.Prompt, "Document: highly relevant") {
			score = "9"
		} else if strings.Contains(req.Prompt, "Document: somewhat") {
			score = "5"
		}
		_ = json.NewEncoder(w).Encode(generateResponse{Response: score})
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "m", "r")
	scores, err := c.Rerank(context.Background(), "q", []string{"highly relevant", "somewhat", "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if scores[0] != 0.9 || scores[1] != 0.5 || scores[2] != 0 {
		t.Errorf("scores = %v, want [0.9 0.5 0]", scores)
	}
}

func TestOllamaRetriesThenSucceeds(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{{1}}})
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "m", "r")
	if _, err := c.Embed(context.Background(), []string{"x"}, ""); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestOllamaGivesUpAfterRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewOllama(srv.URL, "m", "r")
	if _, err := c.Embed(context.Background(), []string{"x"}, ""); err == nil {
		t.Error("expected error after exhausting retries")
	}
}
