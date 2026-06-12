package synth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericmann/journal/internal/embed"
)

func TestOllamaCompleteSendsOptionsAndParsesResponse(t *testing.T) {
	var gotReq ollamaGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("path = %q, want /api/generate", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(ollamaGenerateResponse{
			Response:        "a local draft",
			PromptEvalCount: 1200,
			EvalCount:       340,
		})
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, 32768)
	resp, err := c.Complete(context.Background(), Request{Model: "gemma4:12b", MaxTokens: 4096, Prompt: "synthesize"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "a local draft" || resp.InputTokens != 1200 || resp.OutputTokens != 340 {
		t.Errorf("resp = %+v", resp)
	}
	if gotReq.Model != "gemma4:12b" || gotReq.Prompt != "synthesize" || gotReq.Stream {
		t.Errorf("request = %+v", gotReq)
	}
	// num_ctx must always be sent: Ollama's 4096 default silently truncates
	// synthesis prompts.
	if got := gotReq.Options["num_ctx"]; got != float64(32768) {
		t.Errorf("num_ctx = %v, want 32768", got)
	}
	if got := gotReq.Options["num_predict"]; got != float64(4096) {
		t.Errorf("num_predict = %v, want 4096", got)
	}
}

func TestOllamaCompleteErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, 32768)
	if _, err := c.Complete(context.Background(), Request{Model: "nope", Prompt: "x"}); err == nil {
		t.Error("expected error on non-200 status")
	}
}

func TestOllamaCompleteUnreachable(t *testing.T) {
	// A closed port: transport failure must wrap embed.ErrUnreachable so the
	// CLI's "is Ollama running?" hint fires.
	c := NewOllama("http://127.0.0.1:1", 32768)
	_, err := c.Complete(context.Background(), Request{Model: "m", Prompt: "x"})
	if !errors.Is(err, embed.ErrUnreachable) {
		t.Errorf("err = %v, want embed.ErrUnreachable", err)
	}
}
