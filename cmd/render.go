package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// renderMarkdown writes md to out, styled with glamour when out is an
// interactive terminal (bold/italic/headers/lists/code, Claude-Code-style) and
// as plain markdown otherwise (piped, redirected, or tests) so nothing is
// garbled with ANSI escapes.
func renderMarkdown(out io.Writer, md string) {
	if isTerminal(out) {
		if r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(100)); err == nil {
			if styled, err := r.Render(md); err == nil {
				fmt.Fprint(out, styled)
				return
			}
		}
	}
	fmt.Fprintln(out, strings.TrimRight(md, "\n"))
}

// isTerminal reports whether out is a terminal-backed *os.File.
func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
