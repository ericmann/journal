package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/editor"
	"github.com/ericmann/journal/internal/index"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/vcs"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// now is the clock, overridable in tests.
var now = time.Now

// Seams for composing a note without inline text, overridable in tests so the
// editor/stdin paths can run without a real terminal.
var (
	// openEditor composes a note in the user's editor and returns its contents.
	openEditor = editorOpenDefault
	// stdinIsTerminal reports whether stdin is an interactive terminal.
	stdinIsTerminal = defaultStdinIsTerminal
	// stdinIsPiped reports whether stdin is a pipe / redirected file / socket
	// (i.e. has data to read) rather than a char device like a TTY or /dev/null.
	stdinIsPiped = defaultStdinIsPiped
	// readStdin reads the whole of stdin (used when input is piped).
	readStdin = readStdinDefault
)

func editorOpenDefault(cmd string) (string, error) { return editor.Open(cmd) }

func readStdinDefault() ([]byte, error) { return io.ReadAll(os.Stdin) }

func defaultStdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// defaultStdinIsPiped is true when stdin is not a character device — i.e. it is
// a pipe, a redirected file, or a socket, so a note can be read from it.
func defaultStdinIsPiped() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice == 0
}

// composeNote obtains note text when none was given on the command line. It
// routes by the kind of stdin: an interactive terminal opens the editor; piped
// or redirected input is read directly; anything else (e.g. /dev/null, or a
// non-interactive context with no input) is a clear error rather than a hung
// read or an editor launched without a terminal.
func composeNote(cfg *config.Config) (string, error) {
	switch {
	case stdinIsTerminal():
		text, err := openEditor(editor.Resolve(cfg.Editor))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("aborting capture: empty note (nothing saved in the editor)")
		}
		return text, nil
	case stdinIsPiped():
		data, err := readStdin()
		if err != nil {
			return "", fmt.Errorf("reading note from stdin: %w", err)
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("no note text given and not running in a terminal; " +
			"pass the note as an argument, pipe it on stdin, or run in a terminal to use the editor")
	}
}

var (
	captureTags    []string
	captureProject string
	captureMarker  string
)

var captureCmd = &cobra.Command{
	Use:   "capture [text]",
	Short: "Append a timestamped note to today's journal (no embedding)",
	Long: "capture appends a timestamped, tagged block to today's daily file (or to a\n" +
		"project's notes with --project). It is append-only and returns immediately\n" +
		"(embedding happens later in `journal index`). When the repo is a git work\n" +
		"tree it also auto-commits the note (git_autocommit, unsigned by default).\n\n" +
		"With no text it opens your editor to compose the note (like `git commit`),\n" +
		"or reads the note from stdin when input is piped (e.g. `journal capture <\n" +
		"note.md`). The editor follows the `editor` config key, then $JOURNAL_EDITOR,\n" +
		"$VISUAL, $EDITOR, then nano.",
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := config.FindRepoRoot(".")
		if err != nil {
			return err
		}
		cfg, err := config.Load(root)
		if err != nil {
			return err
		}
		text := strings.Join(args, " ")
		if len(args) == 0 {
			if text, err = composeNote(cfg); err != nil {
				return err
			}
		}
		path, err := capture(root, now(), text, captureTags, captureProject, captureMarker)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "captured → %s\n", relTo(root, path))

		// Commit the note immediately so it's never left uncommitted — even when
		// the watcher isn't running. No-op outside a git repo; failures are
		// logged, never fatal (the markdown is already safely on disk).
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

// autoCommitCapture commits note changes after a capture, gated by config and
// only when root is a git top level. Returns committed=false when disabled, not
// a git repo, or nothing changed.
func autoCommitCapture(cfg *config.Config, root string, t time.Time) (bool, error) {
	if !cfg.GitAutocommit || !vcs.IsRepoRoot(root) {
		return false, nil
	}
	return vcs.CommitAll(root, index.CaptureCommitMessage(t), cfg.GitAutocommitSign)
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
