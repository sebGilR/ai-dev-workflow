package git

import (
	"errors"
	"os/exec"
	"strings"
)

// run executes a git command in dir and returns trimmed stdout.
func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", errors.New(strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// IsGitRepo reports whether dir is inside a git repository.
func IsGitRepo(dir string) bool {
	_, err := run(dir, "rev-parse", "--git-dir")
	return err == nil
}

// Toplevel returns the absolute path of the repository root.
func Toplevel(dir string) (string, error) {
	return run(dir, "rev-parse", "--show-toplevel")
}

// CurrentBranch returns the name of the current branch.
// Returns "detached-head" when HEAD is not on a branch (matches Python behaviour).
func CurrentBranch(dir string) (string, error) {
	out, err := run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	if out == "HEAD" {
		return "detached-head", nil
	}
	return out, nil
}

// HeadSHA returns the full SHA of HEAD.
func HeadSHA(dir string) (string, error) {
	return run(dir, "rev-parse", "HEAD")
}

// MergeBase returns the common ancestor of ref1 and ref2.
func MergeBase(dir, ref1, ref2 string) (string, error) {
	return run(dir, "merge-base", ref1, ref2)
}
