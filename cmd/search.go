package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

// candidateN is how many nearest chunks the vector search fetches before
// reranking down to k. Brute-force KNN at this scale is cheap.
const candidateN = 50

var (
	searchK       int
	searchTags    []string
	searchProject string
	searchSince   string
	searchJSON    bool
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Semantic search over notes (embed → vector KNN → rerank)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		results, err := func() ([]Result, error) {
			cfg, err := loadConfig()
			if err != nil {
				return nil, err
			}
			since, err := parseSince(searchSince)
			if err != nil {
				return nil, err
			}
			f := store.Filter{Tags: searchTags, Project: searchProject}
			if since > 0 {
				f.Since = now().Add(-since)
			}
			return runSearch(cmd.Context(), cfg, newEmbedder(cfg), strings.Join(args, " "), searchK, f)
		}()
		if err != nil {
			return renderError(out, err, searchJSON)
		}
		return renderResults(out, results, searchJSON)
	},
}

// runSearch embeds the query (with the retrieval instruction), fetches the
// nearest candidates, reranks them, and returns the top k as results. If
// reranking fails it falls back to vector-distance order so search still works.
func runSearch(ctx context.Context, cfg *config.Config, e embed.Embedder, query string, k int, f store.Filter) ([]Result, error) {
	if k <= 0 {
		k = 5
	}
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	qvecs, err := e.Embed(ctx, []string{query}, cfg.RetrievalInstruction)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}
	if len(qvecs) != 1 {
		return nil, fmt.Errorf("embedder returned %d query vectors", len(qvecs))
	}

	cands, err := s.KNN(ctx, qvecs[0], candidateN, f)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return []Result{}, nil
	}

	scored := rerankOrDistance(ctx, e, query, cands)
	if len(scored) > k {
		scored = scored[:k]
	}
	results := make([]Result, len(scored))
	for i, sc := range scored {
		results[i] = chunkToResult(sc.chunk, sc.score)
	}
	return results, nil
}

type scoredChunk struct {
	chunk store.Chunk
	score float64
}

// rerankOrDistance reranks candidates by the reranker model, falling back to
// vector-distance order (with a distance-derived score) if reranking errors.
func rerankOrDistance(ctx context.Context, e embed.Embedder, query string, cands []store.Candidate) []scoredChunk {
	docs := make([]string, len(cands))
	for i, c := range cands {
		docs[i] = rerankDoc(c.Chunk)
	}
	scores, err := e.Rerank(ctx, query, docs)
	if err != nil || len(scores) != len(cands) {
		// Fallback: candidates are already in ascending-distance order.
		out := make([]scoredChunk, len(cands))
		for i, c := range cands {
			out[i] = scoredChunk{chunk: c.Chunk, score: 1.0 / (1.0 + c.Distance)}
		}
		return out
	}
	out := make([]scoredChunk, len(cands))
	for i, c := range cands {
		out[i] = scoredChunk{chunk: c.Chunk, score: float64(scores[i])}
	}
	// Stable sort by score desc so ties keep KNN (distance) order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

func rerankDoc(c store.Chunk) string {
	if c.Heading == "" {
		return c.Body
	}
	return c.Heading + "\n" + c.Body
}

func init() {
	searchCmd.Flags().IntVar(&searchK, "k", 5, "number of results to return")
	searchCmd.Flags().StringArrayVar(&searchTags, "tag", nil, "filter to chunks with this tag (repeatable)")
	searchCmd.Flags().StringVar(&searchProject, "project", "", "filter to a project slug")
	searchCmd.Flags().StringVar(&searchSince, "since", "", "only chunks created within this window (e.g. 2w, 14d)")
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "emit JSON ({results:[...]}) instead of text")
	rootCmd.AddCommand(searchCmd)
}
