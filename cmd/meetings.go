package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

// Meeting is one entry in the meetings listing (one per transcript file).
type Meeting struct {
	Filename  string `json:"filename"`
	Timestamp string `json:"timestamp"` // RFC3339, or "" if unknown
	Title     string `json:"title"`
	Snippet   string `json:"snippet"`
}

type meetingsEnvelope struct {
	Meetings []Meeting `json:"meetings"`
}

var meetingsJSON bool

var meetingsCmd = &cobra.Command{
	Use:   "meetings",
	Short: "List recent meeting transcripts (newest first)",
	Long: "meetings lists the transcripts in your landing zone, newest first, with each\n" +
		"file's title (first H1 or filename), timestamp, and a snippet. Transcripts are\n" +
		"populated by `journal quill-sync` (or dropped-in files) and indexed by the watcher.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return renderError(out, err, meetingsJSON)
		}
		ms, err := recentMeetings(cmd.Context(), cfg, time.Time{}, 50)
		if err != nil {
			return renderError(out, err, meetingsJSON)
		}
		return renderMeetings(out, ms, meetingsJSON)
	},
}

// recentMeetings returns recent meeting transcripts (one per file), newest
// first, optionally limited to those whose timestamp is at/after since.
func recentMeetings(ctx context.Context, cfg *config.Config, since time.Time, limit int) ([]Meeting, error) {
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	// Transcript chunks share a per-file timestamp (mtime); Recent yields them
	// newest-first, so first-seen per path is the file's representative.
	chunks, err := s.Recent(ctx, store.Filter{Source: store.SourceTranscript, Since: since}, 0)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Meeting
	for _, c := range chunks {
		if seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		title, snip := meetingMeta(filepath.Join(cfg.Root(), filepath.FromSlash(c.Path)), c.Body)
		ts := ""
		if !c.CreatedAt.IsZero() {
			ts = c.CreatedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, Meeting{
			Filename:  filepath.Base(c.Path),
			Timestamp: ts,
			Title:     title,
			Snippet:   snip,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// meetingMeta derives a title (first H1, else filename stem) and a snippet from
// the transcript file, falling back to the chunk body if the file is unreadable.
func meetingMeta(absPath, fallbackBody string) (title, snip string) {
	stem := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return stem, snippet(fallbackBody, 240)
	}
	body := stripFrontmatter(string(data))
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimSpace(line[2:])
			break
		}
	}
	if title == "" {
		title = stem
	}
	return title, snippet(body, 240)
}

// stripFrontmatter drops a leading YAML frontmatter block (--- ... ---).
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	if idx := strings.Index(s[4:], "\n---"); idx >= 0 {
		rest := s[4+idx+4:]
		return strings.TrimLeft(rest, "\n")
	}
	return s
}

func marshalMeetings(ms []Meeting) (string, error) {
	if ms == nil {
		ms = []Meeting{}
	}
	b, err := json.MarshalIndent(meetingsEnvelope{Meetings: ms}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func renderMeetings(out io.Writer, ms []Meeting, jsonMode bool) error {
	if jsonMode {
		if ms == nil {
			ms = []Meeting{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(meetingsEnvelope{Meetings: ms})
	}
	if len(ms) == 0 {
		fmt.Fprintln(out, "no meetings (run `journal quill-sync`, then `journal index`)")
		return nil
	}
	for _, m := range ms {
		ts := m.Timestamp
		if ts == "" {
			ts = "(undated)"
		}
		fmt.Fprintf(out, "%s  %s  — %s\n", ts, m.Filename, m.Title)
		if m.Snippet != "" {
			fmt.Fprintf(out, "    %s\n", m.Snippet)
		}
	}
	return nil
}

func init() {
	meetingsCmd.Flags().BoolVar(&meetingsJSON, "json", false, "emit JSON ({meetings:[...]}) instead of text")
	rootCmd.AddCommand(meetingsCmd)
}
