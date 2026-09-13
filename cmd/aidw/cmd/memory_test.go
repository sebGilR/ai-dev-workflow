package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aidw/cmd/aidw/internal/slug"
	"aidw/cmd/aidw/internal/wip"
)

// The memory commands are repo-scoped, not branch-workflow-scoped: they need
// only the repo root and (for `store`/`list`) the branch name. Routing them
// through the .wip resolver broke /wip-document-project, which indexes docs on
// a fresh repo before any /wip-start has run. These tests pin that
// independence.

// TestRepoAndBranch_WorksWithoutWipState covers AC-B2 for the memory family:
// resolution succeeds with no .wip directory present, and creates none.
func TestRepoAndBranch_WorksWithoutWipState(t *testing.T) {
	dir := initTestGitRepo(t)

	// Sanity: the wip resolver genuinely reports no active work here, so the
	// test would have failed before the fix.
	if _, err := wip.FindBranchState(dir, ""); err == nil {
		t.Fatal("precondition: expected no active work in a fresh repo")
	}

	repo, branch, repoID, err := repoAndBranch(dir)
	if err != nil {
		t.Fatalf("repoAndBranch on a repo with no .wip: %v", err)
	}
	if repoID == "" {
		t.Error("repoID must not be empty")
	}

	wantRepo, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if repo != wantRepo {
		t.Errorf("repo = %q, want %q", repo, wantRepo)
	}
	if branch == "" {
		t.Error("branch must not be empty")
	}
	if _, err := os.Stat(filepath.Join(dir, ".wip")); !os.IsNotExist(err) {
		t.Error("memory resolution must not create a .wip directory")
	}
}

// TestRepoAndBranch_MatchesWipBranchKey guards the fact key: memory rows are
// keyed by branch, and callers that resolve a branch through the wip package
// must land on the same key. If these two slugifications ever drift, facts get
// written where they cannot be read back.
func TestRepoAndBranch_MatchesWipBranchKey(t *testing.T) {
	dir := initTestGitRepo(t)

	// A branch name that slugification actually changes.
	cmd := exec.Command("git", "checkout", "-b", "feat/release.1")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}

	_, branch, _, err := repoAndBranch(dir)
	if err != nil {
		t.Fatalf("repoAndBranch: %v", err)
	}
	if want := slug.SafeSlug("feat/release.1"); branch != want {
		t.Errorf("branch = %q, want the slugified %q", branch, want)
	}

	// And it agrees with the branch key the wip package resolves.
	state, err := wip.EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}
	if state.Branch != branch {
		t.Errorf("wip branch key %q != memory branch key %q", state.Branch, branch)
	}
}

// TestRepoAndBranch_NotAGitRepo keeps the genuine error path an error.
func TestRepoAndBranch_NotAGitRepo(t *testing.T) {
	if _, _, _, err := repoAndBranch(t.TempDir()); err == nil {
		t.Error("expected an error outside a git repo")
	}
}
