package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ericmann/journal/internal/store"
)

// Result is one item in the stable --json schema for read commands.
type Result struct {
	Path      string   `json:"path"`
	LineStart int      `json:"line_start"`
	LineEnd   int      `json:"line_end"`
	Heading   string   `json:"heading"`
	Snippet   string   `json:"snippet"`
	Score     float64  `json:"score"`
	Tags      []string `json:"tags"`
	Markers   []string `json:"markers"`
}

// resultsEnvelope is the top-level JSON object: {"results": [...]}.
type resultsEnvelope struct {
	Results []Result `json:"results"`
}

// errorEnvelope is the JSON error shape: {"error": "..."} — emitted on failure
// so a machine consumer can distinguish an error from empty results.
type errorEnvelope struct {
	Error string `json:"error"`
}

// Citation renders the stable "path:line_start-line_end" reference.
func (r Result) Citation() string {
	return fmt.Sprintf("%s:%d-%d", r.Path, r.LineStart, r.LineEnd)
}

// chunkToResult converts a store chunk + score into a Result.
func chunkToResult(c store.Chunk, score float64) Result {
	return Result{
		Path:      c.Path,
		LineStart: c.LineStart,
		LineEnd:   c.LineEnd,
		Heading:   c.Heading,
		Snippet:   snippet(c.Body, 240),
		Score:     score,
		Tags:      nonNil(c.Tags),
		Markers:   nonNil(c.Markers),
	}
}

// snippet collapses whitespace and truncates body to at most n runes.
func snippet(body string, n int) string {
	s := strings.Join(strings.Fields(body), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// marshalResults serializes results to the stable {results:[...]} JSON used by
// both --json output and the MCP server.
func marshalResults(results []Result) (string, error) {
	b, err := json.MarshalIndent(resultsEnvelope{Results: nonNilResults(results)}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// renderResults writes results as JSON (stable schema) or human-readable text.
func renderResults(out io.Writer, results []Result, jsonMode bool) error {
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(resultsEnvelope{Results: nonNilResults(results)})
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "no results")
		return nil
	}
	for _, r := range results {
		head := r.Heading
		if head != "" {
			head = "  " + head
		}
		fmt.Fprintf(out, "%s%s", r.Citation(), head)
		if r.Score > 0 {
			fmt.Fprintf(out, "  (%.3f)", r.Score)
		}
		fmt.Fprintln(out)
		if r.Snippet != "" {
			fmt.Fprintf(out, "    %s\n", r.Snippet)
		}
	}
	return nil
}

func nonNilResults(r []Result) []Result {
	if r == nil {
		return []Result{}
	}
	return r
}

// renderError writes a JSON error envelope (json mode) and returns errSilent so
// the root command exits non-zero without re-printing to stderr. In text mode it
// returns err unchanged for the root to report.
func renderError(out io.Writer, err error, jsonMode bool) error {
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(errorEnvelope{Error: err.Error()})
		return errSilent
	}
	return err
}
