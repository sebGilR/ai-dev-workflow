package wip

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// initGitRepo creates a git repo with an initial commit so that
// git rev-parse calls succeed in all git versions.
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

	return dir
}

// ── EnsureBranchState ────────────────────────────────────────────────────────

func TestEnsureBranchState_CreatesDatedDir(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "my-feature")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	// WipDir must exist and match YYYYMMDDHHMMSS-my-feature pattern
	if _, err := os.Stat(state.WipDir); err != nil {
		t.Fatalf("WipDir does not exist: %v", err)
	}
	base := filepath.Base(state.WipDir)
	pattern := regexp.MustCompile(`^\d{14}-my-feature$`)
	if !pattern.MatchString(base) {
		t.Errorf("WipDir name %q does not match dated pattern", base)
	}
}

func TestEnsureBranchState_FindsExistingDatedDir(t *testing.T) {
	dir := initGitRepo(t)

	// First call creates a dir
	state1, err := EnsureBranchState(dir, "feat-a")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Write a sentinel file into the dated dir
	sentinel := filepath.Join(state1.WipDir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second call must return the same directory
	state2, err := EnsureBranchState(dir, "feat-a")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if state1.WipDir != state2.WipDir {
		t.Errorf("second call returned different WipDir: %q vs %q", state1.WipDir, state2.WipDir)
	}
	// Sentinel file must still be present (not overwritten by a new dir)
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("sentinel file missing after second call: %v", err)
	}
}

func TestEnsureBranchState_LegacyDirFallback(t *testing.T) {
	dir := initGitRepo(t)

	// Manually create a legacy (un-dated) dir
	legacyDir := filepath.Join(dir, ".wip", "my-branch")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(legacyDir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := EnsureBranchState(dir, "my-branch")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}
	// Use os.SameFile to compare because macOS /var is a symlink to /private/var
	wantInfo, err1 := os.Stat(legacyDir)
	gotInfo, err2 := os.Stat(state.WipDir)
	if err1 != nil || err2 != nil || !os.SameFile(wantInfo, gotInfo) {
		t.Errorf("expected legacy dir %q, got %q", legacyDir, state.WipDir)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("sentinel file missing: %v", err)
	}
}

func TestEnsureBranchState_SeedsStatusJSON(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "seed-test")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	statusPath := filepath.Join(state.WipDir, "status.json")
	if _, err := os.Stat(statusPath); err != nil {
		t.Fatalf("status.json missing: %v", err)
	}

	// stage must be "started" on fresh init
	stage := state.Status.Stage
	if stage != "started" {
		t.Errorf("expected stage=started, got %q", stage)
	}
	// branch must match what we passed
	branch := state.Status.Branch
	if branch != "seed-test" {
		t.Errorf("expected branch=seed-test, got %q", branch)
	}
}

func TestEnsureBranchState_SeedsWIPFiles(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "file-seed")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	for _, f := range wipFiles {
		p := filepath.Join(state.WipDir, f)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("WIP file %q missing: %v", f, err)
		}
	}
}

func TestEnsureBranchState_ContextMdUpdated(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "ctx-test")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(state.WipDir, "context.md"))
	if err != nil {
		t.Fatalf("read context.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "ctx-test") {
		t.Errorf("context.md does not contain branch name; got:\n%s", content)
	}
}

// ── SetStage ─────────────────────────────────────────────────────────────────

func TestSetStage_ValidTransition(t *testing.T) {
	dir := initGitRepo(t)
	// pre-initialize so the current branch (main/master) has a wip dir
	if _, err := EnsureBranchState(dir, ""); err != nil {
		t.Fatalf("pre-init: %v", err)
	}

	result, err := SetStage(dir, "planned", true)
	if err != nil {
		t.Fatalf("SetStage: %v", err)
	}
	stage := result.Stage
	if stage != "planned" {
		t.Errorf("expected stage=planned, got %q", stage)
	}
}

func TestSetStage_InvalidStage(t *testing.T) {
	dir := initGitRepo(t)

	_, err := SetStage(dir, "nonexistent-stage", true)
	if err == nil {
		t.Fatal("expected error for invalid stage, got nil")
	}
}

func TestSetStage_VerificationBlocksMissingFile(t *testing.T) {
	dir := initGitRepo(t)
	if _, err := EnsureBranchState(dir, ""); err != nil {
		t.Fatalf("pre-init: %v", err)
	}

	// "planned" stage requires a non-empty plan.md — the seeded file is too small
	_, err := SetStage(dir, "planned", false)
	if err == nil {
		t.Fatal("expected verification error for empty plan.md, got nil")
	}
}

func TestSetStage_SkipVerificationBypasses(t *testing.T) {
	dir := initGitRepo(t)
	if _, err := EnsureBranchState(dir, ""); err != nil {
		t.Fatalf("pre-init: %v", err)
	}

	result, err := SetStage(dir, "planned", true)
	if err != nil {
		t.Fatalf("SetStage with skip-verification: %v", err)
	}
	if result.Stage != "planned" {
		t.Errorf("expected planned, got %v", result.Stage)
	}
}

// ── ClearWip ─────────────────────────────────────────────────────────────────

func TestClearWip_KeepsMostRecentDatedDir(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	if err := os.MkdirAll(wipBase, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create three dated dirs — older 8-digit, newer 8-digit, and newest 14-digit
	oldest := filepath.Join(wipBase, "20260101-feat")
	older := filepath.Join(wipBase, "20260312-feat")
	newest := filepath.Join(wipBase, "20260312150000-feat")
	for _, d := range []string{oldest, older, newest} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ClearWip(dir, false)
	if err != nil {
		t.Fatalf("ClearWip: %v", err)
	}

	if result.Kept == nil || *result.Kept != "20260312150000-feat" {
		t.Errorf("expected kept=20260312150000-feat, got %v", result.Kept)
	}

	// Older dirs must be gone
	for _, d := range []string{oldest, older} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("dir %s should have been deleted", d)
		}
	}
	// Newest dir must remain
	if _, err := os.Stat(newest); err != nil {
		t.Errorf("newest dir should still exist: %v", err)
	}
}

func TestClearWip_DeletesLegacyDirs(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	if err := os.MkdirAll(wipBase, 0o755); err != nil {
		t.Fatal(err)
	}

	dated := filepath.Join(wipBase, "20260312-main")
	legacy := filepath.Join(wipBase, "old-branch-name")
	for _, d := range []string{dated, legacy} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ClearWip(dir, false)
	if err != nil {
		t.Fatalf("ClearWip: %v", err)
	}

	found := false
	for _, del := range result.Deleted {
		if del == "old-branch-name" {
			found = true
		}
	}
	if !found {
		t.Error("expected legacy dir to be in Deleted list")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("legacy dir should have been deleted")
	}
}

func TestClearWip_PreservesLastLegacyDirWhenNoDated(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	if err := os.MkdirAll(wipBase, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create two legacy dirs, no dated dirs
	older := filepath.Join(wipBase, "aaa-branch")
	newer := filepath.Join(wipBase, "zzz-branch")
	for _, d := range []string{older, newer} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ClearWip(dir, false)
	if err != nil {
		t.Fatalf("ClearWip: %v", err)
	}

	// Should keep the alphabetically last legacy dir
	if result.Kept == nil || *result.Kept != "zzz-branch" {
		t.Errorf("expected kept=zzz-branch, got %v", result.Kept)
	}
	if _, err := os.Stat(older); !os.IsNotExist(err) {
		t.Error("older legacy dir should have been deleted")
	}
	if _, err := os.Stat(newer); err != nil {
		t.Errorf("newer legacy dir should still exist: %v", err)
	}
}

func TestClearWip_EmptyWip(t *testing.T) {
	dir := initGitRepo(t)

	result, err := ClearWip(dir, false)
	if err != nil {
		t.Fatalf("ClearWip on empty wip: %v", err)
	}
	if result.Kept != nil {
		t.Errorf("expected Kept=nil, got %v", *result.Kept)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected no deletions, got %v", result.Deleted)
	}
}

// ── ClearOtherBranches ───────────────────────────────────────────────────────

func TestClearOtherBranches_KeepsCurrentBranchDir(t *testing.T) {
	dir := initGitRepo(t)

	// Get the current branch's wip dir first
	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	// Add an extra branch dir to be deleted
	wipBase := filepath.Dir(state.WipDir)
	other := filepath.Join(wipBase, "20260101-other-branch")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := ClearOtherBranches(dir, false)
	if err != nil {
		t.Fatalf("ClearOtherBranches: %v", err)
	}

	keepName := filepath.Base(state.WipDir)
	if result.Kept == nil || *result.Kept != keepName {
		t.Errorf("expected kept=%s, got %v", keepName, result.Kept)
	}

	// Current branch dir must remain
	if _, err := os.Stat(state.WipDir); err != nil {
		t.Errorf("current branch dir should still exist: %v", err)
	}
	// Other dir must be gone
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Error("other branch dir should have been deleted")
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != "20260101-other-branch" {
		t.Errorf("expected Deleted=[20260101-other-branch], got %v", result.Deleted)
	}
}

func TestClearOtherBranches_PreservesAllFilesInCurrentDir(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	// Write several files into the current branch dir
	files := []string{"plan.md", "context.md", "execution.md", "pr.md", "extra.txt"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(state.WipDir, f), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Add another branch dir to be deleted
	wipBase := filepath.Dir(state.WipDir)
	other := filepath.Join(wipBase, "20260101-other-branch")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := ClearOtherBranches(dir, false); err != nil {
		t.Fatalf("ClearOtherBranches: %v", err)
	}

	// All files in kept dir must survive
	for _, f := range files {
		p := filepath.Join(state.WipDir, f)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("file %s should still exist: %v", f, err)
		}
	}
}

func TestClearOtherBranches_DeletesLegacyDirs(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	// Add a legacy (undated) dir
	wipBase := filepath.Dir(state.WipDir)
	legacy := filepath.Join(wipBase, "old-branch-name")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := ClearOtherBranches(dir, false)
	if err != nil {
		t.Fatalf("ClearOtherBranches: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("legacy dir should have been deleted")
	}
	found := false
	for _, d := range result.Deleted {
		if d == "old-branch-name" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected old-branch-name in Deleted, got %v", result.Deleted)
	}
}

func TestClearOtherBranches_NothingToDelete(t *testing.T) {
	dir := initGitRepo(t)

	// Initialize the current branch so .wip exists
	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	result, err := ClearOtherBranches(dir, false)
	if err != nil {
		t.Fatalf("ClearOtherBranches on empty wip: %v", err)
	}

	keepName := filepath.Base(state.WipDir)
	if result.Kept == nil || *result.Kept != keepName {
		t.Errorf("expected kept=%s, got %v", keepName, result.Kept)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected no deletions, got %v", result.Deleted)
	}
}

func TestClearOtherBranches_DeletesMultipleDirsAndSorts(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	wipBase := filepath.Dir(state.WipDir)
	stale := []string{"20260101-aaa-branch", "20260201-bbb-branch", "20260301-ccc-branch"}
	for _, name := range stale {
		if err := os.MkdirAll(filepath.Join(wipBase, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ClearOtherBranches(dir, false)
	if err != nil {
		t.Fatalf("ClearOtherBranches: %v", err)
	}

	if len(result.Deleted) != len(stale) {
		t.Fatalf("expected %d deletions, got %d: %v", len(stale), len(result.Deleted), result.Deleted)
	}
	for i, name := range stale {
		if result.Deleted[i] != name {
			t.Errorf("Deleted[%d]: expected %s, got %s", i, name, result.Deleted[i])
		}
		if _, err := os.Stat(filepath.Join(wipBase, name)); !os.IsNotExist(err) {
			t.Errorf("dir %s should have been deleted", name)
		}
	}

	// Current branch dir must still exist
	if _, err := os.Stat(state.WipDir); err != nil {
		t.Errorf("current branch dir should still exist: %v", err)
	}
}

// ── CleanupBranch ─────────────────────────────────────────────────────────────

func TestCleanupBranch_KeepsContextAndPR(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "cleanup-test")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	// Write content into all WIP files so they exist
	for _, f := range append(wipFiles, "extra.md") {
		if err := os.WriteFile(filepath.Join(state.WipDir, f), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := CleanupBranch(dir, false)
	if err != nil {
		t.Fatalf("CleanupBranch: %v", err)
	}

	for keep := range keepOnCleanup {
		p := filepath.Join(state.WipDir, keep)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %q to be kept, but it's missing: %v", keep, err)
		}
	}

	for _, del := range result.Deleted {
		if keepOnCleanup[del] {
			t.Errorf("file %q should not have been deleted", del)
		}
	}
}

// ── titleCase helper ─────────────────────────────────────────────────────────

func TestTitleCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plan", "Plan"},
		{"review", "Review"},
		{"execution notes", "Execution Notes"},
		{"pr", "Pr"},
		{"", ""},
	}
	for _, tc := range cases {
		got := titleCase(tc.in)
		if got != tc.want {
			t.Errorf("titleCase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── MigrateWip ───────────────────────────────────────────────────────────────

// makeWipDir creates a .wip/<name>/status.json so MigrateWip recognises it as
// a WIP branch directory.
func makeWipDir(t *testing.T, wipBase, name string) {
	t.Helper()
	dir := filepath.Join(wipBase, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateWip_RenamesLegacyDirs(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	makeWipDir(t, wipBase, "my-feature")
	makeWipDir(t, wipBase, "fix-bug")

	result, err := MigrateWip(dir)
	if err != nil {
		t.Fatalf("MigrateWip: %v", err)
	}
	if len(result.Migrated) != 2 {
		t.Errorf("expected 2 migrated, got %d: %v", len(result.Migrated), result.Migrated)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", result.Warnings)
	}
	// Originals should be gone; dated dirs should exist.
	datedRe := regexp.MustCompile(`^\d{14}-`)
	for _, m := range result.Migrated {
		if !datedRe.MatchString(m.New) {
			t.Errorf("migrated dir %q does not match YYYYMMDDHHMMSS- pattern", m.New)
		}
		if _, err := os.Stat(filepath.Join(wipBase, m.New)); err != nil {
			t.Errorf("expected %s to exist after migration: %v", m.New, err)
		}
		if _, err := os.Stat(filepath.Join(wipBase, m.Old)); err == nil {
			t.Errorf("expected %s to be gone after migration", m.Old)
		}
	}
}

func TestMigrateWip_LeavesAlreadyDatedDirsUntouched(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	makeWipDir(t, wipBase, "20260101-my-feature")

	result, err := MigrateWip(dir)
	if err != nil {
		t.Fatalf("MigrateWip: %v", err)
	}
	if len(result.Migrated) != 0 {
		t.Errorf("expected 0 migrated, got %d: %v", len(result.Migrated), result.Migrated)
	}
	if _, err := os.Stat(filepath.Join(wipBase, "20260101-my-feature")); err != nil {
		t.Error("already-dated dir should still exist")
	}
}

func TestMigrateWip_SkipsCollisionWithWarning(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	makeWipDir(t, wipBase, "my-feature")

	// Run migration once to get the dated name, then restore the legacy dir.
	result1, err := MigrateWip(dir)
	if err != nil || len(result1.Migrated) != 1 {
		t.Fatalf("first migration failed: err=%v migrated=%v", err, result1.Migrated)
	}
	datedName := result1.Migrated[0].New

	// Recreate legacy dir — dated dir still exists, so the next run collides.
	makeWipDir(t, wipBase, "my-feature")

	result2, err := MigrateWip(dir)
	if err != nil {
		t.Fatalf("MigrateWip with collision: %v", err)
	}
	if len(result2.Migrated) != 0 {
		t.Errorf("expected 0 migrated on collision, got %d", len(result2.Migrated))
	}
	if len(result2.Warnings) != 1 {
		t.Errorf("expected 1 warning for collision, got %d: %v", len(result2.Warnings), result2.Warnings)
	}
	// Both dirs should still exist.
	if _, err := os.Stat(filepath.Join(wipBase, "my-feature")); err != nil {
		t.Error("legacy dir should still exist after skipped collision")
	}
	if _, err := os.Stat(filepath.Join(wipBase, datedName)); err != nil {
		t.Error("dated dir should still exist after skipped collision")
	}
}

func TestMigrateWip_SkipsNonWipDirs(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	// A directory with no canonical WIP files should not be migrated.
	nonWip := filepath.Join(wipBase, "random-dir")
	if err := os.MkdirAll(nonWip, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := MigrateWip(dir)
	if err != nil {
		t.Fatalf("MigrateWip: %v", err)
	}
	if len(result.Migrated) != 0 {
		t.Errorf("expected 0 migrated for non-WIP dir, got %d", len(result.Migrated))
	}
	if _, err := os.Stat(nonWip); err != nil {
		t.Error("non-WIP dir should not be touched")
	}
}

func TestCleanupBranch_DryRun(t *testing.T) {
	dir := initGitRepo(t)
	state, _ := EnsureBranchState(dir, "dry-run-test")
	dummy := filepath.Join(state.WipDir, "dummy.txt")
	os.WriteFile(dummy, []byte("content"), 0o644)

	result, err := CleanupBranch(dir, true)
	if err != nil {
		t.Fatalf("CleanupBranch dry run: %v", err)
	}

	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}

	if _, err := os.Stat(dummy); os.IsNotExist(err) {
		t.Error("file should still exist during dry run")
	}
}

func TestClearWip_DryRun(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	os.MkdirAll(wipBase, 0o755)
	older := filepath.Join(wipBase, "20260101-feat")
	os.MkdirAll(older, 0o755)

	result, err := ClearWip(dir, true)
	if err != nil {
		t.Fatalf("ClearWip dry run: %v", err)
	}

	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}

	if _, err := os.Stat(older); os.IsNotExist(err) {
		t.Error("directory should still exist during dry run")
	}
}
