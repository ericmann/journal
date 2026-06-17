package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedSendsModelInputAndOrdersByIndex(t *testing.T) {
	var gotAuth, gotPath string
	var gotReq openAIEmbedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		// Return out of order to prove the client sorts by index.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 1, "embedding": []float32{0.3, 0.4}},
				{"index": 0, "embedding": []float32{0.1, 0.2}},
			},
		})
	}))
	defer srv.Close()

	c := NewOpenAI(srv.URL, "test-key", "text-embedding-3-small")
	vecs, err := c.Embed(context.Background(), []string{"first", "second"}, "Represent this query:")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/embeddings" || gotAuth != "Bearer test-key" {
		t.Errorf("path/auth = %q / %q", gotPath, gotAuth)
	}
	if gotReq.Model != "text-embedding-3-small" || len(gotReq.Input) != 2 {
		t.Errorf("request = %+v", gotReq)
	}
	// Instruction is prefixed to each input.
	if gotReq.Input[0] != "Represent this query:\nfirst" {
		t.Errorf("input[0] not instruction-prefixed: %q", gotReq.Input[0])
	}
	// Vectors come back in input order despite the shuffled response.
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][0] != 0.3 {
		t.Errorf("vectors not ordered by index: %v", vecs)
	}
}

func TestOpenAIEmbedRequiresKey(t *testing.T) {
	c := NewOpenAI("https://api.openai.com/v1", "", "text-embedding-3-small")
	if _, err := c.Embed(context.Background(), []string{"x"}, ""); err == nil {
		t.Error("expected error when OPENAI_API_KEY is empty")
	}
}

func TestOpenAIRerankUnsupported(t *testing.T) {
	c := NewOpenAI("https://api.openai.com/v1", "k", "m")
	if _, err := c.Rerank(context.Background(), "q", []string{"d"}); err == nil {
		t.Error("expected Rerank to be unsupported (so search falls back to distance order)")
	}
}

func TestOpenAIEmbedCountMismatchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]}]}`)) // 1 vec for 2 inputs
	}))
	defer srv.Close()
	c := NewOpenAI(srv.URL, "k", "m")
	if _, err := c.Embed(context.Background(), []string{"a", "b"}, ""); err == nil {
		t.Error("expected count-mismatch error")
	}
}
