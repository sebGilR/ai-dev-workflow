package wip

import (
	"errors"
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

	result, err := ClearWip(dir, false, false)
	if err != nil {
		t.Fatalf("ClearWip: %v", err)
	}

	if result.Kept == nil || *result.Kept != "20260312150000-feat" {
		t.Errorf("expected kept=20260312150000-feat, got %v", result.Kept)
	}

	// Older dirs must be moved out of place (archived), not deleted
	for _, d := range []string{oldest, older} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("dir %s should have been archived out of place", d)
		}
	}
	// ... but still recoverable under .wip/.archive/
	for _, name := range []string{"20260101-feat", "20260312-feat"} {
		if _, err := os.Stat(filepath.Join(wipBase, globalArchiveDirName, name)); err != nil {
			t.Errorf("expected %s to be archived under .archive/: %v", name, err)
		}
	}
	// Newest dir must remain
	if _, err := os.Stat(newest); err != nil {
		t.Errorf("newest dir should still exist: %v", err)
	}
}

func TestClearWip_ArchivesLegacyDirs(t *testing.T) {
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

	result, err := ClearWip(dir, false, false)
	if err != nil {
		t.Fatalf("ClearWip: %v", err)
	}

	found := false
	for _, a := range result.Archived {
		if a == "old-branch-name" {
			found = true
		}
	}
	if !found {
		t.Error("expected legacy dir to be in Archived list")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("legacy dir should have been moved out of place")
	}
	if _, err := os.Stat(filepath.Join(wipBase, globalArchiveDirName, "old-branch-name")); err != nil {
		t.Errorf("expected old-branch-name to be recoverable under .archive/: %v", err)
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

	result, err := ClearWip(dir, false, false)
	if err != nil {
		t.Fatalf("ClearWip: %v", err)
	}

	// Should keep the alphabetically last legacy dir
	if result.Kept == nil || *result.Kept != "zzz-branch" {
		t.Errorf("expected kept=zzz-branch, got %v", result.Kept)
	}
	if _, err := os.Stat(older); !os.IsNotExist(err) {
		t.Error("older legacy dir should have been moved out of place")
	}
	if _, err := os.Stat(newer); err != nil {
		t.Errorf("newer legacy dir should still exist: %v", err)
	}
}

func TestClearWip_EmptyWip(t *testing.T) {
	dir := initGitRepo(t)

	result, err := ClearWip(dir, false, false)
	if err != nil {
		t.Fatalf("ClearWip on empty wip: %v", err)
	}
	if result.Kept != nil {
		t.Errorf("expected Kept=nil, got %v", *result.Kept)
	}
	if len(result.Archived) != 0 || len(result.Deleted) != 0 {
		t.Errorf("expected no archives/deletions, got archived=%v deleted=%v", result.Archived, result.Deleted)
	}
}

func TestClearWip_Purge_DeletesArchiveToo(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	if err := os.MkdirAll(wipBase, 0o755); err != nil {
		t.Fatal(err)
	}
	oldest := filepath.Join(wipBase, "20260101-feat")
	newest := filepath.Join(wipBase, "20260312150000-feat")
	for _, d := range []string{oldest, newest} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// First archive, then purge — the archived content must also be gone after purge.
	if _, err := ClearWip(dir, false, false); err != nil {
		t.Fatalf("archive pass: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wipBase, globalArchiveDirName, "20260101-feat")); err != nil {
		t.Fatalf("expected archived content before purge: %v", err)
	}

	result, err := ClearWip(dir, false, true)
	if err != nil {
		t.Fatalf("ClearWip purge: %v", err)
	}
	if !result.Purge {
		t.Error("expected Purge=true in result")
	}
	if _, err := os.Stat(filepath.Join(wipBase, globalArchiveDirName)); !os.IsNotExist(err) {
		t.Error(".archive/ should be gone after purge")
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

	result, err := ClearOtherBranches(dir, false, false)
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
	// Other dir must be moved out of place (archived)
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Error("other branch dir should have been moved out of place")
	}
	if _, err := os.Stat(filepath.Join(wipBase, globalArchiveDirName, "20260101-other-branch")); err != nil {
		t.Errorf("expected other branch dir recoverable under .archive/: %v", err)
	}
	if len(result.Archived) != 1 || result.Archived[0] != "20260101-other-branch" {
		t.Errorf("expected Archived=[20260101-other-branch], got %v", result.Archived)
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

	if _, err := ClearOtherBranches(dir, false, false); err != nil {
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

func TestClearOtherBranches_ArchivesLegacyDirs(t *testing.T) {
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

	result, err := ClearOtherBranches(dir, false, false)
	if err != nil {
		t.Fatalf("ClearOtherBranches: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("legacy dir should have been moved out of place")
	}
	if _, err := os.Stat(filepath.Join(wipBase, globalArchiveDirName, "old-branch-name")); err != nil {
		t.Errorf("expected old-branch-name recoverable under .archive/: %v", err)
	}
	found := false
	for _, a := range result.Archived {
		if a == "old-branch-name" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected old-branch-name in Archived, got %v", result.Archived)
	}
}

func TestClearOtherBranches_NothingToDelete(t *testing.T) {
	dir := initGitRepo(t)

	// Initialize the current branch so .wip exists
	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	result, err := ClearOtherBranches(dir, false, false)
	if err != nil {
		t.Fatalf("ClearOtherBranches on empty wip: %v", err)
	}

	keepName := filepath.Base(state.WipDir)
	if result.Kept == nil || *result.Kept != keepName {
		t.Errorf("expected kept=%s, got %v", keepName, result.Kept)
	}
	if len(result.Archived) != 0 || len(result.Deleted) != 0 {
		t.Errorf("expected no archives/deletions, got archived=%v deleted=%v", result.Archived, result.Deleted)
	}
}

func TestClearOtherBranches_ArchivesMultipleDirsAndSorts(t *testing.T) {
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

	result, err := ClearOtherBranches(dir, false, false)
	if err != nil {
		t.Fatalf("ClearOtherBranches: %v", err)
	}

	if len(result.Archived) != len(stale) {
		t.Fatalf("expected %d archived, got %d: %v", len(stale), len(result.Archived), result.Archived)
	}
	for i, name := range stale {
		if result.Archived[i] != name {
			t.Errorf("Archived[%d]: expected %s, got %s", i, name, result.Archived[i])
		}
		if _, err := os.Stat(filepath.Join(wipBase, name)); !os.IsNotExist(err) {
			t.Errorf("dir %s should have been moved out of place", name)
		}
		if _, err := os.Stat(filepath.Join(wipBase, globalArchiveDirName, name)); err != nil {
			t.Errorf("dir %s should be recoverable under .archive/: %v", name, err)
		}
	}

	// Current branch dir must still exist
	if _, err := os.Stat(state.WipDir); err != nil {
		t.Errorf("current branch dir should still exist: %v", err)
	}
}

func TestClearOtherBranches_NoActiveWorkErrors(t *testing.T) {
	dir := initGitRepo(t)
	if _, err := ClearOtherBranches(dir, false, false); !errors.Is(err, ErrNoActiveWork) {
		t.Errorf("expected ErrNoActiveWork, got %v", err)
	}
}

// ── CleanupBranch ─────────────────────────────────────────────────────────────

func TestCleanupBranch_KeepsContextAndPR(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	// Write content into all WIP files so they exist
	for _, f := range append(wipFiles, "extra.md") {
		if err := os.WriteFile(filepath.Join(state.WipDir, f), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := CleanupBranch(dir, false, false)
	if err != nil {
		t.Fatalf("CleanupBranch: %v", err)
	}

	for keep := range keepOnCleanup {
		p := filepath.Join(state.WipDir, keep)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %q to be kept, but it's missing: %v", keep, err)
		}
	}

	for _, a := range result.Archived {
		if keepOnCleanup[a] {
			t.Errorf("file %q should not have been archived away", a)
		}
		if _, err := os.Stat(filepath.Join(state.WipDir, a)); !os.IsNotExist(err) {
			t.Errorf("archived file %q should no longer be at its original path", a)
		}
	}
	if result.ArchiveDir == "" {
		t.Fatal("expected a non-empty ArchiveDir when files were archived")
	}
	for _, a := range result.Archived {
		if _, err := os.Stat(filepath.Join(result.ArchiveDir, a)); err != nil {
			t.Errorf("expected %q under archive dir %q: %v", a, result.ArchiveDir, err)
		}
	}

	// An unrecognised attachment (not one of the standard wipFiles) must be
	// preserved (archived), never silently dropped.
	if _, err := os.Stat(filepath.Join(result.ArchiveDir, "extra.md")); err != nil {
		t.Errorf("expected unknown attachment extra.md to be archived, not lost: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(result.ArchiveDir, "extra.md")); err != nil || string(data) != "content" {
		t.Errorf("expected extra.md's content to survive archiving unchanged, got %q (err=%v)", data, err)
	}

	// status.json must always survive cleanup (not just purge) — it is the
	// workflow's stage/state record.
	if _, err := os.Stat(filepath.Join(state.WipDir, "status.json")); err != nil {
		t.Errorf("status.json must survive cleanup: %v", err)
	}
}

func TestCleanupBranch_ArchiveSurvivesSecondRun(t *testing.T) {
	dir := initGitRepo(t)
	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}
	if err := os.WriteFile(filepath.Join(state.WipDir, "research.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := CleanupBranch(dir, false, false); err != nil {
		t.Fatalf("first CleanupBranch: %v", err)
	}
	archiveRoot := filepath.Join(state.WipDir, branchArchiveDirName)
	if _, err := os.Stat(archiveRoot); err != nil {
		t.Fatalf("expected archive dir after first run: %v", err)
	}

	// Note: EnsureBranchState (called internally by CleanupBranch to resolve
	// the branch dir) re-seeds any missing wipFiles placeholders on every
	// call — including the ones just archived. That reseed-then-archive
	// cycle is pre-existing EnsureBranchState behavior, not something this
	// test exercises; what matters here is that running cleanup again does
	// not fail trying to re-archive archive/ into itself, and does not
	// touch (let alone lose) the first run's archived content.
	result2, err := CleanupBranch(dir, false, false)
	if err != nil {
		t.Fatalf("second CleanupBranch: %v", err)
	}
	for _, a := range result2.Archived {
		if a == branchArchiveDirName {
			t.Errorf("archive/ must never sweep itself, got Archived=%v", result2.Archived)
		}
	}
	// The first run's archived batch must still be present and recoverable
	// after the second run, whether or not the two timestamps collided.
	found := 0
	_ = filepath.Walk(archiveRoot, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "research.md" {
			found++
		}
		return nil
	})
	if found == 0 {
		t.Error("expected research.md to remain recoverable under archive/ after second run")
	}
}

func TestCleanupBranch_Purge_DeletesArchiveToo(t *testing.T) {
	dir := initGitRepo(t)
	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}
	if err := os.WriteFile(filepath.Join(state.WipDir, "research.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := CleanupBranch(dir, false, false); err != nil {
		t.Fatalf("archive pass: %v", err)
	}
	archiveRoot := filepath.Join(state.WipDir, branchArchiveDirName)
	if _, err := os.Stat(archiveRoot); err != nil {
		t.Fatalf("expected archive dir before purge: %v", err)
	}

	if err := os.WriteFile(filepath.Join(state.WipDir, "research.md"), []byte("more content"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := CleanupBranch(dir, false, true)
	if err != nil {
		t.Fatalf("CleanupBranch purge: %v", err)
	}
	if !result.Purge {
		t.Error("expected Purge=true")
	}
	if _, err := os.Stat(archiveRoot); !os.IsNotExist(err) {
		t.Error("archive/ should be gone after purge")
	}
	if _, err := os.Stat(filepath.Join(state.WipDir, "research.md")); !os.IsNotExist(err) {
		t.Error("research.md should be gone after purge")
	}
	// status.json must always survive — cleanup (even purge) must never reset workflow state.
	if _, err := os.Stat(filepath.Join(state.WipDir, "status.json")); err != nil {
		t.Errorf("status.json must survive purge: %v", err)
	}
}

// ── FindBranchState / lookup-only commands (Cluster B, E3#1) ────────────────

func TestFindBranchState_NoActiveWork(t *testing.T) {
	dir := initGitRepo(t)

	if _, err := FindBranchState(dir, ""); !errors.Is(err, ErrNoActiveWork) {
		t.Errorf("expected ErrNoActiveWork, got %v", err)
	}
	// Must create nothing.
	if _, err := os.Stat(filepath.Join(dir, ".wip")); !os.IsNotExist(err) {
		t.Error("FindBranchState must not create .wip")
	}
}

func TestFindBranchState_FindsExistingDatedDir(t *testing.T) {
	dir := initGitRepo(t)
	created, err := EnsureBranchState(dir, "feat-x")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	found, err := FindBranchState(dir, "feat-x")
	if err != nil {
		t.Fatalf("FindBranchState: %v", err)
	}
	if found.WipDir != created.WipDir {
		t.Errorf("expected %q, got %q", created.WipDir, found.WipDir)
	}
}

func TestStatusNextContextSummary_DoNotCreateState(t *testing.T) {
	dir := initGitRepo(t)

	if _, err := SummarizeStatus(dir); !errors.Is(err, ErrNoActiveWork) {
		t.Errorf("SummarizeStatus: expected ErrNoActiveWork, got %v", err)
	}
	if _, err := GetNextAction(dir); !errors.Is(err, ErrNoActiveWork) {
		t.Errorf("GetNextAction: expected ErrNoActiveWork, got %v", err)
	}
	if _, err := WriteContextSummary(dir); !errors.Is(err, ErrNoActiveWork) {
		t.Errorf("WriteContextSummary: expected ErrNoActiveWork, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".wip")); !os.IsNotExist(err) {
		t.Error("no .wip directory should have been created by lookup-only calls")
	}
}

// ── Summary generation (Cluster A, E3#5) ────────────────────────────────────

func TestSummaryIncludesTailAndSpec(t *testing.T) {
	dir := initGitRepo(t)
	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	long := strings.Repeat("x", 400) + "TAIL-MARKER"
	if err := os.WriteFile(filepath.Join(state.WipDir, "execution.md"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state.WipDir, "spec.md"), []byte("# Spec\nSPEC-MARKER"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state.WipDir, "task-context.md"), []byte("TASK-CONTEXT-MARKER"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := WriteContextSummary(dir)
	if err != nil {
		t.Fatalf("WriteContextSummary: %v", err)
	}
	data, err := os.ReadFile(result.SummaryPath)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "TAIL-MARKER") {
		t.Error("expected execution.md's tail (append-mode) to be included, not truncated head")
	}
	if !strings.Contains(content, "## Specification") || !strings.Contains(content, "SPEC-MARKER") {
		t.Error("expected a ## Specification section with spec.md content")
	}
	if !strings.Contains(content, "## Task Context") || !strings.Contains(content, "TASK-CONTEXT-MARKER") {
		t.Error("expected a ## Task Context section with task-context.md content")
	}
	if !strings.HasPrefix(content, "<!-- aidw:summary generated_at=") {
		t.Error("expected a provenance header on the first line")
	}
}

func TestSummaryStalenessFlag(t *testing.T) {
	dir := initGitRepo(t)
	if _, err := EnsureBranchState(dir, ""); err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	if _, err := WriteContextSummary(dir); err != nil {
		t.Fatalf("WriteContextSummary: %v", err)
	}

	staleness, err := CheckSummaryStaleness(dir)
	if err != nil {
		t.Fatalf("CheckSummaryStaleness: %v", err)
	}
	if staleness.Stale {
		t.Error("expected fresh summary to report stale=false")
	}

	state, _ := FindBranchState(dir, "")
	if err := os.WriteFile(filepath.Join(state.WipDir, "research.md"), []byte("new findings"), 0o644); err != nil {
		t.Fatal(err)
	}

	staleness2, err := CheckSummaryStaleness(dir)
	if err != nil {
		t.Fatalf("CheckSummaryStaleness after edit: %v", err)
	}
	if !staleness2.Stale {
		t.Error("expected summary to report stale=true after editing a source file")
	}
}

// ── Known limitations fixed only in phase 2 (Cluster E3 item 9) ────────────
//
// These capture repro recipes for gaps the Astra evaluation found that
// phase 1 does not (and should not try to) fix — they require the work-ID
// store from Cluster G/I. Keeping them here, skipped, means the recipe
// stays in-tree instead of living only in evaluation-assessment.md.

func TestKnownLimitation_WorktreeRemovalLosesWipState(t *testing.T) {
	t.Skip("fixed in phase 2 — work-ID store; see spec Cluster G/I. " +
		"Repro: .wip/<branch>/ lives inside the worktree's own checkout " +
		"(git.Toplevel resolves to the worktree path, not the main repo), " +
		"so `git worktree remove` deletes all WIP state for that branch " +
		"with it — nothing external survives removal.")
}

func TestKnownLimitation_StartBranchFlagIsNotPersisted(t *testing.T) {
	t.Skip("fixed in phase 2 — work-ID store; see spec Cluster G/I. " +
		"Repro: `aidw start . --branch foo` (cmd/start.go) resolves and " +
		"seeds .wip/<date>-foo/ for one call, but nothing records that " +
		"this session is bound to branch \"foo\" rather than the actual " +
		"current git branch — the next command resolves via the real git " +
		"branch again and can land in a different .wip dir.")
}

func TestKnownLimitation_SetStageImplementingIsUngated(t *testing.T) {
	t.Skip("fixed in phase 2 — work-ID store; see spec Cluster G/I. " +
		"Repro: unlike \"planned\"/\"specified\"/\"spec-reviewed\"/\"researched\"/" +
		"\"reviewed\", the \"implementing\" stage has no required-files entry " +
		"in SetStage's switch (wip.go), so `aidw set-stage . implementing` " +
		"succeeds even with an empty or missing spec.md — there is no task " +
		"list to actually implement against.")
}

func TestKnownLimitation_NoTaskSpecReportsFalseCompletion(t *testing.T) {
	t.Skip("fixed in phase 2 — work-ID store; see spec Cluster G/I. " +
		"Repro: NextTask (task.go) returns (nil, nil) both when every " +
		"parsed task is complete AND when spec.md has zero parseable " +
		"\"Task N\" headers — GetNextAction's \"implementing\" case " +
		"(wip.go) treats both as \"finished, go to /wip-review\", so a " +
		"spec that was never broken into tasks reads as falsely complete.")
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
	state, _ := EnsureBranchState(dir, "")
	dummy := filepath.Join(state.WipDir, "dummy.txt")
	os.WriteFile(dummy, []byte("content"), 0o644)

	result, err := CleanupBranch(dir, true, false)
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

	result, err := ClearWip(dir, true, false)
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
