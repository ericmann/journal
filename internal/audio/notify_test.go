package audio

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNotifyCallsNotifierWithTitleAndMessage(t *testing.T) {
	f := &FakeNotifier{}
	var out bytes.Buffer

	Notify(f, "journal log", "● recording", &out)

	if len(f.Calls) != 1 {
		t.Fatalf("expected notifier to be called once, got %d calls", len(f.Calls))
	}
	if f.Calls[0].Title != "journal log" || f.Calls[0].Message != "● recording" {
		t.Errorf("Notify(title=%q, message=%q), want (journal log, ● recording)", f.Calls[0].Title, f.Calls[0].Message)
	}
	if out.String() != "" {
		t.Errorf("expected no output on success, got %q", out.String())
	}
}

func TestNotifyDegradesSilentlyOnError(t *testing.T) {
	f := &FakeNotifier{Err: errors.New("no notifier available")}
	var out bytes.Buffer

	Notify(f, "journal log", "✓ logged: Voice Note", &out)

	if len(f.Calls) != 1 {
		t.Fatal("expected notifier to be called even though it errors")
	}
	if !strings.Contains(out.String(), "notification failed") {
		t.Errorf("expected notification failure to be logged, got %q", out.String())
	}
}

func TestNotifyNilNotifierIsNoOp(t *testing.T) {
	var out bytes.Buffer
	// Must not panic.
	Notify(nil, "journal log", "● recording", &out)
	if out.String() != "" {
		t.Errorf("expected no output for nil notifier, got %q", out.String())
	}
}

func TestOsascriptNotifierNoBinariesReturnsError(t *testing.T) {
	// Neither osascript nor terminal-notifier exist on this PATH (CI/Linux
	// runners) — Notify must return a descriptive error, never block.
	t.Setenv("PATH", t.TempDir())

	err := osascriptNotifier{}.Notify("journal log", "● recording")
	if err == nil {
		t.Fatal("expected error when no notifier binary is available")
	}
	if !strings.Contains(err.Error(), "no notifier available") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNotifySendNotifierNoBinaryReturnsError(t *testing.T) {
	// notify-send does not exist on this PATH — Notify must return a
	// descriptive error, never block.
	t.Setenv("PATH", t.TempDir())

	err := notifySendNotifier{}.Notify("journal log", "● recording")
	if err == nil {
		t.Fatal("expected error when notify-send is unavailable")
	}
	if !strings.Contains(err.Error(), "notify-send") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDefaultNotifierForGOOS(t *testing.T) {
	tests := []struct {
		goos string
		want Notifier
	}{
		{goos: "darwin", want: osascriptNotifier{}},
		{goos: "linux", want: notifySendNotifier{}},
		{goos: "windows", want: osascriptNotifier{}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := defaultNotifierForGOOS(tt.goos); got != tt.want {
				t.Errorf("defaultNotifierForGOOS(%q) = %#v, want %#v", tt.goos, got, tt.want)
			}
		})
	}
}

func TestAppleScriptQuoteEscapesQuotesAndBackslashes(t *testing.T) {
	got := appleScriptQuote(`say "hi" \ there`)
	want := `"say \"hi\" \\ there"`
	if got != want {
		t.Errorf("appleScriptQuote() = %q, want %q", got, want)
	}
}
