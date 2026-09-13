package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initWorktreeTestRepo creates a git repo at a fresh dir with one commit on
// branch, following initHookGitRepo's exact pattern
// (cmd/aidw/internal/install/hooks_test.go:123-147) — hard rule 1: `-b
// <branch>` is always passed explicitly, never a bare `git init`. Only
// local (non---global) git config is touched (hard rule 2). Deliberately
// NOT reusing this file's existing initRepoAt helper, which still uses a
// bare `git init` with no branch flag — exactly the pattern hard rule 1
// forbids for new tests.
func initWorktreeTestRepo(t *testing.T, dir, branch string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}
	initArgs := []string{"init", "-q"}
	if branch != "" {
		initArgs = append(initArgs, "-b", branch)
	}
	run(initArgs...)
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
}

func TestWorktreeList_IncludesMainAndAddedWorktrees(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	initWorktreeTestRepo(t, repo, "main")

	wtPath := filepath.Join(base, "wt2")
	cmd := exec.Command("git", "worktree", "add", "-b", "second-branch", wtPath)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	paths, err := WorktreeList(repo)
	if err != nil {
		t.Fatal(err)
	}

	repoResolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	wtResolved, err := filepath.EvalSymlinks(wtPath)
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, p := range paths {
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		found[resolved] = true
	}
	if !found[repoResolved] {
		t.Fatalf("WorktreeList() = %v, missing main worktree %s", paths, repo)
	}
	if !found[wtResolved] {
		t.Fatalf("WorktreeList() = %v, missing added worktree %s", paths, wtPath)
	}
	if len(paths) != 2 {
		t.Fatalf("WorktreeList() returned %d paths, want 2: %v", len(paths), paths)
	}
}

func TestWorktreeList_SkipsBareRepo(t *testing.T) {
	base := t.TempDir()
	bareDir := filepath.Join(base, "bare.git")

	cmd := exec.Command("git", "init", "-q", "--bare", "-b", "main", bareDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	paths, err := WorktreeList(bareDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("WorktreeList() on a bare repo = %v, want empty (bare has no working tree)", paths)
	}
}
