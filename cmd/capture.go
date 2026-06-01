package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/note"
	"github.com/spf13/cobra"
)

// now is the clock, overridable in tests.
var now = time.Now

var (
	captureTags    []string
	captureProject string
	captureMarker  string
)

var captureCmd = &cobra.Command{
	Use:   "capture <text>",
	Short: "Append a timestamped note to today's journal (no embedding)",
	Long: "capture appends a timestamped, tagged block to today's daily file (or to a\n" +
		"project's notes with --project). It is append-only and returns immediately;\n" +
		"embedding happens later in `journal index`.",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := config.FindRepoRoot(".")
		if err != nil {
			return err
		}
		text := strings.Join(args, " ")
		path, err := capture(root, now(), text, captureTags, captureProject, captureMarker)
		if err != nil {
			return err
		}
		rel := relTo(root, path)
		fmt.Fprintf(cmd.OutOrStdout(), "captured → %s\n", rel)
		return nil
	},
}

// capture builds a block from text + flags and appends it append-only. Tags and
// markers are the union of the flags and any #tags/@markers found inline in the
// text. It returns the absolute path written. It performs no embedding.
func capture(root string, t time.Time, text string, flagTags []string, project, flagMarker string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("capture text must not be empty")
	}

	tags := mergeTags(flagTags, note.ParseTags(text))

	markers := note.ParseMarkers(text)
	if flagMarker != "" {
		m := strings.ToLower(strings.TrimSpace(flagMarker))
		if !note.ValidMarker(m) {
			return "", fmt.Errorf("invalid marker %q (want decision|question|todo)", flagMarker)
		}
		markers = mergeStrings(markers, []string{m})
	}

	b := note.Block{Time: t, Tags: tags, Markers: markers, Body: text}
	if project != "" {
		return note.AppendProject(root, note.Slugify(project), b)
	}
	return note.AppendDaily(root, b)
}

// mergeTags normalizes flag tags (split values, strip leading '#', case-fold)
// and unions them with already-parsed inline tags, preserving first-seen order.
func mergeTags(flagTags, inline []string) []string {
	var norm []string
	for _, raw := range flagTags {
		for _, part := range strings.Split(raw, ",") {
			p := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "#")))
			if p != "" {
				norm = append(norm, p)
			}
		}
	}
	return mergeStrings(norm, inline)
}

func mergeStrings(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func init() {
	captureCmd.Flags().StringSliceVar(&captureTags, "tags", nil, "comma-separated tags (also detected inline as #tag)")
	captureCmd.Flags().StringVar(&captureProject, "project", "", "capture into projects/<slug>/ instead of the daily file")
	captureCmd.Flags().StringVar(&captureMarker, "marker", "", "structured marker: decision|question|todo")
	rootCmd.AddCommand(captureCmd)
}
