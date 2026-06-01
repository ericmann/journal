package synth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicCompleteParsesAndSendsHeaders(t *testing.T) {
	var gotKey, gotVersion, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"weekly draft here"}],"usage":{"input_tokens":1200,"output_tokens":340}}`))
	}))
	defer srv.Close()

	c := NewAnthropic("sk-secret-key").WithHTTPClient(srv.Client(), srv.URL)
	resp, err := c.Complete(context.Background(), Request{Model: "claude-test", MaxTokens: 1024, Prompt: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "weekly draft here" {
		t.Errorf("text = %q", resp.Text)
	}
	if resp.InputTokens != 1200 || resp.OutputTokens != 340 {
		t.Errorf("tokens = %d/%d", resp.InputTokens, resp.OutputTokens)
	}
	if gotKey != "sk-secret-key" {
		t.Errorf("x-api-key = %q", gotKey)
	}
	if gotVersion == "" {
		t.Error("missing anthropic-version header")
	}
	var sent messagesRequest
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Model != "claude-test" || sent.MaxTokens != 1024 || len(sent.Messages) != 1 {
		t.Errorf("request body wrong: %+v", sent)
	}
}

func TestAnthropicErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid"}}`))
	}))
	defer srv.Close()

	c := NewAnthropic("sk-super-secret").WithHTTPClient(srv.Client(), srv.URL)
	_, err := c.Complete(context.Background(), Request{Model: "m", MaxTokens: 10, Prompt: "x"})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if strings.Contains(err.Error(), "sk-super-secret") {
		t.Errorf("error leaked the API key: %v", err)
	}
}

func TestFakeRecordsRequest(t *testing.T) {
	f := &Fake{Reply: "canned"}
	resp, err := f.Complete(context.Background(), Request{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "canned" || f.LastReq.Prompt != "p" || f.CallCount != 1 {
		t.Errorf("fake state wrong: resp=%q last=%+v calls=%d", resp.Text, f.LastReq, f.CallCount)
	}
}
