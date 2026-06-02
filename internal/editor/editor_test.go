package editor

import (
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	// Clear all sources, then layer them back to assert precedence.
	t.Setenv("JOURNAL_EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	if got := Resolve(""); got != Default {
		t.Errorf("empty everything: got %q, want %q", got, Default)
	}
	if got := Resolve("vim"); got != "vim" {
		t.Errorf("configured wins over default: got %q", got)
	}

	t.Setenv("EDITOR", "emacs")
	if got := Resolve(""); got != "emacs" {
		t.Errorf("$EDITOR wins over default: got %q", got)
	}
	t.Setenv("VISUAL", "code --wait")
	if got := Resolve(""); got != "code --wait" {
		t.Errorf("$VISUAL wins over $EDITOR: got %q", got)
	}
	if got := Resolve("nvim"); got != "nvim" {
		t.Errorf("configured wins over $VISUAL/$EDITOR: got %q", got)
	}
	t.Setenv("JOURNAL_EDITOR", "helix")
	if got := Resolve("nvim"); got != "helix" {
		t.Errorf("$JOURNAL_EDITOR wins over all: got %q", got)
	}
}

// TestOpenRoundTrips uses a shell snippet as a fake editor that writes content
// into the file it is handed, proving Open passes the path and reads it back.
func TestOpenRoundTrips(t *testing.T) {
	got, err := Open(`printf 'composed in editor\n' >`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "composed in editor\n" {
		t.Errorf("Open round-trip = %q, want %q", got, "composed in editor\n")
	}
}

func TestOpenPropagatesEditorFailure(t *testing.T) {
	if _, err := Open("false"); err == nil {
		t.Error("expected an error when the editor exits non-zero")
	}
}
