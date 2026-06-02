package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/vcs"
)

// gitC runs a git command in dir and fails the test on error.
func gitC(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// journalRepoWithRemote creates an initialized journal repo that is a git repo
// tracking a bare remote, with the seed commit pushed and upstream configured.
func journalRepoWithRemote(t *testing.T) (cfg *config.Config, remote, root string) {
	t.Helper()
	if !vcs.Available() {
		t.Skip("git not available")
	}
	base := t.TempDir()
	remote = filepath.Join(base, "remote.git")
	gitC(t, base, "init", "--bare", "-q", remote)

	root = filepath.Join(base, "primary")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitC(t, root, "init", "-q")
	gitC(t, root, "config", "user.email", "test@example.com")
	gitC(t, root, "config", "user.name", "Test")
	gitC(t, root, "config", "commit.gpgsign", "false")
	if _, err := initRepo(root); err != nil {
		t.Fatal(err)
	}
	gitC(t, root, "remote", "add", "origin", remote)
	gitC(t, root, "add", "-A")
	gitC(t, root, "commit", "-q", "-m", "seed")
	gitC(t, root, "push", "-u", "-q", "origin", "HEAD")

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, remote, root
}

func TestRunSyncNoUpstreamIsNoOp(t *testing.T) {
	if !vcs.Available() {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitC(t, root, "init", "-q")
	gitC(t, root, "config", "user.email", "test@example.com")
	gitC(t, root, "config", "user.name", "Test")
	gitC(t, root, "config", "commit.gpgsign", "false")
	if _, err := initRepo(root); err != nil {
		t.Fatal(err)
	}
	gitC(t, root, "add", "-A")
	gitC(t, root, "commit", "-q", "-m", "seed")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runSync(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), false, &buf); err != nil {
		t.Fatalf("no-upstream sync should be a clean no-op: %v", err)
	}
	if !strings.Contains(buf.String(), "no upstream") {
		t.Errorf("expected no-upstream message, got: %s", buf.String())
	}
}

func TestRunSyncPushesWhenAhead(t *testing.T) {
	cfg, remote, root := journalRepoWithRemote(t)

	// A new, committed note puts the branch ahead of the remote.
	if err := os.WriteFile(filepath.Join(root, "daily", "note.md"), []byte("# note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitC(t, root, "add", "-A")
	gitC(t, root, "commit", "-q", "-m", "note")

	var buf bytes.Buffer
	if err := runSync(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), false, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "pushed") {
		t.Errorf("expected a push, got: %s", buf.String())
	}
	// The remote now has the note: a fresh clone sees it.
	check := filepath.Join(t.TempDir(), "verify")
	gitC(t, t.TempDir(), "clone", "-q", remote, check)
	if _, err := os.Stat(filepath.Join(check, "daily", "note.md")); err != nil {
		t.Errorf("pushed note not found on remote: %v", err)
	}
}

func TestRunSyncPullsAndReindexesWhenBehind(t *testing.T) {
	cfg, remote, root := journalRepoWithRemote(t)

	// Another clone pushes a note, leaving the primary behind.
	other := filepath.Join(t.TempDir(), "other")
	gitC(t, t.TempDir(), "clone", "-q", remote, other)
	gitC(t, other, "config", "user.email", "other@example.com")
	gitC(t, other, "config", "user.name", "Other")
	gitC(t, other, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(other, "daily"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "daily", "remote.md"), []byte("# 2026-06-01\n\n## 09:00\nfrom afar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitC(t, other, "add", "-A")
	gitC(t, other, "commit", "-q", "-m", "remote note")
	gitC(t, other, "push", "-q")

	fake := embed.NewFake(cfg.EmbedDim)
	var buf bytes.Buffer
	if err := runSync(context.Background(), cfg, fake, false, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "merged") {
		t.Errorf("expected a merge/pull, got: %s", buf.String())
	}
	// The pulled note is on disk and was embedded by the re-index step.
	if _, err := os.Stat(filepath.Join(root, "daily", "remote.md")); err != nil {
		t.Errorf("pulled note missing on disk: %v", err)
	}
	if fake.EmbedTexts == 0 {
		t.Error("expected re-index to embed the pulled note")
	}
}

func TestRunSyncDryRunReportsWithoutPushing(t *testing.T) {
	cfg, remote, root := journalRepoWithRemote(t)

	if err := os.WriteFile(filepath.Join(root, "daily", "note.md"), []byte("# note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitC(t, root, "add", "-A")
	gitC(t, root, "commit", "-q", "-m", "note")

	var buf bytes.Buffer
	if err := runSync(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), true, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "[dry-run]") {
		t.Errorf("expected dry-run notice, got: %s", buf.String())
	}
	// Nothing was pushed: a fresh clone must NOT have the note.
	check := filepath.Join(t.TempDir(), "verify")
	gitC(t, t.TempDir(), "clone", "-q", remote, check)
	if _, err := os.Stat(filepath.Join(check, "daily", "note.md")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not push; remote unexpectedly has the note (err=%v)", err)
	}
}
