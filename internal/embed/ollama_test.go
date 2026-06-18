package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
			w.Write([]byte(`{"error":"do embedding request: Post \"http://127.0.0.1:12345/v1/embeddings\": EOF"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(okBody)
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
		w.Write([]byte(`{"error":"model \"nomodel\" not found"}`))
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
		w.Write([]byte(`{"error":"do embedding request: Post \"http://127.0.0.1:12345/v1/embeddings\": EOF"}`))
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
