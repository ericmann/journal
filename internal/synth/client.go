// Package synth assembles prompts from gathered notes and runs synthesis jobs
// (weekly reflection draft, decision rollup, stale-thread surfacing) against the
// Anthropic API. Prompt assembly is pure and golden-file tested; the API client
// sits behind an interface with a deterministic fake so tests run without a
// network. The API key is read from the environment only and is never logged.
package synth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Request is a synthesis completion request.
type Request struct {
	Model     string
	MaxTokens int
	Prompt    string
}

// Response is a synthesis completion result.
type Response struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

// Client completes a synthesis prompt. Implementations: the Anthropic HTTP
// client and a deterministic Fake.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Anthropic is the HTTP Client backed by the Anthropic Messages API.
type Anthropic struct {
	apiKey  string // from env only; never logged
	baseURL string
	version string
	hc      *http.Client
}

// NewAnthropic constructs a client with the given API key (read from the
// environment by the caller). The key is never logged or persisted.
func NewAnthropic(apiKey string) *Anthropic {
	return &Anthropic{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com",
		version: "2023-06-01",
		hc:      &http.Client{Timeout: 120 * time.Second},
	}
}

// WithHTTPClient overrides the HTTP client and base URL (used by tests).
func (a *Anthropic) WithHTTPClient(hc *http.Client, baseURL string) *Anthropic {
	a.hc = hc
	a.baseURL = baseURL
	return a
}

type messagesRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []messageContent `json:"messages"`
}

type messageContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Complete calls the Anthropic Messages API.
func (a *Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(messagesRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  []messageContent{{Role: "user", Content: req.Prompt}},
	})
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", a.version)

	resp, err := a.hc.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// Note: never include the API key in errors; only the response body.
		return Response{}, fmt.Errorf("anthropic API status %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var mr messagesResponse
	if err := json.Unmarshal(data, &mr); err != nil {
		return Response{}, err
	}
	var sb bytes.Buffer
	for _, c := range mr.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return Response{
		Text:         sb.String(),
		InputTokens:  mr.Usage.InputTokens,
		OutputTokens: mr.Usage.OutputTokens,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Fake is a deterministic Client for tests. It records the last prompt and
// returns a canned reply.
type Fake struct {
	Reply     string
	LastReq   Request
	CallCount int
	ForcedErr error
}

// Complete records the request and returns the canned reply (or ForcedErr).
func (f *Fake) Complete(_ context.Context, req Request) (Response, error) {
	f.CallCount++
	f.LastReq = req
	if f.ForcedErr != nil {
		return Response{}, f.ForcedErr
	}
	reply := f.Reply
	if reply == "" {
		reply = "FAKE SYNTH OUTPUT"
	}
	return Response{Text: reply, InputTokens: 100, OutputTokens: 50}, nil
}
