package synth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI is a Client backed by any OpenAI-compatible Chat Completions endpoint
// (OpenAI, OpenRouter, Groq, Together, a local server, …). It POSTs to
// {baseURL}/chat/completions with a Bearer key. The key comes from the
// environment via config and is never logged.
type OpenAI struct {
	baseURL string // includes the version path, e.g. https://openrouter.ai/api/v1
	apiKey  string
	hc      *http.Client
}

// NewOpenAI returns a client for baseURL (must include the version path, e.g.
// https://api.openai.com/v1) authenticating with apiKey.
func NewOpenAI(baseURL, apiKey string) *OpenAI {
	return &OpenAI{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		// Remote providers vary; a generous budget covers a slow free tier.
		hc: &http.Client{Timeout: 5 * time.Minute},
	}
}

// WithHTTPClient overrides the HTTP client and base URL (used by tests).
func (o *OpenAI) WithHTTPClient(hc *http.Client, baseURL string) *OpenAI {
	o.hc = hc
	o.baseURL = strings.TrimRight(baseURL, "/")
	return o
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Complete calls the OpenAI-compatible /chat/completions endpoint.
func (o *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(chatRequest{
		Model:     req.Model,
		Messages:  []chatMessage{{Role: "user", Content: req.Prompt}},
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.hc.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// Never include the key in errors; only the response body.
		return Response{}, fmt.Errorf("openai API status %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return Response{}, err
	}
	if len(cr.Choices) == 0 {
		return Response{}, fmt.Errorf("openai API returned no choices: %s", truncate(string(data), 200))
	}
	return Response{
		Text:         cr.Choices[0].Message.Content,
		InputTokens:  cr.Usage.PromptTokens,
		OutputTokens: cr.Usage.CompletionTokens,
	}, nil
}
