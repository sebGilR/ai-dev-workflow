package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// initRepoAt initializes a git repo at dir (creating it) with one commit,
// and returns the symlink-resolved path.
func initRepoAt(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "init")

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func commonDir(t *testing.T, dir string) string {
	t.Helper()
	out, err := CommonDir(dir)
	if err != nil {
		t.Fatalf("CommonDir(%s): %v", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(out)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", out, err)
	}
	return resolved
}

// TestCommonDir_SameForAllWorktreesOfOneClone is the property RepoIdentity
// depends on: a linked worktree has its own --git-dir but shares one
// --git-common-dir with the repo it was added from.
func TestCommonDir_SameForAllWorktreesOfOneClone(t *testing.T) {
	repo := initRepoAt(t, filepath.Join(t.TempDir(), "repo"))
	worktree := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", worktree, "-b", "wt-branch")

	resolvedWorktree, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		t.Fatal(err)
	}

	main := commonDir(t, repo)
	linked := commonDir(t, resolvedWorktree)
	if main != linked {
		t.Fatalf("CommonDir differs across worktrees of one clone: %q vs %q", main, linked)
	}

	// Sanity check that the two worktrees really are distinct checkouts
	// (otherwise the assertion above would be vacuous).
	mainTop, err := Toplevel(repo)
	if err != nil {
		t.Fatal(err)
	}
	linkedTop, err := Toplevel(resolvedWorktree)
	if err != nil {
		t.Fatal(err)
	}
	if mainTop == linkedTop {
		t.Fatal("expected the linked worktree to have a different toplevel")
	}
}

func TestCommonDir_DiffersForIndependentClones(t *testing.T) {
	base := t.TempDir()
	// Same basename on purpose: identity must not be keyed by name.
	repoA := initRepoAt(t, filepath.Join(base, "a", "repo"))
	repoB := initRepoAt(t, filepath.Join(base, "b", "repo"))

	if commonDir(t, repoA) == commonDir(t, repoB) {
		t.Fatal("independent clones with the same basename must have different common dirs")
	}
}

func TestCommonDir_SameFromSubdirectory(t *testing.T) {
	repo := initRepoAt(t, filepath.Join(t.TempDir(), "repo"))
	sub := filepath.Join(repo, "nested", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if got, want := commonDir(t, sub), commonDir(t, repo); got != want {
		t.Fatalf("CommonDir from subdirectory = %q, want toplevel's %q", got, want)
	}
}

func TestCommonDir_IsAbsolute(t *testing.T) {
	repo := initRepoAt(t, filepath.Join(t.TempDir(), "repo"))
	sub := filepath.Join(repo, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// --path-format=absolute is what guarantees this; without it git
	// returns a bare ".git" relative to the cwd.
	out, err := CommonDir(sub)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(out) {
		t.Fatalf("CommonDir() = %q, want an absolute path", out)
	}
}

func TestCommonDir_ErrorsOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	if IsGitRepo(dir) {
		t.Skip("temp dir is unexpectedly inside a git repo")
	}
	if _, err := CommonDir(dir); err == nil {
		t.Fatal("expected CommonDir to fail outside a git repository")
	}
}
