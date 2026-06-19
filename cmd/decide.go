package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	decideRationale string
	decideProject   string
)

var decideCmd = &cobra.Command{
	Use:   "decide <statement>",
	Short: "Capture a @decision — a one-line statement with optional rationale",
	Long: "decide appends a timestamped @decision block to today's journal (or to a\n" +
		"project's notes with --project). The statement is the first argument; use\n" +
		"--rationale to add a Rationale: line. Auto-commits like `journal capture`.\n\n" +
		"Example:\n" +
		"  journal decide \"use sqlite-vec for vectors\" --rationale \"Pure-Go, no cgo\"",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := resolveRoot()
		if err != nil {
			return err
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		body := strings.Join(args, " ")
		if decideRationale != "" {
			body += "\nRationale: " + strings.TrimSpace(decideRationale)
		}
		path, err := capture(root, now(), body, nil, decideProject, "decision")
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "decided → %s\n", relTo(root, path))
		committed, cerr := autoCommitCapture(cfg, root, now())
		switch {
		case cerr != nil:
			fmt.Fprintf(out, "  (auto-commit skipped: %v)\n", cerr)
		case committed:
			fmt.Fprintln(out, "  committed ✓")
		}
		return nil
	},
}

func init() {
	decideCmd.Flags().StringVar(&decideRationale, "rationale", "", "optional rationale for the decision")
	decideCmd.Flags().StringVar(&decideProject, "project", "", "capture into projects/<slug>/ instead of the daily file")
	rootCmd.AddCommand(decideCmd)
}
