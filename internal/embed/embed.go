// Package embed defines the embedding + reranking boundary to the local Ollama
// service. The Embedder interface is implemented by a real HTTP client and by a
// deterministic Fake so that all unit and integration tests run without a
// network.
package embed

import "context"

// Embedder produces embedding vectors for documents/queries and reranks
// candidate documents against a query. Implementations must be safe for the
// single-threaded CLI use; the HTTP client is also safe for concurrent calls.
type Embedder interface {
	// Embed returns one vector per input text. When instruction is non-empty it
	// is applied as a retrieval-instruction prefix to each text (used for
	// queries; documents are embedded with an empty instruction).
	Embed(ctx context.Context, texts []string, instruction string) ([][]float32, error)

	// Rerank scores each doc for relevance to query; higher is more relevant.
	// The returned slice is parallel to docs.
	Rerank(ctx context.Context, query string, docs []string) ([]float32, error)
}
