package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestParseScore covers score extraction from model responses: plain integers,
// floats, N/10 fractions, labelled forms, prose-prefixed responses, and the
// safe-default-to-zero cases (empty, no number, out-of-range).
func TestParseScore(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  float32
	}{
		{"plain integer", "7", 0.7},
		{"plain float", "7.5", 0.75},
		{"zero", "0", 0.0},
		{"max", "10", 1.0},
		{"n/10 fraction", "8/10", 0.8},
		{"n/10 with spaces", "9 / 10", 0.9},
		{"float n/10", "7.5/10", 0.75},
		{"labelled score colon", "Score: 7", 0.7},
		{"labelled relevance", "Relevance: 8", 0.8},
		{"labelled rating", "Rating: 6", 0.6},
		{"prose then number", "The document is highly relevant to the query. 9", 0.9},
		// label wins over the trailing prose number
		{"label beats trailing prose", "Score: 8 — this is a strong match with 5 examples", 0.8},
		// last in-range number wins when no fraction/label
		{"last in-range number", "I considered 3 options and settled on 7", 0.7},
		{"empty string", "", 0.0},
		{"no number", "Excellent relevance!", 0.0},
		// numbers outside [0,10] are skipped by the fallback scan
		{"out of range 15 only", "15", 0.0},
		// negative numbers are not matched by the digit regex
		{"negative response", "-1", 0.0},
		// whitespace-only response
		{"whitespace only", "   ", 0.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseScore(tc.input)
			if got != tc.want {
				t.Errorf("parseScore(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestRerank_Ordering verifies that Rerank returns higher scores for documents
// containing the query keyword than for unrelated documents, producing a
// measurable precision lift over the raw candidate order.
func TestRerank_Ordering(t *testing.T) {
	// relevant doc scores 9, irrelevant doc scores 1 — simulates a capable reranker.
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// If the doc contains the word "relevant", return a high score.
		score := "1"
		if strings.Contains(req.Prompt, "directly relevant content") {
			score = "9"
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"response":"` + score + `"}`))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "embed-model", "rerank-model").WithHTTPClient(srv.Client())
	o.maxRetries = 0

	docs := []string{
		"unrelated content about other topics",
		"directly relevant content that answers the question",
		"another unrelated document",
	}
	scores, err := o.Rerank(context.Background(), "test query", docs)
	if err != nil {
		t.Fatalf("Rerank returned error: %v", err)
	}
	if len(scores) != len(docs) {
		t.Fatalf("expected %d scores, got %d", len(docs), len(scores))
	}
	if callCount.Load() != int32(len(docs)) {
		t.Errorf("expected %d generate calls, got %d", len(docs), callCount.Load())
	}
	// The relevant doc (index 1) must score higher than the unrelated ones.
	if scores[1] <= scores[0] {
		t.Errorf("relevant doc score %v should exceed unrelated doc score %v", scores[1], scores[0])
	}
	if scores[1] <= scores[2] {
		t.Errorf("relevant doc score %v should exceed unrelated doc score %v", scores[1], scores[2])
	}
}

// TestIsTransientEmbedFailure covers the helper directly.
func TestIsTransientEmbedFailure(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "EOF body is transient",
			status: 400,
			body:   `{"error":"do embedding request: Post \"http://127.0.0.1:12345/v1/embeddings\": EOF"}`,
			want:   true,
		},
		{
			name:   "connection refused body is transient",
			status: 400,
			body:   `{"error":"do embedding request: Post \"http://127.0.0.1:12345/v1/embeddings\": connection refused"}`,
			want:   true,
		},
		{
			name:   "do embedding request phrase alone is transient",
			status: 400,
			body:   `{"error":"do embedding request: some other error"}`,
			want:   true,
		},
		{
			name:   "model not found is not transient",
			status: 400,
			body:   `{"error":"model \"nomodel\" not found"}`,
			want:   false,
		},
		{
			name:   "invalid dimensions is not transient",
			status: 400,
			body:   `{"error":"invalid dimensions"}`,
			want:   false,
		},
		{
			name:   "500 with EOF body is not classified by this helper",
			status: 500,
			body:   `{"error":"EOF"}`,
			want:   false,
		},
		{
			name:   "200 is not transient",
			status: 200,
			body:   ``,
			want:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isTransientEmbedFailure(tc.status, tc.body)
			if got != tc.want {
				t.Errorf("isTransientEmbedFailure(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

// TestEmbed_TransientRetry verifies that a 400 with an EOF-style body is
// retried and that Embed ultimately succeeds when a later attempt returns 200.
func TestEmbed_TransientRetry(t *testing.T) {
	callCount := 0
	okBody, _ := json.Marshal(embedResponse{
		Embeddings: [][]float32{{0.1, 0.2, 0.3}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: simulate transient embed-server crash.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"do embedding request: Post \"http://127.0.0.1:12345/v1/embeddings\": EOF"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(okBody)
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "test-model", "").WithHTTPClient(srv.Client())
	o.maxRetries = 3

	vecs, err := o.Embed(context.Background(), []string{"hello"}, "")
	if err != nil {
		t.Fatalf("Embed returned error after transient failure: %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 calls (1 failure + 1 success), got %d", callCount)
	}
	if len(vecs) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(vecs))
	}
}

// TestEmbed_NonTransientNotRetried verifies that a genuine client-error 400
// (e.g. model not found) fails immediately without retrying.
func TestEmbed_NonTransientNotRetried(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"model \"nomodel\" not found"}`))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "nomodel", "").WithHTTPClient(srv.Client())
	o.maxRetries = 3

	_, err := o.Embed(context.Background(), []string{"hello"}, "")
	if err == nil {
		t.Fatal("expected error for non-transient 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected status 400 in error, got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 call (no retries), got %d", callCount)
	}
}

// TestEmbed_ExhaustedRetries verifies that when all retries are consumed the
// error still surfaces clearly.
func TestEmbed_ExhaustedRetries(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"do embedding request: Post \"http://127.0.0.1:12345/v1/embeddings\": EOF"}`))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "test-model", "").WithHTTPClient(srv.Client())
	o.maxRetries = 2

	_, err := o.Embed(context.Background(), []string{"hello"}, "")
	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}
	if !strings.Contains(err.Error(), "retries") {
		t.Errorf("expected 'retries' in error message, got: %v", err)
	}
	if callCount != 3 { // initial attempt + 2 retries
		t.Errorf("expected 3 calls (1 + maxRetries), got %d", callCount)
	}
}
