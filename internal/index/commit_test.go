package index

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommitMessage(t *testing.T) {
	ts := time.Date(2026, 6, 1, 12, 12, 0, 0, time.UTC)
	msg := CommitMessage(Stats{Embedded: 2, Updated: 1, Deleted: 0}, ts)
	if !strings.Contains(msg, "+2 new") || !strings.Contains(msg, "~1 revised") || !strings.Contains(msg, "-0 removed") {
		t.Errorf("message missing counts: %q", msg)
	}
	if !strings.Contains(msg, "2026-06-01 12:12") {
		t.Errorf("message missing timestamp: %q", msg)
	}
	// No-change case gets the synced phrasing, not "+0 new".
	noop := CommitMessage(Stats{}, ts)
	if !strings.Contains(noop, "synced notes") {
		t.Errorf("zero-change message = %q, want 'synced notes'", noop)
	}
}

func TestCaptureCommitMessage(t *testing.T) {
	msg := CaptureCommitMessage(time.Date(2026, 6, 1, 12, 53, 0, 0, time.UTC))
	if !strings.Contains(msg, "note") || !strings.Contains(msg, "2026-06-01 12:53") {
		t.Errorf("capture message = %q, want a note + timestamp", msg)
	}
}

func TestAutoCommitNoOpOutsideGitRepo(t *testing.T) {
	// A plain temp dir is not a git repo -> AutoCommit is a no-op, no error.
	committed, err := AutoCommit(t.TempDir(), Stats{Embedded: 1}, false, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if committed {
		t.Error("should not commit outside a git repo")
	}
}

func TestAutoCommitCommitsNoteChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)
	// A captured note + the gitignored index.
	mustWrite(t, filepath.Join(root, ".gitignore"), ".journal/index/\n")
	mustWrite(t, filepath.Join(root, ".journal", "index", "journal.db"), "BINARY")
	mustWrite(t, filepath.Join(root, "daily", "2026", "06", "d.md"), "# 2026-06-01\n\n## 09:00\nnote\n")

	committed, err := AutoCommit(root, Stats{Embedded: 1}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("expected a commit")
	}
	tracked := gitOut(t, root, "ls-files")
	if !strings.Contains(tracked, "daily/2026/06/d.md") {
		t.Errorf("note not committed: %s", tracked)
	}
	if strings.Contains(tracked, "journal.db") {
		t.Errorf("gitignored index was committed: %s", tracked)
	}
	// Re-running with no changes is a clean no-op.
	committed, err = AutoCommit(root, Stats{}, false, time.Now())
	if err != nil || committed {
		t.Errorf("expected no-op on clean tree, got committed=%v err=%v", committed, err)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
