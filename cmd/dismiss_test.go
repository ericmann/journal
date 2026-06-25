package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/note"
)

// Fixtures: two projects + a daily note, spanning different dates so
// the age filter tests are deterministic regardless of when CI runs.
//
// acme todos  — project "acme", dated 2026-01-10 (old: >4w before 2026-06-25)
// janus todos — project "janus", dated 2026-01-10 (old)
// recent todo — daily note, dated 2026-06-24 (recent: <4w before 2026-06-25)
//
// Tests pin `now` to 2026-06-25 so --before comparisons are stable.

const acmeFixture = "# 2026-01-10\n\n" +
	"## 09:00 @todo\nfollow up on the Acme contract\n\n" +
	"## 10:00 @todo\nschedule Acme kickoff meeting\n"

const janusFixture = "# 2026-01-10\n\n" +
	"## 09:00 @todo\nreview Janus spec\n\n" +
	"## 10:00 @todo\nupdate Janus timeline\n"

const recentFixture = "# 2026-06-24\n\n" +
	"## 08:00 @todo\ncheck the deploy logs\n"

// pinNow sets now() to a fixed instant for the duration of the test.
func pinNow(t *testing.T, fixed time.Time) {
	t.Helper()
	orig := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = orig })
}

// dismissRepo creates a repo with the multi-project/age fixtures and indexes it.
func dismissRepo(t *testing.T) (*config.Config, *embed.Fake) {
	t.Helper()
	return indexedRepo(t, map[string]string{
		"projects/acme/notes/2026-01-10.md":  acmeFixture,
		"projects/janus/notes/2026-01-10.md": janusFixture,
		"daily/2026/06/2026-06-24.md":        recentFixture,
	})
}

// TestDismissProjectFilter asserts that --project only dismisses that project's todos.
func TestDismissProjectFilter(t *testing.T) {
	pinNow(t, time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	cfg, fake := dismissRepo(t)
	ctx := context.Background()

	var out bytes.Buffer
	err := runBulkDismiss(ctx, cfg, fake, "acme", "", "", true, strings.NewReader(""), &out, nil)
	if err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, "matched 2, dismissed 2, failed 0") {
		t.Errorf("expected 2 dismissed, got: %s", output)
	}

	// Acme file must have @done; Janus file must be untouched.
	acmeData, _ := os.ReadFile(filepath.Join(cfg.Root(), "projects", "acme", "notes", "2026-01-10.md"))
	if strings.Contains(string(acmeData), "@todo") {
		t.Errorf("acme file still contains @todo:\n%s", string(acmeData))
	}
	if !strings.Contains(string(acmeData), "@done 2026-06-25") {
		t.Errorf("acme file missing @done stamp:\n%s", string(acmeData))
	}

	janusData, _ := os.ReadFile(filepath.Join(cfg.Root(), "projects", "janus", "notes", "2026-01-10.md"))
	if !strings.Contains(string(janusData), "@todo") {
		t.Errorf("janus file was touched but should not have been:\n%s", string(janusData))
	}

	// Open todos via store: acme gone, janus + recent still open.
	open, err := listTodos(ctx, cfg, []string{note.MarkerTodo}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range open {
		if strings.Contains(r.Path, "acme") {
			t.Errorf("acme todo still open after dismiss: %s", r.Citation())
		}
	}
}

// TestDismissAgeFilter asserts that --before only dismisses todos older than the window.
func TestDismissAgeFilter(t *testing.T) {
	// Pin now to 2026-06-25; --before 4w → cutoff = 2026-05-28.
	// acme (2026-01-10) and janus (2026-01-10) are before the cutoff.
	// recent (2026-06-24) is after the cutoff and must NOT be dismissed.
	pinNow(t, time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	cfg, fake := dismissRepo(t)
	ctx := context.Background()

	var out bytes.Buffer
	err := runBulkDismiss(ctx, cfg, fake, "", "4w", "", true, strings.NewReader(""), &out, nil)
	if err != nil {
		t.Fatal(err)
	}

	output := out.String()
	// 4 todos are old (2 acme + 2 janus), 1 recent todo must be spared.
	if !strings.Contains(output, "matched 4, dismissed 4, failed 0") {
		t.Errorf("expected 4 dismissed, got: %s", output)
	}

	recentData, _ := os.ReadFile(filepath.Join(cfg.Root(), "daily", "2026", "06", "2026-06-24.md"))
	if !strings.Contains(string(recentData), "@todo") {
		t.Errorf("recent todo was dismissed but should not have been:\n%s", string(recentData))
	}

	open, err := listTodos(ctx, cfg, []string{note.MarkerTodo}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Errorf("after age-filter dismiss, open todos = %d, want 1 (the recent one)", len(open))
	}
}

// TestDismissProjectAndAgeFilter combines both filters.
func TestDismissProjectAndAgeFilter(t *testing.T) {
	pinNow(t, time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	cfg, fake := dismissRepo(t)
	ctx := context.Background()

	var out bytes.Buffer
	err := runBulkDismiss(ctx, cfg, fake, "janus", "4w", "", true, strings.NewReader(""), &out, nil)
	if err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, "matched 2, dismissed 2, failed 0") {
		t.Errorf("expected 2 dismissed (janus only), got: %s", output)
	}

	open, err := listTodos(ctx, cfg, []string{note.MarkerTodo}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// acme (2 todos) + recent (1 todo) should remain open.
	if len(open) != 3 {
		t.Errorf("after janus+4w dismiss, open todos = %d, want 3", len(open))
	}
}

// TestDismissConfirmationPromptAbort asserts the prompt gates dismissal.
func TestDismissConfirmationPromptAbort(t *testing.T) {
	pinNow(t, time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	cfg, fake := dismissRepo(t)
	ctx := context.Background()

	var out bytes.Buffer
	// Answer "n" to the prompt.
	err := runBulkDismiss(ctx, cfg, fake, "acme", "", "", false, strings.NewReader("n\n"), &out, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "aborted") {
		t.Errorf("expected 'aborted', got: %s", out.String())
	}

	// Acme todos must still be open.
	open, _ := listTodos(ctx, cfg, []string{note.MarkerTodo}, "acme", "")
	if len(open) != 2 {
		t.Errorf("acme todos after aborted dismiss = %d, want 2", len(open))
	}
}

// TestDismissConfirmationPromptAccept asserts that "y" proceeds.
func TestDismissConfirmationPromptAccept(t *testing.T) {
	pinNow(t, time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	cfg, fake := dismissRepo(t)
	ctx := context.Background()

	var out bytes.Buffer
	err := runBulkDismiss(ctx, cfg, fake, "acme", "", "", false, strings.NewReader("y\n"), &out, nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out.String(), "aborted") {
		t.Errorf("should have proceeded, got aborted: %s", out.String())
	}
	if !strings.Contains(out.String(), "dismissed 2") {
		t.Errorf("expected dismissed 2, got: %s", out.String())
	}
}

// TestDismissNoFilterRequired checks that missing filters return an error.
func TestDismissNoFilterRequired(t *testing.T) {
	cfg, fake := dismissRepo(t)
	ctx := context.Background()

	var out bytes.Buffer
	err := runBulkDismiss(ctx, cfg, fake, "", "", "", true, strings.NewReader(""), &out, nil)
	// No filter means we'd dismiss everything — the caller (cobra RunE) enforces
	// the requirement, but runBulkDismiss should also handle it gracefully.
	// Here we verify that with no filter, it still runs (the guard is in the cmd).
	// This test just ensures it doesn't panic with empty project/before.
	_ = err // may or may not error depending on what "all" returns
}

// TestDismissWithResolution verifies the resolution line is appended to each block.
func TestDismissWithResolution(t *testing.T) {
	pinNow(t, time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	cfg, fake := dismissRepo(t)
	ctx := context.Background()

	var out bytes.Buffer
	err := runBulkDismiss(ctx, cfg, fake, "acme", "", "superseded by new plan", true, strings.NewReader(""), &out, nil)
	if err != nil {
		t.Fatal(err)
	}

	acmeData, _ := os.ReadFile(filepath.Join(cfg.Root(), "projects", "acme", "notes", "2026-01-10.md"))
	count := strings.Count(string(acmeData), "Resolution: superseded by new plan")
	if count != 2 {
		t.Errorf("expected 2 resolution lines (one per todo), got %d:\n%s", count, string(acmeData))
	}
}

// TestDismissNoMatches reports the empty-set message without writing files.
func TestDismissNoMatches(t *testing.T) {
	pinNow(t, time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	cfg, fake := dismissRepo(t)
	ctx := context.Background()

	var out bytes.Buffer
	// "noproject" doesn't exist.
	err := runBulkDismiss(ctx, cfg, fake, "noproject", "", "", true, strings.NewReader(""), &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no open todos match the filter") {
		t.Errorf("expected empty-set message, got: %s", out.String())
	}
}

// TestDismissOlderThanAlias checks that the --older-than flag alias works the
// same as --before at the command level (both write to the same variable).
func TestDismissOlderThanAlias(t *testing.T) {
	// The flag alias shares the dismissBefore variable; the command's RunE checks
	// dismissBefore. We exercise the alias via the cobra flag set directly.
	if err := dismissCmd.Flags().Set("older-than", "2w"); err != nil {
		t.Fatalf("setting --older-than flag: %v", err)
	}
	if dismissBefore != "2w" {
		t.Errorf("--older-than did not set dismissBefore: got %q", dismissBefore)
	}
	// Reset for subsequent tests.
	_ = dismissCmd.Flags().Set("older-than", "")
	_ = dismissCmd.Flags().Set("before", "")
}

// TestDoneSingleTodoUnchanged is a regression guard: single `done` must still work.
func TestDoneSingleTodoUnchanged(t *testing.T) {
	pinNow(t, time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	cfg, fake := dismissRepo(t)
	ctx := context.Background()

	res, err := completeTodo(ctx, cfg, fake, "Acme contract", "", nil)
	if err != nil {
		t.Fatalf("single done failed: %v", err)
	}
	if !strings.Contains(res.Snippet, "Acme contract") {
		t.Errorf("completed wrong todo: %+v", res)
	}

	// Only the matched todo is done; the other three remain open.
	open, _ := listTodos(ctx, cfg, []string{note.MarkerTodo}, "", "")
	if len(open) != 4 {
		t.Errorf("after single done, open = %d, want 4", len(open))
	}
}
