// Package editor resolves and launches the user's text editor so a note can be
// composed interactively (like `git commit` with no -m). It mirrors git's
// editor precedence and runs the editor as a shell command so flags work.
package editor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Default is the fallback editor when none is configured or in the environment.
// nano is chosen over vi because it is more broadly available and friendlier;
// override it per-repo with the `editor` config key or the usual env vars.
const Default = "nano"

// Resolve returns the editor command to launch, trying, in order: the
// JOURNAL_EDITOR env var, the repo's configured editor, $VISUAL, $EDITOR, then
// Default. The result is a shell command string (e.g. "vim" or "code --wait").
func Resolve(configured string) string {
	for _, candidate := range []string{
		os.Getenv("JOURNAL_EDITOR"),
		configured,
		os.Getenv("VISUAL"),
		os.Getenv("EDITOR"),
	} {
		if s := strings.TrimSpace(candidate); s != "" {
			return s
		}
	}
	return Default
}

// Open creates an empty temporary markdown file, opens it in editorCmd, waits
// for the editor to close, and returns the file's contents. editorCmd is run
// via `sh -c` with the file path passed as a positional argument (not
// interpolated), so editor strings with flags work and the path is never
// subject to word-splitting. The editor inherits the terminal's stdio.
func Open(editorCmd string) (string, error) {
	f, err := os.CreateTemp("", "journal-*.md")
	if err != nil {
		return "", fmt.Errorf("creating temp file for note: %w", err)
	}
	path := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(path) }()

	if err := OpenPath(editorCmd, path); err != nil {
		return "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading composed note: %w", err)
	}
	return string(data), nil
}

// OpenPath opens an existing file in editorCmd and waits for the editor to
// close. Same launch mechanics as Open (`sh -c '<editorCmd> "$1"'`); used by
// `journal edit` to work on a real note file rather than a temp buffer.
func OpenPath(editorCmd, path string) error {
	// `sh -c '<editorCmd> "$1"' sh <path>` — the extra "sh" becomes $0, path is $1.
	c := exec.Command("sh", "-c", editorCmd+` "$1"`, "sh", path)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor %q exited with an error: %w", editorCmd, err)
	}
	return nil
}
