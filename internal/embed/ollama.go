package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ollama is the HTTP Embedder backed by a local Ollama service. It talks to
// /api/embed for embeddings and scores rerank candidates via /api/generate with
// the reranker model (LLM-as-reranker), since Ollama has no dedicated rerank
// endpoint. See docs/DECISIONS.md.
type Ollama struct {
	baseURL     string
	embedModel  string
	rerankModel string
	hc          *http.Client

	// maxRetries bounds transient-failure retries (network / 5xx).
	maxRetries int
	// rerankWorkers bounds concurrent rerank generate calls.
	rerankWorkers int
}

// NewOllama returns a client targeting baseURL (e.g. http://localhost:11434).
func NewOllama(baseURL, embedModel, rerankModel string) *Ollama {
	return &Ollama{
		baseURL:       baseURL,
		embedModel:    embedModel,
		rerankModel:   rerankModel,
		hc:            &http.Client{Timeout: 60 * time.Second},
		maxRetries:    3,
		rerankWorkers: 4,
	}
}

// WithHTTPClient overrides the HTTP client (used by tests with httptest).
func (o *Ollama) WithHTTPClient(hc *http.Client) *Ollama {
	o.hc = hc
	return o
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed calls Ollama /api/embed. A non-empty instruction is prefixed to each
// text (queries pass a retrieval instruction; documents pass "").
func (o *Ollama) Embed(ctx context.Context, texts []string, instruction string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	input := texts
	if instruction != "" {
		input = make([]string, len(texts))
		for i, t := range texts {
			input[i] = instruction + "\n" + t
		}
	}
	body, err := json.Marshal(embedRequest{Model: o.embedModel, Input: input})
	if err != nil {
		return nil, err
	}
	var resp embedResponse
	if err := o.postJSON(ctx, "/api/embed", body, &resp); err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed: got %d embeddings for %d texts", len(resp.Embeddings), len(texts))
	}
	return resp.Embeddings, nil
}

type generateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options,omitempty"`
}

type generateResponse struct {
	Response string `json:"response"`
}

var scoreRe = regexp.MustCompile(`-?\d+(\.\d+)?`)

// Rerank scores each doc against query using the reranker model. Scores are in
// [0,1]; on a per-doc failure the score defaults to 0 rather than failing the
// whole call, so the caller can still fall back to vector order.
func (o *Ollama) Rerank(ctx context.Context, query string, docs []string) ([]float32, error) {
	scores := make([]float32, len(docs))
	if len(docs) == 0 {
		return scores, nil
	}
	type job struct{ i int }
	jobs := make(chan job)
	var wg sync.WaitGroup
	workers := o.rerankWorkers
	if workers < 1 {
		workers = 1
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				scores[j.i] = o.scoreOne(ctx, query, docs[j.i])
			}
		}()
	}
	for i := range docs {
		jobs <- job{i}
	}
	close(jobs)
	wg.Wait()
	return scores, nil
}

func (o *Ollama) scoreOne(ctx context.Context, query, doc string) float32 {
	prompt := fmt.Sprintf(
		"Rate how relevant the following document is to the query on a scale of 0 to 10. "+
			"Reply with only the number.\n\nQuery: %s\n\nDocument: %s\n\nRelevance (0-10):",
		query, doc)
	body, err := json.Marshal(generateRequest{
		Model:   o.rerankModel,
		Prompt:  prompt,
		Stream:  false,
		Options: map[string]any{"temperature": 0, "num_predict": 8},
	})
	if err != nil {
		return 0
	}
	var resp generateResponse
	if err := o.postJSON(ctx, "/api/generate", body, &resp); err != nil {
		return 0
	}
	m := scoreRe.FindString(resp.Response)
	if m == "" {
		return 0
	}
	val, err := strconv.ParseFloat(m, 32)
	if err != nil {
		return 0
	}
	score := float32(val) / 10.0
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

type tagsResponse struct {
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
}

// Tags returns the list of model names available in Ollama (from /api/tags).
// It doubles as a reachability check.
func (o *Ollama) Tags(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama unreachable at %s: %w", o.baseURL, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama /api/tags: status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	var tr tagsResponse
	if err := json.Unmarshal(data, &tr); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tr.Models))
	for _, m := range tr.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

// HasModel reports whether want is present among Ollama's models, tolerating a
// missing ":tag" on either side (e.g. "qwen3-reranker" matches
// "qwen3-reranker:latest").
func HasModel(tags []string, want string) bool {
	wantBase, _, _ := strings.Cut(want, ":")
	for _, t := range tags {
		if t == want {
			return true
		}
		tBase, _, _ := strings.Cut(t, ":")
		if tBase == want || t == wantBase || tBase == wantBase {
			return true
		}
	}
	return false
}

// postJSON POSTs body to path and decodes the JSON response into out, with
// bounded retry + backoff on transient errors.
func (o *Ollama) postJSON(ctx context.Context, path string, body []byte, out any) error {
	var lastErr error
	for attempt := 0; attempt <= o.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := o.hc.Do(req)
		if err != nil {
			lastErr = err
			continue // transient: retry
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("ollama %s: status %d: %s", path, resp.StatusCode, truncate(string(data), 200))
			continue // server-side transient: retry
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("ollama %s: status %d: %s", path, resp.StatusCode, truncate(string(data), 200))
		}
		if readErr != nil {
			return readErr
		}
		return json.Unmarshal(data, out)
	}
	return fmt.Errorf("after %d retries: %w", o.maxRetries, lastErr)
}

func backoff(attempt int) time.Duration {
	return time.Duration(attempt*attempt) * 100 * time.Millisecond
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
