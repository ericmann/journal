// Package cmd holds the Cobra command tree for the journal CLI.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

// errSilent signals that an error has already been reported to the user (e.g. as
// a JSON error envelope on stdout). Execute exits non-zero without printing it.
var errSilent = errors.New("")

// relTo returns path relative to root for display, falling back to the absolute
// path if it can't be made relative.
func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// logLevel is the global stderr log verbosity (default quiet).
var logLevel string

// version is the build version, overridable at build time via:
//
//	go build -ldflags "-X github.com/ericmann/journal/cmd.version=v1.0.0"
var version = "dev"

// rootCmd is the base command invoked as `journal`.
var rootCmd = &cobra.Command{
	Use:           "journal",
	Version:       version,
	Short:         "A local-first developer journal with semantic retrieval and AI synthesis",
	Long:          "journal turns a folder of plain-markdown developer notes into a searchable,\nAI-queryable corpus with scheduled synthesis jobs. Markdown is the source of\ntruth; the on-disk index is a disposable, rebuildable cache.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	// Global: operate on a journal repo other than the current directory. Also
	// settable via $JOURNAL_DIR (the flag wins). ~ is expanded.
	rootCmd.PersistentFlags().StringVar(&journalDir, "journal-dir", "",
		"path to the journal repo (default: current directory; or set "+JournalDirEnv+")")
}

// Execute runs the root command and exits non-zero on error.
func Execute() {
	// Cancel the command context on Ctrl-C / SIGTERM so long-running commands
	// (notably `index --watch`) shut down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, "journal:", err)
		}
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "warn",
		"log verbosity: debug|info|warn|error (logs go to stderr)")
}
