package audio

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Notifier sends a desktop notification. Implementations must be safe to
// call from the recording daemon and the pipeline: a slow or failing
// Notify must never block or abort the caller — see Notify below, which
// is the boundary every call site should go through.
type Notifier interface {
	Notify(title, message string) error
}

// osascriptNotifier sends notifications via macOS's `osascript`
// (`display notification`), falling back to `terminal-notifier` if
// osascript is unavailable or the call fails.
type osascriptNotifier struct{}

// Notify implements Notifier.
func (osascriptNotifier) Notify(title, message string) error {
	var errs []string

	err := runOsascript(title, message)
	if err == nil {
		return nil
	}
	errs = append(errs, err.Error())

	err = runTerminalNotifier(title, message)
	if err == nil {
		return nil
	}
	errs = append(errs, err.Error())

	return fmt.Errorf("no notifier available (%s)", strings.Join(errs, "; "))
}

// runOsascript sends a notification via macOS's `osascript -e 'display
// notification'`. It returns an error (never panics/blocks indefinitely) when
// osascript is missing from PATH or the command fails.
func runOsascript(title, message string) error {
	if _, err := exec.LookPath("osascript"); err != nil {
		return fmt.Errorf("osascript not found in PATH: %w", err)
	}
	script := fmt.Sprintf("display notification %s with title %s",
		appleScriptQuote(message), appleScriptQuote(title))
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		return fmt.Errorf("osascript: %w", err)
	}
	return nil
}

// runTerminalNotifier sends a notification via the `terminal-notifier` CLI,
// the documented fallback when osascript is unavailable.
func runTerminalNotifier(title, message string) error {
	if _, err := exec.LookPath("terminal-notifier"); err != nil {
		return fmt.Errorf("terminal-notifier not found in PATH: %w", err)
	}
	if err := exec.Command("terminal-notifier", "-title", title, "-message", message).Run(); err != nil {
		return fmt.Errorf("terminal-notifier: %w", err)
	}
	return nil
}

// appleScriptQuote renders s as a double-quoted AppleScript string literal,
// escaping backslashes and quotes so untrusted note text can never break out
// of the literal into executable AppleScript.
func appleScriptQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// DefaultNotifier is the live Notifier used by `journal log`. Tests inject a
// fake Notifier instead of calling this, so no test ever pops a real OS
// notification.
var DefaultNotifier Notifier = osascriptNotifier{}

// AvailableNotifier returns the name of the first notifier backend resolvable
// on PATH ("osascript" or "terminal-notifier"), or "" if neither is present.
// It performs the same PATH lookups osascriptNotifier.Notify does, without
// sending a notification, so `journal doctor` can report notifier status
// using the same definition of "available" as the live path.
func AvailableNotifier() string {
	if _, err := exec.LookPath("osascript"); err == nil {
		return "osascript"
	}
	if _, err := exec.LookPath("terminal-notifier"); err == nil {
		return "terminal-notifier"
	}
	return ""
}

// Notify sends a notification via n and degrades silently on failure: it
// writes a log-only line to out rather than returning an error, so a broken
// or absent notifier (no osascript/terminal-notifier, e.g. off macOS) never
// blocks or fails the recording/pipeline it's reporting on. A nil Notifier
// is a no-op.
func Notify(n Notifier, title, message string, out io.Writer) {
	if n == nil {
		return
	}
	if err := n.Notify(title, message); err != nil {
		fmt.Fprintf(out, "  (notification failed: %v)\n", err)
	}
}

// FakeNotifier is a deterministic Notifier for tests — it never execs
// osascript/terminal-notifier. It records every call so tests can assert on
// what was sent.
type FakeNotifier struct {
	// Err, when non-nil, is returned by every Notify call.
	Err error

	Calls []struct{ Title, Message string }
}

// Notify implements Notifier.
func (f *FakeNotifier) Notify(title, message string) error {
	f.Calls = append(f.Calls, struct{ Title, Message string }{title, message})
	return f.Err
}
