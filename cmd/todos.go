package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

var (
	todosDone    bool
	todosAll     bool
	todosProject string
	todosSince   string
	todosJSON    bool
)

var todosCmd = &cobra.Command{
	Use:   "todos",
	Short: "List open @todo notes (newest first)",
	Long: "todos lists your open @todo blocks with citations you can hand to `journal\n" +
		"done`. --done shows completed (@done) items, --all shows both. Capture a todo\n" +
		"with `journal capture \"call bob @todo\"` (or --marker todo); complete it with\n" +
		"`journal done <path:line | text fragment>`.",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return renderError(out, err, todosJSON)
		}
		results, err := listTodos(cmd.Context(), cfg, todosMode(), todosProject, todosSince)
		if err != nil {
			return renderError(out, err, todosJSON)
		}
		return renderTodos(out, results, todosJSON, todosDone)
	},
}

// todosMode maps the flags to which marker sets to list.
func todosMode() []string {
	switch {
	case todosAll:
		return []string{note.MarkerTodo, note.MarkerDone}
	case todosDone:
		return []string{note.MarkerDone}
	default:
		return []string{note.MarkerTodo}
	}
}

// listTodos returns todo/done chunks as Results, newest first. markers is the
// set of marker kinds to include (each queried separately — Filter.Markers is
// AND semantics — then merged newest-first).
func listTodos(ctx context.Context, cfg *config.Config, markers []string, project, sinceStr string) ([]Result, error) {
	since, err := parseSince(sinceStr)
	if err != nil {
		return nil, err
	}
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	var chunks []store.Chunk
	for _, m := range markers {
		f := store.Filter{Markers: []string{m}, Project: project}
		if since > 0 {
			f.Since = now().Add(-since)
		}
		cs, err := s.Recent(ctx, f, 0)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, cs...)
	}
	// Merge newest-first (undated last); dedupe blocks carrying both markers.
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].CreatedAt.IsZero() != chunks[j].CreatedAt.IsZero() {
			return !chunks[i].CreatedAt.IsZero()
		}
		return chunks[i].CreatedAt.After(chunks[j].CreatedAt)
	})
	seen := map[string]bool{}
	var results []Result
	for _, c := range chunks {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		results = append(results, chunkToResult(c, 0))
	}
	return results, nil
}

// renderTodos prints todos numbered with citations (text) or the stable
// {results:[...]} JSON. doneMode only adjusts the empty-state hint.
func renderTodos(out io.Writer, results []Result, jsonMode, doneMode bool) error {
	if jsonMode {
		return renderResults(out, results, true)
	}
	if len(results) == 0 {
		if doneMode {
			fmt.Fprintln(out, "no completed todos")
		} else {
			fmt.Fprintln(out, "no open todos — capture one with `journal capture \"… @todo\"` (todos appear once indexed: `journal index` or the watcher)")
		}
		return nil
	}
	for i, r := range results {
		fmt.Fprintf(out, "%2d. %s", i+1, r.Citation())
		if r.Heading != "" {
			fmt.Fprintf(out, "  %s", r.Heading)
		}
		fmt.Fprintln(out)
		if r.Snippet != "" {
			fmt.Fprintf(out, "      %s\n", r.Snippet)
		}
	}
	if !doneMode {
		fmt.Fprintln(out, "\ncomplete one with `journal done <path:line>` or `journal done \"text fragment\"`")
	}
	return nil
}

func init() {
	todosCmd.Flags().BoolVar(&todosDone, "done", false, "list completed (@done) items instead")
	todosCmd.Flags().BoolVar(&todosAll, "all", false, "list open and completed items")
	todosCmd.Flags().StringVar(&todosProject, "project", "", "filter to a project slug")
	todosCmd.Flags().StringVar(&todosSince, "since", "", "only items created within this window (e.g. 2w)")
	todosCmd.Flags().BoolVar(&todosJSON, "json", false, "emit JSON ({results:[...]})")
	rootCmd.AddCommand(todosCmd)
}
