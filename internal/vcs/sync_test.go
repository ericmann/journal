package vcs

import (
	"os"
	"path/filepath"
	"testing"
)

// git runs a git command in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := run(dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

// configIdentity sets a commit identity and disables signing in dir.
func configIdentity(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "commit.gpgsign", "false")
}

// setupRemote creates a bare remote plus a primary clone that has one commit
// pushed and an upstream tracking branch configured. It returns the bare remote
// path and the primary clone path.
func setupRemote(t *testing.T) (remote, primary string) {
	t.Helper()
	if !Available() {
		t.Skip("git not available")
	}
	base := t.TempDir()
	remote = filepath.Join(base, "remote.git")
	if out, err := run(base, "init", "--bare", "-q", remote); err != nil {
		t.Fatalf("init bare: %v\n%s", err, out)
	}

	primary = filepath.Join(base, "primary")
	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, primary, "init", "-q")
	configIdentity(t, primary)
	git(t, primary, "remote", "add", "origin", remote)
	writeFile(t, primary, "daily/d.md", "# seed\n")
	git(t, primary, "add", "-A")
	git(t, primary, "commit", "-q", "-m", "seed")
	// Push and set upstream without assuming the branch name (main vs master).
	git(t, primary, "push", "-u", "-q", "origin", "HEAD")
	return remote, primary
}

// cloneFrom makes a second working clone of remote with an identity configured.
func cloneFrom(t *testing.T, remote string) string {
	t.Helper()
	base := t.TempDir()
	dest := filepath.Join(base, "clone")
	git(t, base, "clone", "-q", remote, dest)
	configIdentity(t, dest)
	return dest
}

func TestUpstreamDetectsTrackingBranch(t *testing.T) {
	_, primary := setupRemote(t)
	if _, ok := Upstream(primary); !ok {
		t.Error("expected an upstream to be detected on the primary clone")
	}

	// A repo with no upstream reports ok=false (journal's "no remote" signal).
	dir := initRepo(t)
	writeFile(t, dir, "daily/d.md", "note")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "local")
	if branch, ok := Upstream(dir); ok {
		t.Errorf("expected no upstream, got %q", branch)
	}
}

func TestAheadBehindAndPush(t *testing.T) {
	_, primary := setupRemote(t)

	// Fresh after push: in sync.
	if err := Fetch(primary); err != nil {
		t.Fatal(err)
	}
	if a, b, err := AheadBehind(primary); err != nil || a != 0 || b != 0 {
		t.Fatalf("expected 0/0, got ahead=%d behind=%d err=%v", a, b, err)
	}

	// Local commit -> ahead by one.
	writeFile(t, primary, "daily/e.md", "second\n")
	git(t, primary, "add", "-A")
	git(t, primary, "commit", "-q", "-m", "second")
	if a, b, err := AheadBehind(primary); err != nil || a != 1 || b != 0 {
		t.Fatalf("expected ahead=1 behind=0, got ahead=%d behind=%d err=%v", a, b, err)
	}

	// Push clears the ahead count.
	if err := Push(primary); err != nil {
		t.Fatal(err)
	}
	if a, b, err := AheadBehind(primary); err != nil || a != 0 || b != 0 {
		t.Fatalf("after push expected 0/0, got ahead=%d behind=%d err=%v", a, b, err)
	}
}

func TestMergePreferUpstreamFastForward(t *testing.T) {
	remote, primary := setupRemote(t)
	other := cloneFrom(t, remote)

	// The other clone pushes a new commit.
	writeFile(t, other, "daily/f.md", "from other\n")
	git(t, other, "add", "-A")
	git(t, other, "commit", "-q", "-m", "from other")
	git(t, other, "push", "-q")

	// Primary is now behind; fetch + merge fast-forwards cleanly.
	if err := Fetch(primary); err != nil {
		t.Fatal(err)
	}
	if a, b, _ := AheadBehind(primary); a != 0 || b != 1 {
		t.Fatalf("expected ahead=0 behind=1, got %d/%d", a, b)
	}
	if err := MergePreferUpstream(primary); err != nil {
		t.Fatal(err)
	}
	if a, b, _ := AheadBehind(primary); a != 0 || b != 0 {
		t.Fatalf("after merge expected 0/0, got %d/%d", a, b)
	}
	if _, err := os.Stat(filepath.Join(primary, "daily/f.md")); err != nil {
		t.Errorf("pulled file missing after merge: %v", err)
	}
}

func TestMergePreferUpstreamResolvesConflictTowardUpstream(t *testing.T) {
	remote, primary := setupRemote(t)
	other := cloneFrom(t, remote)

	const conflicted = "daily/d.md"

	// Both clones edit the same line; the remote wins on conflict.
	writeFile(t, other, conflicted, "REMOTE WINS\n")
	git(t, other, "add", "-A")
	git(t, other, "commit", "-q", "-m", "remote edit")
	git(t, other, "push", "-q")

	writeFile(t, primary, conflicted, "local edit\n")
	git(t, primary, "add", "-A")
	git(t, primary, "commit", "-q", "-m", "local edit")

	if err := Fetch(primary); err != nil {
		t.Fatal(err)
	}
	if a, b, _ := AheadBehind(primary); a != 1 || b != 1 {
		t.Fatalf("expected diverged ahead=1 behind=1, got %d/%d", a, b)
	}
	if err := MergePreferUpstream(primary); err != nil {
		t.Fatalf("merge should auto-resolve, not fail: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(primary, conflicted))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "REMOTE WINS\n" {
		t.Errorf("conflict should resolve to upstream, got %q", got)
	}
}
