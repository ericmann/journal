// Package cmd holds the Cobra command tree for the journal CLI.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// logLevel is the global stderr log verbosity (default quiet).
var logLevel string

// rootCmd is the base command invoked as `journal`.
var rootCmd = &cobra.Command{
	Use:           "journal",
	Short:         "A local-first developer journal with semantic retrieval and AI synthesis",
	Long:          "journal turns a folder of plain-markdown developer notes into a searchable,\nAI-queryable corpus with scheduled synthesis jobs. Markdown is the source of\ntruth; the on-disk index is a disposable, rebuildable cache.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and exits non-zero on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "journal:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "warn",
		"log verbosity: debug|info|warn|error (logs go to stderr)")
}
