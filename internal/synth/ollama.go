package synth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ericmann/journal/internal/embed"
)

// Ollama is the local Client backed by Ollama's /api/generate. Synthesis
// prompts and drafts never leave the machine. Transport failures wrap
// embed.ErrUnreachable so callers surface the same install/start hint as the
// embedding path.
type Ollama struct {
	baseURL string
	numCtx  int
	hc      *http.Client
}

// NewOllama returns a client targeting baseURL (e.g. http://localhost:11434).
// numCtx is sent with every request: Ollama's server default is 4096 and it
// truncates the prompt silently, which would quietly gut a synthesis run.
func NewOllama(baseURL string, numCtx int) *Ollama {
	return &Ollama{
		baseURL: baseURL,
		numCtx:  numCtx,
		// Local generation on big prompts is slow on first token (model load +
		// prompt eval), so the budget is generous.
		hc: &http.Client{Timeout: 10 * time.Minute},
	}
}

// WithHTTPClient overrides the HTTP client (used by tests with httptest).
func (o *Ollama) WithHTTPClient(hc *http.Client) *Ollama {
	o.hc = hc
	return o
}

type ollamaGenerateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options"`
}

type ollamaGenerateResponse struct {
	Response        string `json:"response"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

// Complete calls Ollama /api/generate with the request's model and prompt.
func (o *Ollama) Complete(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(ollamaGenerateRequest{
		Model:  req.Model,
		Prompt: req.Prompt,
		Stream: false,
		Options: map[string]any{
			"num_ctx":     o.numCtx,
			"num_predict": req.MaxTokens,
		},
	})
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.hc.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("%w at %s: %v", embed.ErrUnreachable, o.baseURL, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("ollama /api/generate status %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var gr ollamaGenerateResponse
	if err := json.Unmarshal(data, &gr); err != nil {
		return Response{}, err
	}
	return Response{
		Text:         gr.Response,
		InputTokens:  gr.PromptEvalCount,
		OutputTokens: gr.EvalCount,
	}, nil
}
