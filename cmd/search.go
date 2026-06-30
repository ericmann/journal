package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/synth"
	"github.com/spf13/cobra"
)

// candidateN is how many nearest chunks the vector search fetches before
// reranking down to k. Brute-force KNN at this scale is cheap.
const candidateN = 50

var (
	searchK        int
	searchTags     []string
	searchProject  string
	searchSince    string
	searchJSON     bool
	searchAnswer   bool
	searchNoAnswer bool
	searchSources  []string
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Semantic search over notes (embed → vector KNN → rerank)",
	Long: "search embeds the query, runs a vector KNN over the index, optionally reranks,\n" +
		"and prints the best matches with citations. When ANTHROPIC_API_KEY is set it also\n" +
		"generates a grounded answer to the question (configured synth_model) above the raw\n" +
		"hits — disable with --no-answer, or force with --answer. --json prints results only.",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		query := strings.Join(args, " ")
		cfg, err := loadConfig()
		if err != nil {
			return renderError(out, err, searchJSON)
		}
		scored, err := func() ([]scoredChunk, error) {
			since, err := parseSince(searchSince)
			if err != nil {
				return nil, err
			}
			srcs, err := parseSourceFilter(searchSources)
			if err != nil {
				return nil, err
			}
			f := store.Filter{Tags: searchTags, Project: searchProject, Sources: srcs}
			if since > 0 {
				f.Since = now().Add(-since)
			}
			return searchChunks(cmd.Context(), cfg, newEmbedder(cfg), query, searchK, f)
		}()
		if err != nil {
			return renderError(out, hintOllama(cfg, err), searchJSON)
		}

		// Optional AI answer (text mode only), grounded in the retrieved chunks.
		client, available, unavailableReason := answerClient(cfg)
		do, unavailable := wantAnswer(searchJSON, searchAnswer, searchNoAnswer, available)
		if unavailable {
			return renderError(out, unavailableReason, searchJSON)
		}
		if do && len(scored) > 0 {
			chunks := make([]store.Chunk, len(scored))
			for i, sc := range scored {
				chunks[i] = sc.chunk
			}
			ans, aerr := answerQuery(cmd.Context(), client, cfg.ActiveSynthModel(), cfg.SynthMaxTokens, query, chunks)
			if aerr != nil {
				fmt.Fprintf(out, "(AI answer unavailable: %v)\n\n", aerr) // non-fatal; raw results still shown
			} else {
				renderMarkdown(out, ans)
				fmt.Fprintf(out, "\n─── sources ───\n\n")
			}
		}
		return renderResults(out, resultsFromScored(scored), searchJSON)
	},
}

// parseSourceFilter maps source selectors ("notes"|"meetings"|"transcript"|"all")
// to store source values. Repeatable: multiple values are OR-ed. nil/empty = any.
func parseSourceFilter(ss []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		v, err := parseSingleSource(s)
		if err != nil {
			return nil, err
		}
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, nil
}

func parseSingleSource(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all", "any":
		return "", nil
	case "notes", "note":
		return store.SourceNote, nil
	case "transcript", "transcripts", "meetings", "meeting":
		return store.SourceTranscript, nil
	case "voice", "log", "logs":
		return store.SourceVoice, nil
	default:
		return "", fmt.Errorf("invalid source %q (want notes|transcript|meetings|voice|log|all)", s)
	}
}

// wantAnswer decides whether to generate an AI answer. forceOn is --answer,
// forceOff is --no-answer. With a usable client the answer is automatic; with
// --answer but no usable client, unavailable signals a clear error; in auto
// mode without one it is silently skipped.
func wantAnswer(jsonMode, forceOn, forceOff, available bool) (do, unavailable bool) {
	if jsonMode || forceOff {
		return false, false
	}
	if available {
		return true, false
	}
	if forceOn {
		return false, true
	}
	return false, false
}

// answerClient builds the synthesis client for grounded search answers per the
// configured provider. available=false with a reason means answers are skipped
// in auto mode (and the reason is surfaced if --answer forces one).
func answerClient(cfg *config.Config) (client synth.Client, available bool, reason error) {
	if cfg.SynthProvider == config.SynthProviderOllama {
		return synth.NewOllama(cfg.OllamaBaseURL, cfg.SynthNumCtx), true, nil
	}
	if cfg.LocalOnly {
		return nil, false, fmt.Errorf("local_only is enabled: cloud answers are disabled — set `synth_provider: ollama` for local answers (see docs/DATA-FLOWS.md)")
	}
	if cfg.SynthProvider == config.SynthProviderOpenAI {
		if _, err := config.OpenAIAPIKey(); err != nil {
			return nil, false, fmt.Errorf("--answer needs %s set in the environment", config.OpenAIKeyEnv)
		}
		key, _ := config.OpenAIAPIKey()
		return synth.NewOpenAI(cfg.SynthOpenAIBaseURL, key), true, nil
	}
	if _, err := config.AnthropicAPIKey(); err != nil {
		return nil, false, fmt.Errorf("--answer needs %s set in the environment", config.AnthropicKeyEnv)
	}
	key, _ := config.AnthropicAPIKey()
	return synth.NewAnthropic(key), true, nil
}

// runSearch returns the top-k results. It is a thin wrapper over searchChunks
// (which retains full chunk bodies for the optional AI answer).
func runSearch(ctx context.Context, cfg *config.Config, e embed.Embedder, query string, k int, f store.Filter) ([]Result, error) {
	scored, err := searchChunks(ctx, cfg, e, query, k, f)
	if err != nil {
		return nil, err
	}
	return resultsFromScored(scored), nil
}

// resultsFromScored maps scored chunks to display Results. Search results carry
// the full chunk body (k is small, and MCP/--json consumers need the content,
// not just the snippet); list commands stay snippet-only.
func resultsFromScored(scored []scoredChunk) []Result {
	results := make([]Result, len(scored))
	for i, sc := range scored {
		results[i] = chunkToResult(sc.chunk, sc.score)
		results[i].Body = sc.chunk.Body
	}
	return results
}

// searchChunks embeds the query (with the retrieval instruction), fetches the
// nearest candidates, reranks them, and returns the top k scored chunks. If
// reranking fails it falls back to vector-distance order so search still works.
func searchChunks(ctx context.Context, cfg *config.Config, e embed.Embedder, query string, k int, f store.Filter) ([]scoredChunk, error) {
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
		return nil, nil
	}

	var scored []scoredChunk
	if cfg.Reranker == "" {
		// Reranking disabled: use vector-KNN (ascending distance) order.
		scored = distanceOrder(cands)
	} else {
		scored = rerankOrDistance(ctx, e, query, cands)
	}
	if len(scored) > k {
		scored = scored[:k]
	}
	return scored, nil
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
		return distanceOrder(cands)
	}
	out := make([]scoredChunk, len(cands))
	for i, c := range cands {
		out[i] = scoredChunk{chunk: c.Chunk, score: float64(scores[i])}
	}
	// Stable sort by score desc so ties keep KNN (distance) order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

// distanceOrder preserves the vector-KNN ascending-distance order, assigning a
// monotonic distance-derived score in (0,1].
func distanceOrder(cands []store.Candidate) []scoredChunk {
	out := make([]scoredChunk, len(cands))
	for i, c := range cands {
		out[i] = scoredChunk{chunk: c.Chunk, score: 1.0 / (1.0 + c.Distance)}
	}
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
	searchCmd.Flags().BoolVar(&searchAnswer, "answer", false, "force an AI answer above the results (needs ANTHROPIC_API_KEY)")
	searchCmd.Flags().BoolVar(&searchNoAnswer, "no-answer", false, "never generate an AI answer, even if a key is set")
	searchCmd.Flags().StringArrayVar(&searchSources, "source", nil, "restrict to a source: notes | transcript | meetings | all (repeatable, OR semantics)")
	rootCmd.AddCommand(searchCmd)
}
