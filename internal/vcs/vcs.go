// Package vcs provides the minimal git operations journal needs to auto-commit
// note changes. It shells out to the `git` binary and is deliberately scoped:
// it only ever commits a repo whose top level is exactly the given root, so it
// can never accidentally commit into a parent repository.
package vcs

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
)

// Available reports whether the git binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// IsRepoRoot reports whether dir is the top level of a git work tree (not merely
// inside one). This guards against committing a notes folder that is nested in
// some larger repository.
func IsRepoRoot(dir string) bool {
	if !Available() {
		return false
	}
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	top, err1 := filepath.EvalSymlinks(strings.TrimSpace(out))
	want, err2 := filepath.EvalSymlinks(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	return top == want
}

// CommitAll stages every (non-ignored) change under root and commits it with
// message. It returns committed=false (and nil error) when there is nothing to
// commit. When sign is false, signing is force-disabled for this commit
// regardless of the repo's commit.gpgsign setting.
func CommitAll(root, message string, sign bool) (committed bool, err error) {
	if _, err := run(root, "add", "-A"); err != nil {
		return false, err
	}
	// Nothing staged? `diff --cached --quiet` exits 0 when the index matches HEAD.
	if _, err := run(root, "diff", "--cached", "--quiet"); err == nil {
		return false, nil
	}
	args := []string{}
	if !sign {
		args = append(args, "-c", "commit.gpgsign=false")
	}
	args = append(args, "commit", "-m", message)
	if _, err := run(root, args...); err != nil {
		return false, err
	}
	return true, nil
}

// run executes `git -C dir <args...>` and returns combined output.
func run(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}
