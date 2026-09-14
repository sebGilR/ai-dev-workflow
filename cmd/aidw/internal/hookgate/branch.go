package hookgate

import (
	"os"
	"os/exec"
	"strings"

	"aidw/cmd/aidw/internal/git"
)

// statFunc is os.Stat, aliased so isDir has a single call site to swap in
// tests if ever needed.
var statFunc = os.Stat

// resolveGateBranch resolves the current branch name in a way that works
// even on an unborn HEAD (a git repo with zero commits), unlike
// git.CurrentBranch's underlying `rev-parse --abbrev-ref HEAD`, which fails
// with "ambiguous argument 'HEAD'" before the first commit. Passing a
// non-empty branch name to wip.FindBranchState skips its internal
// git.CurrentBranch call (wip.resolveBranchName only invokes it when branch
// == ""), so this keeps zero-commit repos correctly routed to
// ErrNoActiveWork instead of the generic-error fail-open path.
func resolveGateBranch(repoTop string) (string, error) {
	out, err := exec.Command("git", "-C", repoTop, "symbolic-ref", "-q", "--short", "HEAD").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	return git.CurrentBranch(repoTop)
}
