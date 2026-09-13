package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepoWithBranch creates a git repo at a fresh t.TempDir() with one
// commit on branch, following initHookGitRepo's exact pattern
// (cmd/aidw/internal/install/hooks_test.go:123-147) — hard rule 1: `-b
// <branch>` is always passed explicitly, never a bare `git init`. This
// package's own initTestGitRepo (resolve_test.go) uses a bare `git init` and
// is explicitly named in spec.md's hard rule 1 exclusion list — do not reuse
// it for new tests; write against this helper instead.
func initGitRepoWithBranch(t *testing.T, branch string) string {
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
	run("init", "-q", "-b", branch)
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// --- resolveMigrateRoots: item 3(a) — multiple worktrees ---

func TestResolveMigrateRoots_MultipleWorktreesBothIncluded(t *testing.T) {
	repo := initGitRepoWithBranch(t, "main")
	worktreeDir := filepath.Join(filepath.Dir(repo), "wt2")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature", worktreeDir)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	resolvedWt, err := filepath.EvalSymlinks(worktreeDir)
	if err != nil {
		t.Fatal(err)
	}

	roots, err := resolveMigrateRoots(repo, nil, false)
	if err != nil {
		t.Fatalf("resolveMigrateRoots: %v", err)
	}

	seen := map[string]bool{}
	for _, r := range roots {
		resolved, err := filepath.EvalSymlinks(r)
		if err != nil {
			t.Fatal(err)
		}
		seen[resolved] = true
	}
	if !seen[repo] {
		t.Errorf("expected main worktree %s in roots, got %+v", repo, roots)
	}
	if !seen[resolvedWt] {
		t.Errorf("expected second worktree %s in roots, got %+v", resolvedWt, roots)
	}
}

// --- resolveMigrateRoots: item 3(b) — duplicate --path is deduped ---

func TestResolveMigrateRoots_DuplicatePathNotDoubled(t *testing.T) {
	repo := initGitRepoWithBranch(t, "main")

	// Pass the main worktree's own path again via --path — git.WorktreeList
	// already includes it, so this must not produce a second entry.
	roots, err := resolveMigrateRoots(repo, []string{repo}, false)
	if err != nil {
		t.Fatalf("resolveMigrateRoots: %v", err)
	}

	count := 0
	for _, r := range roots {
		resolved, err := filepath.EvalSymlinks(r)
		if err != nil {
			t.Fatal(err)
		}
		if resolved == repo {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of %s in roots, got %d: %+v", repo, count, roots)
	}
}

// --- resolveMigrateRoots: review.md #8 — a symlinked alias of an already-
// discovered worktree must dedup against it, not double it. ---

func TestResolveMigrateRoots_SymlinkedPathDedupedWithRealPath(t *testing.T) {
	repo := initGitRepoWithBranch(t, "main")

	// A symlink alias of the main worktree, alongside a parent dir that
	// itself isn't the repo — mirrors macOS's real /tmp -> /private/tmp
	// case: two distinct absolute spellings of the SAME physical
	// directory. filepath.Abs alone would treat these as different roots;
	// filepath.EvalSymlinks resolves both to the same key.
	aliasParent := t.TempDir()
	alias := filepath.Join(aliasParent, "repo-alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}

	roots, err := resolveMigrateRoots(repo, []string{alias}, false)
	if err != nil {
		t.Fatalf("resolveMigrateRoots: %v", err)
	}

	count := 0
	for _, r := range roots {
		resolved, err := filepath.EvalSymlinks(r)
		if err != nil {
			t.Fatal(err)
		}
		if resolved == repo {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected the symlinked alias to dedup with the real worktree path, got %d occurrences: %+v", count, roots)
	}
}

// --- resolveMigrateRoots: item 3(c) — a nonexistent --path is skipped, not fatal ---

func TestResolveMigrateRoots_NonexistentPathSkippedNotFatal(t *testing.T) {
	repo := initGitRepoWithBranch(t, "main")
	bogus := filepath.Join(repo, "does-not-exist")

	roots, err := resolveMigrateRoots(repo, []string{bogus}, false)
	if err != nil {
		t.Fatalf("resolveMigrateRoots must not error on an unresolvable --path, got: %v", err)
	}
	for _, r := range roots {
		if r == bogus {
			t.Errorf("nonexistent path %s must not appear in roots: %+v", bogus, roots)
		}
	}
	// The real worktree must still be present — one bad --path must not
	// drop everything else.
	found := false
	for _, r := range roots {
		resolved, err := filepath.EvalSymlinks(r)
		if err != nil {
			t.Fatal(err)
		}
		if resolved == repo {
			found = true
		}
	}
	if !found {
		t.Errorf("expected real worktree %s to still be present, got %+v", repo, roots)
	}
}

// --- item 1's fix: --dry-run without --cleanup-sources is refused, not
// silently discarded into a real migration. Must run the real binary in a
// subprocess (Die calls os.Exit directly). ---

func TestMigrateState_DryRunWithoutCleanupSources_Refused(t *testing.T) {
	repo := initGitRepoWithBranch(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	if err := os.MkdirAll(wipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statusJSON := `{"repo":"r","repo_path":"` + repo + `","branch":"main","stage":"started","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(wipDir, "status.json"), []byte(statusJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	cmd := exec.Command(buildAidw(t), "migrate-state", ".", "--dry-run")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "AIDW_STATE_DIR="+stateDir)
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("run aidw: %v", err)
		}
	}

	if exitCode == 0 {
		t.Fatalf("expected non-zero exit for --dry-run without --cleanup-sources, got 0. Output:\n%s", out)
	}

	// The refusal must actually prevent the migration — no work record may
	// have been created.
	workRoot := filepath.Join(stateDir, "work")
	entries, err := os.ReadDir(workRoot)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--dry-run without --cleanup-sources must not perform a real migration, but found %d work record(s)", len(entries))
	}
}
