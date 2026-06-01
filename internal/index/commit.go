package index

import (
	"fmt"
	"time"

	"github.com/ericmann/journal/internal/vcs"
)

// commitVerbs add a little personality to auto-commit messages.
var commitVerbs = []string{"captured", "jotted down", "logged", "scribbled", "stashed", "recorded", "filed away"}

// CommitMessage builds a short, informative, slightly-fun auto-commit message.
func CommitMessage(st Stats, t time.Time) string {
	when := t.Format("Mon 2006-01-02 15:04")
	if st.Embedded == 0 && st.Updated == 0 && st.Deleted == 0 {
		return fmt.Sprintf("📓 synced notes · %s", when)
	}
	verb := commitVerbs[int(t.Unix()%int64(len(commitVerbs)))]
	return fmt.Sprintf("📓 %s notes — +%d new, ~%d revised, -%d removed · %s",
		verb, st.Embedded, st.Updated, st.Deleted, when)
}

// AutoCommit commits note changes under root, but only if root is the top level
// of a git work tree (never committing into a parent repo). It returns
// committed=false when not a git repo or nothing changed. Callers treat any
// error as non-fatal — the markdown is always safe on disk regardless.
func AutoCommit(root string, st Stats, sign bool, t time.Time) (bool, error) {
	if !vcs.IsRepoRoot(root) {
		return false, nil
	}
	return vcs.CommitAll(root, CommitMessage(st, t), sign)
}
