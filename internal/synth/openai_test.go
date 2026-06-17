package synth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompleteSendsRequestAndParses(t *testing.T) {
	var gotAuth, gotPath string
	var gotReq chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "a remote draft"}}},
			"usage":   map[string]any{"prompt_tokens": 321, "completion_tokens": 88},
		})
	}))
	defer srv.Close()

	c := NewOpenAI(srv.URL, "test-key")
	resp, err := c.Complete(context.Background(), Request{Model: "google/gemma-3-27b-it:free", MaxTokens: 4096, Prompt: "synthesize this"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "a remote draft" || resp.InputTokens != 321 || resp.OutputTokens != 88 {
		t.Errorf("resp = %+v", resp)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth = %q, want Bearer test-key", gotAuth)
	}
	if gotReq.Model != "google/gemma-3-27b-it:free" || len(gotReq.Messages) != 1 || gotReq.Messages[0].Content != "synthesize this" || gotReq.MaxTokens != 4096 {
		t.Errorf("request = %+v", gotReq)
	}
}

func TestOpenAICompleteErrorStatusAndNoChoices(t *testing.T) {
	// Non-200 → error.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid model"}}`, http.StatusBadRequest)
	}))
	defer bad.Close()
	if _, err := NewOpenAI(bad.URL, "k").Complete(context.Background(), Request{Model: "x", Prompt: "y"}); err == nil {
		t.Error("expected error on 400")
	}

	// 200 but empty choices → error (don't silently return "").
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer empty.Close()
	if _, err := NewOpenAI(empty.URL, "k").Complete(context.Background(), Request{Model: "x", Prompt: "y"}); err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Errorf("expected no-choices error, got %v", err)
	}
}

// The base URL's trailing slash must not produce a double slash in the path.
func TestOpenAINormalizesBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	if _, err := NewOpenAI(srv.URL+"/", "k").Complete(context.Background(), Request{Model: "m", Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions (no double slash)", gotPath)
	}
}
