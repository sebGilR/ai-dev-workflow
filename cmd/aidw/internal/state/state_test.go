package state

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"aidw/cmd/aidw/internal/git"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestExecutionRoot_DelegatesToGitToplevel(t *testing.T) {
	dir := initGitRepo(t)

	want, err := git.Toplevel(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExecutionRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ExecutionRoot() = %q, want %q (git.Toplevel)", got, want)
	}
}

func TestStateDir_RespectsEnvOverride(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", "/tmp/custom-aidw-state")
	if got := StateDir(); got != "/tmp/custom-aidw-state" {
		t.Fatalf("StateDir() = %q, want override", got)
	}
}

func TestStateDir_FallsBackToXDG(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	if got := StateDir(); got != filepath.Join("/tmp/xdg-state", "aidw") {
		t.Fatalf("StateDir() = %q, want XDG-based path", got)
	}
}

func TestStateDir_FallsBackToHomeLocalState(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/fake-home")
	want := filepath.Join("/tmp/fake-home", ".local", "state", "aidw")
	if got := StateDir(); got != want {
		t.Fatalf("StateDir() = %q, want %q", got, want)
	}
}

func TestRepoIdentity_SameAcrossWorktrees(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", stateDir)

	repo := initGitRepo(t)
	worktreeDir := filepath.Join(t.TempDir(), "wt")

	cmd := exec.Command("git", "worktree", "add", worktreeDir, "-b", "wt-branch")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	resolvedWorktree, err := filepath.EvalSymlinks(worktreeDir)
	if err != nil {
		t.Fatal(err)
	}

	idMain, err := RepoIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	idWorktree, err := RepoIdentity(resolvedWorktree)
	if err != nil {
		t.Fatal(err)
	}
	if idMain != idWorktree {
		t.Fatalf("RepoIdentity mismatch across worktrees: %q vs %q", idMain, idWorktree)
	}
}

func TestRepoIdentity_DifferentForIndependentRepos(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", stateDir)

	base := t.TempDir()
	repoA := filepath.Join(base, "repo")
	repoB := filepath.Join(base, "other", "repo")
	if err := os.MkdirAll(repoA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repoB, 0o755); err != nil {
		t.Fatal(err)
	}

	initRepoAt := func(dir string) string {
		t.Helper()
		run := func(args ...string) {
			t.Helper()
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		run("init")
		run("config", "user.email", "test@test.com")
		run("config", "user.name", "Test")
		if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", ".")
		run("commit", "-m", "init")
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		return resolved
	}

	rA := initRepoAt(repoA)
	rB := initRepoAt(repoB)

	idA, err := RepoIdentity(rA)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := RepoIdentity(rB)
	if err != nil {
		t.Fatal(err)
	}
	if idA == idB {
		t.Fatalf("RepoIdentity should differ for independent repos with same basename, got %q for both", idA)
	}
}

func TestRepoIdentity_NeverWritesReposJSON(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", stateDir)

	repo := initGitRepo(t)
	if _, err := RepoIdentity(repo); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(stateDir, "repos.json")); !os.IsNotExist(err) {
		t.Fatalf("RepoIdentity must not write repos.json, stat err = %v", err)
	}
}

func TestAcquireLock_SecondAcquireFailsImmediately(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "work.json")

	release, err := AcquireLock(target)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if _, err := AcquireLock(target); err == nil {
		t.Fatal("expected second AcquireLock to fail while first is held")
	}
}

func TestAcquireLock_StealsStaleLockAndReleaseWorks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "work.json")

	firstRelease, err := AcquireLock(target)
	if err != nil {
		t.Fatal(err)
	}

	lockPath := target + ".lock"
	old := time.Now().Add(-2 * LockStaleThreshold)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	secondRelease, err := AcquireLock(target)
	if err != nil {
		t.Fatalf("expected steal of stale lock to succeed: %v", err)
	}

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file should exist after steal: %v", err)
	}

	// The original (now-stale) holder's release must NOT remove the new
	// holder's lock file — the ownership-token check, not just staleness.
	firstRelease()
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("stale original holder's release() must not remove new holder's lock: %v", err)
	}

	secondRelease()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock file removed after legitimate release, stat err = %v", err)
	}
}
