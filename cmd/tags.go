package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/vcs"
	"github.com/spf13/cobra"
)

var (
	tagsJSON   bool
	tagsDryRun bool
)

// tagNameRe validates a tag name: only the characters allowed by note.tagRe.
var tagNameRe = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "List #tags with usage counts",
	Long: "tags lists every distinct #tag in the indexed corpus with its usage\n" +
		"count. Use `journal tags rename <old> <new>` to rewrite a tag across all\n" +
		"notes and re-index affected files.",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return renderError(out, err, tagsJSON)
		}
		tags, err := listTags(cmd.Context(), cfg)
		if err != nil {
			return renderError(out, err, tagsJSON)
		}
		return renderTagList(out, tags, tagsJSON)
	},
}

var tagsRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename a #tag across all notes, re-index, and auto-commit",
	Long: "rename rewrites every occurrence of #old to #new across all notes,\n" +
		"re-indexes affected files, and auto-commits. --dry-run previews which\n" +
		"files would change without writing anything. The leading # is optional.",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		oldTag := strings.TrimPrefix(args[0], "#")
		newTag := strings.TrimPrefix(args[1], "#")
		logf := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
		n, err := renameTags(cmd.Context(), cfg, newEmbedder(cfg), oldTag, newTag, tagsDryRun, logf)
		if err != nil {
			return err
		}
		if tagsDryRun {
			fmt.Fprintf(out, "dry-run: would update %d file(s)\n", n)
		} else {
			fmt.Fprintf(out, "renamed #%s → #%s in %d file(s)\n", oldTag, newTag, n)
		}
		return nil
	},
}

// listTags returns all distinct tags with counts from the index.
func listTags(ctx context.Context, cfg *config.Config) ([]store.TagCount, error) {
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.Tags(ctx)
}

// renderTagList writes the tag list as text or JSON.
func renderTagList(out io.Writer, tags []store.TagCount, jsonMode bool) error {
	type tagItem struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	type envelope struct {
		Tags []tagItem `json:"tags"`
	}
	if jsonMode {
		items := make([]tagItem, len(tags))
		for i, tc := range tags {
			items[i] = tagItem{Tag: tc.Tag, Count: tc.Count}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(envelope{Tags: items})
	}
	if len(tags) == 0 {
		fmt.Fprintln(out, "no tags found (index may be empty — run `journal index`)")
		return nil
	}
	for _, tc := range tags {
		fmt.Fprintf(out, "#%-30s %d\n", tc.Tag, tc.Count)
	}
	return nil
}

// renameTags rewrites #oldTag to #newTag across all indexed markdown files.
// It returns the number of files changed. Non-fatal re-index failures are
// logged via logf; the files on disk are always in the new state on return.
func renameTags(ctx context.Context, cfg *config.Config, e embed.Embedder, oldTag, newTag string, dryRun bool, logf func(string, ...any)) (int, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if !tagNameRe.MatchString(oldTag) {
		return 0, fmt.Errorf("invalid tag name %q: must contain only [0-9A-Za-z_-]", oldTag)
	}
	if !tagNameRe.MatchString(newTag) {
		return 0, fmt.Errorf("invalid tag name %q: must contain only [0-9A-Za-z_-]", newTag)
	}
	if oldTag == newTag {
		return 0, fmt.Errorf("old and new tag are identical: %q", oldTag)
	}

	files, err := index.Walk(cfg.Root(), noteExcludes(cfg), time.Time{})
	if err != nil {
		return 0, fmt.Errorf("walking files: %w", err)
	}

	re := buildTagReplaceRe(oldTag)
	repl := "${1}#" + newTag + "${3}"

	var changedPaths []string
	for _, f := range files {
		content, ferr := index.ReadFile(f)
		if ferr != nil {
			return 0, fmt.Errorf("reading %s: %w", f.RelPath, ferr)
		}
		newContent := re.ReplaceAllString(content, repl)
		if newContent == content {
			continue
		}
		changedPaths = append(changedPaths, f.RelPath)
		if dryRun {
			logf("  %s", f.RelPath)
			continue
		}
		if werr := os.WriteFile(f.AbsPath, []byte(newContent), 0o644); werr != nil {
			return 0, fmt.Errorf("writing %s: %w", f.RelPath, werr)
		}
		logf("updated %s", f.RelPath)
	}

	if dryRun || len(changedPaths) == 0 {
		return len(changedPaths), nil
	}

	// Re-index changed files; failure is non-fatal (the watcher or next `journal
	// index` will catch up).
	s, serr := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if serr != nil {
		logf("re-index skipped (run `journal index` later): %v", serr)
		return len(changedPaths), nil
	}
	defer s.Close()

	ix := index.NewIndexer(s, e)
	for _, relPath := range changedPaths {
		absPath := filepath.Join(cfg.Root(), filepath.FromSlash(relPath))
		data, rerr := os.ReadFile(absPath)
		if rerr != nil {
			logf("re-index skipped for %s: %v", relPath, rerr)
			continue
		}
		if _, ierr := ix.IndexContent(ctx, relPath, string(data)); ierr != nil {
			logf("re-index skipped for %s (run `journal index` later): %v", relPath, ierr)
		}
	}

	// Auto-commit.
	if cfg.GitAutocommit && vcs.IsRepoRoot(cfg.Root()) {
		msg := index.TagsRenameCommitMessage(oldTag, newTag, len(changedPaths), now())
		if committed, cerr := vcs.CommitAll(cfg.Root(), msg, cfg.GitAutocommitSign); cerr != nil {
			logf("auto-commit skipped (notes are safe on disk): %v", cerr)
		} else if committed {
			logf("committed ✓")
		}
	}

	return len(changedPaths), nil
}

// buildTagReplaceRe returns a regexp matching the exact #tag token with the
// same boundary rules as note.tagRe: preceded by start-of-text or whitespace;
// followed by a non-tag character or end-of-text. Captured groups:
//
//	(1) prefix (whitespace or empty for start-of-text)
//	(2) #tag literal
//	(3) trailing character (non-tag char, or empty string at end-of-text)
func buildTagReplaceRe(tag string) *regexp.Regexp {
	return regexp.MustCompile(`(^|\s)(#` + regexp.QuoteMeta(tag) + `)([^0-9A-Za-z_-]|$)`)
}

func init() {
	tagsCmd.Flags().BoolVar(&tagsJSON, "json", false, "emit JSON ({tags:[...]})")
	tagsRenameCmd.Flags().BoolVar(&tagsDryRun, "dry-run", false, "preview which files would change without writing")
	tagsCmd.AddCommand(tagsRenameCmd)
	rootCmd.AddCommand(tagsCmd)
}
