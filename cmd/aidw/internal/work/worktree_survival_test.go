package work

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aidw/cmd/aidw/internal/git"
)

// initGitRepoForSurvivalTest mirrors wip_test.go's initGitRepo helper,
// normalized with filepath.EvalSymlinks immediately after creation: macOS
// t.TempDir() returns a /var/folders/... path that is a symlink to
// /private/var/folders/...; `git rev-parse` resolves the real path, so
// comparisons must normalize both sides or they spuriously fail.
func initGitRepoForSurvivalTest(t *testing.T) string {
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

// TestWorktreeSurvival_RecordOutlivesRemovedWorktree is AC-G scenario 3,
// the design's core value proposition: a work record's attachment data
// must remain readable after the worktree it was attached to is removed,
// because the state store lives entirely outside any worktree.
func TestWorktreeSurvival_RecordOutlivesRemovedWorktree(t *testing.T) {
	repo := initGitRepoForSurvivalTest(t)

	worktreeParent := t.TempDir()
	worktreePath := filepath.Join(worktreeParent, "wt")

	cmd := exec.Command("git", "worktree", "add", worktreePath, "-b", "survival-branch")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	resolvedWorktree, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		t.Fatal(err)
	}

	head, err := git.HeadSHA(resolvedWorktree)
	if err != nil {
		t.Fatal(err)
	}

	// Point AIDW_STATE_DIR at a directory outside both the main repo and
	// the new worktree, per the design's "durable history never lives
	// under any worktree" invariant.
	stateDir := t.TempDir()

	r := New("survives worktree removal", ModeDelivery)
	r.Attachments = append(r.Attachments, Attachment{
		RepoID:       "survival-repo",
		WorktreePath: resolvedWorktree,
		Branch:       "survival-branch",
		Head:         head,
	})
	if err := Save(stateDir, r); err != nil {
		t.Fatal(err)
	}

	// --force is safe here: the worktree has no uncommitted changes (the
	// initial commit is inherited from the parent repo and nothing has
	// been modified in the worktree checkout), so there is nothing this
	// test would silently discard.
	removeCmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	removeCmd.Dir = repo
	if out, err := removeCmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree remove: %v\n%s", err, out)
	}

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree directory to be gone, stat err = %v", err)
	}

	// The actual read-back: work.Load must still succeed and return the
	// same attachment data even though the worktree it points at is gone.
	loaded, err := Load(stateDir, r.WorkID)
	if err != nil {
		t.Fatalf("Load after worktree removal failed: %v", err)
	}
	if len(loaded.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(loaded.Attachments))
	}
	got := loaded.Attachments[0]
	if got.WorktreePath != resolvedWorktree || got.Branch != "survival-branch" || got.Head != head {
		t.Fatalf("attachment data changed after worktree removal: %+v", got)
	}
}
