package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/vcs"
	"github.com/spf13/cobra"
)

var doneCmd = &cobra.Command{
	Use:   "done <path:line | text fragment>",
	Short: "Complete an open @todo (rewrites it to @done with today's date)",
	Long: "done finds the open @todo matching the reference — a citation from `journal\n" +
		"todos` (path:line) or a unique text fragment — and rewrites that one @todo\n" +
		"token to `@done YYYY-MM-DD` in the note file, then re-indexes and auto-commits.\n" +
		"The note's content is otherwise untouched.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		logf := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
		res, err := completeTodo(cmd.Context(), cfg, newEmbedder(cfg), args[0], logf)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "done ✓ %s\n", res.Citation())
		if res.Snippet != "" {
			fmt.Fprintf(out, "    %s\n", res.Snippet)
		}
		return nil
	},
}

// citationRe matches "path:NN" or "path:NN-MM" refs from `journal todos`.
var citationRe = regexp.MustCompile(`^(.+\.md):(\d+)(?:-(\d+))?$`)

// todoTokenRe matches the @todo marker token with the same boundary rules as
// note.ParseMarkers, so we rewrite exactly what the indexer recognized.
var todoTokenRe = regexp.MustCompile(`(^|\s)@todo\b`)

// completeTodo finds the single open @todo matching ref, rewrites its @todo
// token to "@done YYYY-MM-DD" in the source file, re-indexes that file (failure
// non-fatal: the watcher catches up), and auto-commits. It is the shared core
// for the done command, the MCP done tool, and the TUI. It returns the
// completed item (its pre-completion citation/snippet).
func completeTodo(ctx context.Context, cfg *config.Config, e embed.Embedder, ref string, logf func(string, ...any)) (Result, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Result{}, fmt.Errorf("a reference is required: a citation from `journal todos` (path:line) or a text fragment")
	}

	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return Result{}, err
	}
	defer s.Close()

	open, err := s.Recent(ctx, store.Filter{Markers: []string{note.MarkerTodo}}, 0)
	if err != nil {
		return Result{}, err
	}
	if len(open) == 0 {
		return Result{}, fmt.Errorf("no open todos")
	}

	target, err := matchTodo(open, ref)
	if err != nil {
		return Result{}, err
	}

	// Rewrite the first @todo token inside the chunk's line range.
	abs := filepath.Join(cfg.Root(), filepath.FromSlash(target.Path))
	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{}, fmt.Errorf("reading %s: %w", target.Path, err)
	}
	lines := strings.Split(string(data), "\n")
	start, end := target.LineStart-1, target.LineEnd // 1-based inclusive -> slice range
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	replaced := false
	stamp := now().Format("2006-01-02")
	for i := start; i < end; i++ {
		if loc := todoTokenRe.FindStringSubmatchIndex(lines[i]); loc != nil {
			// loc[2]:loc[3] is the boundary prefix (group 1); the token follows it.
			tokenStart := loc[3]
			lines[i] = lines[i][:tokenStart] + "@done " + stamp + lines[i][tokenStart+len("@todo"):]
			replaced = true
			break
		}
	}
	if !replaced {
		return Result{}, fmt.Errorf("%s no longer contains @todo at lines %d-%d — the index is stale; run `journal index` and retry",
			target.Path, target.LineStart, target.LineEnd)
	}
	newContent := strings.Join(lines, "\n")
	if err := os.WriteFile(abs, []byte(newContent), 0o644); err != nil {
		return Result{}, fmt.Errorf("writing %s: %w", target.Path, err)
	}

	// Re-index so todos/--done reflect the change immediately. Non-fatal (e.g.
	// Ollama down in the moment): the file is already updated; the watcher or the
	// next `journal index` reconciles.
	if _, ierr := index.NewIndexer(s, e).IndexContent(ctx, target.Path, newContent); ierr != nil {
		logf("re-index skipped (run `journal index` later): %v", ierr)
	}

	// Auto-commit the completion, same gate as capture.
	if cfg.GitAutocommit && vcs.IsRepoRoot(cfg.Root()) {
		if committed, cerr := vcs.CommitAll(cfg.Root(), index.DoneCommitMessage(now()), cfg.GitAutocommitSign); cerr != nil {
			logf("auto-commit skipped (note is safe on disk): %v", cerr)
		} else if committed {
			logf("committed ✓")
		}
	}
	return chunkToResult(target, 0), nil
}

// matchTodo resolves ref against the open todos: a citation (path:line) matches
// the chunk containing that line; otherwise a case-insensitive text fragment
// must match exactly one chunk body.
func matchTodo(open []store.Chunk, ref string) (store.Chunk, error) {
	if m := citationRe.FindStringSubmatch(ref); m != nil {
		path := filepath.ToSlash(m[1])
		line, _ := strconv.Atoi(m[2])
		for _, c := range open {
			if c.Path == path && line >= c.LineStart && line <= c.LineEnd {
				return c, nil
			}
		}
		return store.Chunk{}, fmt.Errorf("no open todo at %s:%d (see `journal todos`)", path, line)
	}

	needle := strings.ToLower(ref)
	var matches []store.Chunk
	for _, c := range open {
		if strings.Contains(strings.ToLower(c.Body), needle) || strings.Contains(strings.ToLower(c.Heading), needle) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return store.Chunk{}, fmt.Errorf("no open todo matching %q (see `journal todos`)", ref)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches %d open todos — be more specific or use a citation:\n", ref, len(matches))
		for _, c := range matches {
			fmt.Fprintf(&b, "  %s:%d-%d  %s\n", c.Path, c.LineStart, c.LineEnd, snippet(c.Body, 80))
		}
		return store.Chunk{}, fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
	}
}

func init() {
	rootCmd.AddCommand(doneCmd)
}
