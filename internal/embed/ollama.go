package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUnreachable indicates the Ollama service could not be contacted (e.g. it is
// not running). Callers can errors.Is against it to print an install/start hint
// instead of a raw transport error.
var ErrUnreachable = errors.New("ollama unreachable")

// Ollama is the HTTP Embedder backed by a local Ollama service. It talks to
// /api/embed for embeddings and scores rerank candidates via /api/generate with
// the reranker model (LLM-as-reranker), since Ollama has no dedicated rerank
// endpoint. See docs/DECISIONS.md.
type Ollama struct {
	baseURL     string
	embedModel  string
	rerankModel string
	hc          *http.Client

	// maxRetries bounds retries for transport-level and 5xx failures.
	maxRetries int
	// transientBudget is the wall-clock window for retrying transient
	// embed-runner crashes (400 with EOF / "do embedding request" body). Sized
	// to outlast a model reload — a SIGTRAP'd runner can take 10–30 s to
	// restart on Apple Silicon. Tests shrink this to keep the suite fast.
	transientBudget time.Duration
	// rerankWorkers bounds concurrent rerank generate calls.
	rerankWorkers int
}

// NewOllama returns a client targeting baseURL (e.g. http://localhost:11434).
func NewOllama(baseURL, embedModel, rerankModel string) *Ollama {
	return &Ollama{
		baseURL:         baseURL,
		embedModel:      embedModel,
		rerankModel:     rerankModel,
		hc:              &http.Client{Timeout: 60 * time.Second},
		maxRetries:      3,
		transientBudget: 45 * time.Second,
		rerankWorkers:   4,
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

var (
	// scoreFractionRe matches explicit "N/10" relevance fractions (e.g. "8/10", "7.5/10").
	scoreFractionRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*/\s*10\b`)
	// scoreLabelRe matches labelled scores ("Score: 7", "Relevance: 8.5").
	scoreLabelRe = regexp.MustCompile(`(?i)(?:score|relevance|rating)[:\s]+(\d+(?:\.\d+)?)`)
	// scoreRe matches standalone non-negative numbers used as a last-resort fallback.
	scoreRe = regexp.MustCompile(`\b(\d+(?:\.\d+)?)\b`)
)

// parseScore extracts a [0,1] relevance score from a model response.
// It tries (in order): an explicit "N/10" fraction, a labelled form ("Score: N"),
// then the last standalone number in [0,10]. Returns 0 when no plausible score
// is found so the caller falls back to vector-distance order.
func parseScore(s string) float32 {
	s = strings.TrimSpace(s)
	if m := scoreFractionRe.FindStringSubmatch(s); m != nil {
		if v, err := strconv.ParseFloat(m[1], 32); err == nil {
			return clampUnit(float32(v) / 10.0)
		}
	}
	if m := scoreLabelRe.FindStringSubmatch(s); m != nil {
		if v, err := strconv.ParseFloat(m[1], 32); err == nil {
			return clampUnit(float32(v) / 10.0)
		}
	}
	// Fallback: last number in [0,10] — models often trail the response with the score.
	// Skip digits immediately preceded by '-' to avoid parsing "-1" as 1.
	idxs := scoreRe.FindAllStringSubmatchIndex(s, -1)
	for i := len(idxs) - 1; i >= 0; i-- {
		start := idxs[i][2] // start of capture group 1
		if start > 0 && s[start-1] == '-' {
			continue
		}
		m := s[idxs[i][2]:idxs[i][3]]
		if v, err := strconv.ParseFloat(m, 32); err == nil && v >= 0 && v <= 10 {
			return clampUnit(float32(v) / 10.0)
		}
	}
	return 0
}

func clampUnit(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

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
		"You are a relevance judge.\n"+
			"Score how well the DOCUMENT addresses the QUERY on a scale of 0 to 10.\n\n"+
			"Rubric:\n"+
			"  0  – completely unrelated\n"+
			"  5  – on-topic but does not directly answer the query\n"+
			"  10 – directly and completely addresses the query\n\n"+
			"QUERY: %s\n\n"+
			"DOCUMENT:\n%s\n\n"+
			"Respond with a single integer (0–10) and nothing else.",
		query, doc)
	body, err := json.Marshal(generateRequest{
		Model:   o.rerankModel,
		Prompt:  prompt,
		Stream:  false,
		Options: map[string]any{"temperature": 0, "num_predict": 16},
	})
	if err != nil {
		return 0
	}
	var resp generateResponse
	if err := o.postJSON(ctx, "/api/generate", body, &resp); err != nil {
		return 0
	}
	return parseScore(resp.Response)
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
		return nil, fmt.Errorf("%w at %s: %v", ErrUnreachable, o.baseURL, err)
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

// postJSON POSTs body to path and decodes the JSON response into out.
//
// Retry strategy:
//   - Transport-level and 5xx failures retry up to maxRetries times.
//   - Transient embed-runner crashes (400 with EOF / "do embedding request" body)
//     retry across the full transientBudget window so a model reload has time to
//     complete. The per-sleep backoff is capped and jittered; context cancellation
//     aborts the wait promptly.
//   - Non-retryable 4xx (model not found, bad dimensions) fail immediately.
func (o *Ollama) postJSON(ctx context.Context, path string, body []byte, out any) error {
	transientDeadline := time.Now().Add(o.transientBudget)
	var lastErr error
	retries := 0

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := o.hc.Do(req)
		if err != nil {
			// Transport-level failure (connection refused, no such host, timeout).
			lastErr = fmt.Errorf("%w at %s: %v", ErrUnreachable, o.baseURL, err)
			if retries >= o.maxRetries {
				break
			}
			retries++
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(cappedBackoff(retries)):
			}
			continue
		}

		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("ollama %s: status %d: %s", path, resp.StatusCode, truncate(string(data), 200))
			if retries >= o.maxRetries {
				break
			}
			retries++
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(cappedBackoff(retries)):
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			bodyStr := string(data)
			if isTransientEmbedFailure(resp.StatusCode, bodyStr) {
				lastErr = fmt.Errorf("ollama %s: status %d: %s", path, resp.StatusCode, truncate(bodyStr, 200))
				remaining := time.Until(transientDeadline)
				if remaining <= 0 {
					break
				}
				retries++
				sleep := cappedBackoff(retries)
				if sleep > remaining {
					sleep = remaining
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(sleep):
				}
				continue
			}
			return fmt.Errorf("ollama %s: status %d: %s", path, resp.StatusCode, truncate(bodyStr, 200))
		}

		if readErr != nil {
			return readErr
		}
		return json.Unmarshal(data, out)
	}

	return fmt.Errorf("after %d retries: %w", retries, lastErr)
}

// isTransientEmbedFailure reports whether a non-200 response is a transient
// embed-server crash (e.g. Ollama's llama-server child died mid-request) rather
// than a genuine client error. Only called for 4xx responses; the signatures
// below appear in real Ollama EOF crashes and must not match payload errors like
// "model not found" or "invalid dimensions".
func isTransientEmbedFailure(status int, body string) bool {
	if status != http.StatusBadRequest {
		return false
	}
	return strings.Contains(body, "EOF") ||
		strings.Contains(body, "connection refused") ||
		strings.Contains(body, "do embedding request")
}

// cappedBackoff returns the sleep duration before retry n (1-indexed).
// It is n²×100ms, capped at 5s, with ±25% jitter so repeated chunks do not
// resync onto the same cadence. math/rand global funcs are goroutine-safe in Go 1.20+.
func cappedBackoff(n int) time.Duration {
	const maxBackoff = 5 * time.Second
	d := time.Duration(n*n) * 100 * time.Millisecond
	if d > maxBackoff || d < 0 { // d < 0 catches int overflow on large n
		d = maxBackoff
	}
	// ±25% jitter: rand in [0, d/2], subtract d/4 → [-d/4, +d/4]
	quarter := int64(d) / 4
	jitter := time.Duration(rand.Int63n(quarter+1)*2) - time.Duration(quarter)
	return d + jitter
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
