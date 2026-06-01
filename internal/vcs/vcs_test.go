package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a git repo at dir with identity configured, or skips the
// test if git is unavailable.
func initRepo(t *testing.T) string {
	t.Helper()
	if !Available() {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := run(dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsRepoRoot(t *testing.T) {
	dir := initRepo(t)
	if !IsRepoRoot(dir) {
		t.Error("expected repo root to be detected")
	}
	// A non-repo temp dir is not a repo root.
	if IsRepoRoot(t.TempDir()) {
		t.Error("non-repo dir reported as repo root")
	}
	// A subdirectory of a repo is NOT the top level.
	sub := filepath.Join(dir, "daily")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if IsRepoRoot(sub) {
		t.Error("subdirectory reported as repo root (would commit into parent repo)")
	}
}

func TestCommitAllCommitsAndIsNoOpWhenClean(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "daily/2026/06/d.md", "# 2026-06-01\n\n## 09:00\nfirst note\n")

	committed, err := CommitAll(dir, "📓 first", false)
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("expected a commit")
	}
	if out, _ := run(dir, "log", "--oneline"); !strings.Contains(out, "first") {
		t.Errorf("commit not in log: %s", out)
	}

	// Nothing changed -> no commit.
	committed, err = CommitAll(dir, "📓 noop", false)
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Error("expected no commit when working tree is clean")
	}
}

func TestCommitAllHonorsGitignore(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, ".gitignore", ".journal/index/\n")
	writeFile(t, dir, ".journal/index/journal.db", "BINARY")
	writeFile(t, dir, "daily/d.md", "note")

	if _, err := CommitAll(dir, "📓 notes", false); err != nil {
		t.Fatal(err)
	}
	// The gitignored index must not be tracked.
	out, _ := run(dir, "ls-files")
	if strings.Contains(out, "journal.db") {
		t.Errorf("gitignored index was committed:\n%s", out)
	}
	if !strings.Contains(out, "daily/d.md") {
		t.Errorf("note was not committed:\n%s", out)
	}
}

func TestCommitAllUnsignedDoesNotRequireKey(t *testing.T) {
	dir := initRepo(t)
	// Force signing ON in config, but call with sign=false; it must still commit
	// (no signing key/agent needed) because we override gpgsign per-commit.
	if out, err := run(dir, "config", "commit.gpgsign", "true"); err != nil {
		t.Fatalf("config: %v\n%s", err, out)
	}
	if out, err := run(dir, "config", "gpg.format", "ssh"); err != nil {
		t.Fatalf("config: %v\n%s", err, out)
	}
	writeFile(t, dir, "daily/d.md", "note")
	committed, err := CommitAll(dir, "📓 unsigned", false)
	if err != nil {
		t.Fatalf("unsigned commit should succeed without a key: %v", err)
	}
	if !committed {
		t.Error("expected a commit")
	}
}

// guard against accidentally depending on a globally-installed git in a way
// that breaks when absent.
func TestAvailableMatchesLookPath(t *testing.T) {
	_, err := exec.LookPath("git")
	if Available() != (err == nil) {
		t.Error("Available() disagrees with exec.LookPath")
	}
}
