package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var todoProject string

var todoCmd = &cobra.Command{
	Use:   "todo <action>",
	Short: "Capture a @todo — a one-line action item",
	Long: "todo appends a timestamped @todo block to today's journal (or to a project's\n" +
		"notes with --project). The action is the first argument. Auto-commits like\n" +
		"`journal capture`. Complete it later with `journal done`.\n\n" +
		"Example:\n" +
		"  journal todo \"call bob about pricing\"",
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
		path, err := capture(root, now(), body, nil, todoProject, "todo")
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "todo → %s\n", relTo(root, path))
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
	todoCmd.Flags().StringVar(&todoProject, "project", "", "capture into projects/<slug>/ instead of the daily file")
	rootCmd.AddCommand(todoCmd)
}
