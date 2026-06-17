package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// OpenAI is an Embedder backed by any OpenAI-compatible /embeddings endpoint
// (OpenAI, Together, a local server, …). It does NOT support reranking — the
// LLM-as-reranker is an Ollama-only path — so Rerank returns an error and search
// falls back to vector-distance order. The API key comes from config (env) and
// is never logged.
type OpenAI struct {
	baseURL string // includes the version path, e.g. https://api.openai.com/v1
	apiKey  string
	model   string
	hc      *http.Client
}

// NewOpenAI returns an embedder for baseURL authenticating with apiKey using the
// given embedding model.
func NewOpenAI(baseURL, apiKey, model string) *OpenAI {
	return &OpenAI{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		hc:      &http.Client{Timeout: 60 * time.Second},
	}
}

// WithHTTPClient overrides the HTTP client and base URL (used by tests).
func (o *OpenAI) WithHTTPClient(hc *http.Client, baseURL string) *OpenAI {
	o.hc = hc
	o.baseURL = strings.TrimRight(baseURL, "/")
	return o
}

type openAIEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed calls the OpenAI-compatible /embeddings endpoint. A non-empty
// instruction is prefixed to each text (queries pass a retrieval instruction;
// documents pass "").
func (o *OpenAI) Embed(ctx context.Context, texts []string, instruction string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if o.apiKey == "" {
		return nil, fmt.Errorf("embed_provider is \"openai\" but no API key — set OPENAI_API_KEY in the environment")
	}
	input := texts
	if instruction != "" {
		input = make([]string, len(texts))
		for i, t := range texts {
			input[i] = instruction + "\n" + t
		}
	}
	body, err := json.Marshal(openAIEmbedRequest{Model: o.model, Input: input})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// Never include the key in errors; only the response body.
		return nil, fmt.Errorf("openai embed: status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	var er openAIEmbedResponse
	if err := json.Unmarshal(data, &er); err != nil {
		return nil, err
	}
	if len(er.Data) != len(texts) {
		return nil, fmt.Errorf("openai embed: got %d embeddings for %d texts", len(er.Data), len(texts))
	}
	// Order by the response index so vectors line up with inputs.
	sort.Slice(er.Data, func(i, j int) bool { return er.Data[i].Index < er.Data[j].Index })
	out := make([][]float32, len(er.Data))
	for i := range er.Data {
		out[i] = er.Data[i].Embedding
	}
	return out, nil
}

// errRerankUnsupported signals that this provider has no rerank path, so the
// caller should fall back to vector-distance order.
var errRerankUnsupported = errors.New("rerank not supported by the openai embed provider (uses vector-distance order)")

// Rerank is unsupported for OpenAI-compatible embedders; returning an error
// makes search fall back to vector-distance order.
func (o *OpenAI) Rerank(ctx context.Context, query string, docs []string) ([]float32, error) {
	return nil, errRerankUnsupported
}
