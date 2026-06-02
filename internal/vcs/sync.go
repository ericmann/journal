package vcs

import (
	"fmt"
	"strconv"
	"strings"
)

// Upstream returns the configured upstream tracking branch for the current
// branch (e.g. "origin/main") and ok=true. It returns ok=false when the branch
// has no upstream — which is how journal detects "no remote configured": there
// is simply nothing to sync.
func Upstream(root string) (branch string, ok bool) {
	out, err := run(root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return "", false
	}
	branch = strings.TrimSpace(out)
	return branch, branch != ""
}

// Fetch updates remote-tracking refs for the upstream's remote. It is a no-op
// for the working tree.
func Fetch(root string) error {
	if out, err := run(root, "fetch", "--quiet"); err != nil {
		return fmt.Errorf("git fetch: %w\n%s", err, out)
	}
	return nil
}

// AheadBehind reports how many commits the current branch is ahead of and behind
// its upstream. ahead>0 means there are local commits to push; behind>0 means
// the upstream has commits to pull. Call Fetch first so the counts reflect the
// remote's latest state.
func AheadBehind(root string) (ahead, behind int, err error) {
	// `--left-right --count A...B` prints "<left>\t<right>": commits reachable
	// from the left ref only, then the right ref only. With @{u}...HEAD the left
	// is the upstream (behind) and the right is HEAD (ahead).
	out, err := run(root, "rev-list", "--left-right", "--count", "@{u}...HEAD")
	if err != nil {
		return 0, 0, fmt.Errorf("git rev-list: %w\n%s", err, out)
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output: %q", out)
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parsing behind count %q: %w", fields[0], err)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parsing ahead count %q: %w", fields[1], err)
	}
	return ahead, behind, nil
}

// Push publishes local commits to the upstream.
func Push(root string) error {
	if out, err := run(root, "push", "--quiet"); err != nil {
		return fmt.Errorf("git push: %w\n%s", err, out)
	}
	return nil
}

// MergePreferUpstream merges the upstream branch into the current branch,
// resolving any conflicts in favour of the upstream (`-X theirs`). A clean
// fast-forward or auto-merge proceeds normally; only genuine conflicts defer to
// the upstream copy. This implements the "prefer the remote backup" policy for
// unattended cron syncs. Signing is force-disabled so an unattended merge commit
// never blocks on a GPG/SSH key, matching CommitAll.
func MergePreferUpstream(root string) error {
	args := []string{
		"-c", "commit.gpgsign=false",
		"merge", "--no-edit", "-X", "theirs", "@{u}",
	}
	if out, err := run(root, args...); err != nil {
		return fmt.Errorf("git merge: %w\n%s", err, out)
	}
	return nil
}
