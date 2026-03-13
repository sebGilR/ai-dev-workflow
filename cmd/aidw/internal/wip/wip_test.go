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

	// WipDir must exist and match YYYYMMDD-my-feature pattern
	if _, err := os.Stat(state.WipDir); err != nil {
		t.Fatalf("WipDir does not exist: %v", err)
	}
	base := filepath.Base(state.WipDir)
	pattern := regexp.MustCompile(`^\d{8}-my-feature$`)
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
	stage, _ := state.Status["stage"].(string)
	if stage != "started" {
		t.Errorf("expected stage=started, got %q", stage)
	}
	// branch must match what we passed
	branch, _ := state.Status["branch"].(string)
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

	for _, f := range WIP_FILES {
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
	stage, _ := result["stage"].(string)
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
	if result["stage"] != "planned" {
		t.Errorf("expected planned, got %v", result["stage"])
	}
}

// ── ClearWip ─────────────────────────────────────────────────────────────────

func TestClearWip_KeepsMostRecentDatedDir(t *testing.T) {
	dir := initGitRepo(t)
	wipBase := filepath.Join(dir, ".wip")
	if err := os.MkdirAll(wipBase, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create two dated dirs — older and newer
	older := filepath.Join(wipBase, "20260101-feat")
	newer := filepath.Join(wipBase, "20260312-feat")
	for _, d := range []string{older, newer} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ClearWip(dir)
	if err != nil {
		t.Fatalf("ClearWip: %v", err)
	}

	if result.Kept == nil || *result.Kept != "20260312-feat" {
		t.Errorf("expected kept=20260312-feat, got %v", result.Kept)
	}

	// Older dir must be gone
	if _, err := os.Stat(older); !os.IsNotExist(err) {
		t.Errorf("older dir should have been deleted")
	}
	// Newer dir must remain
	if _, err := os.Stat(newer); err != nil {
		t.Errorf("newer dir should still exist: %v", err)
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

	result, err := ClearWip(dir)
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

func TestClearWip_EmptyWip(t *testing.T) {
	dir := initGitRepo(t)

	result, err := ClearWip(dir)
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

// ── CleanupBranch ─────────────────────────────────────────────────────────────

func TestCleanupBranch_KeepsContextAndPR(t *testing.T) {
	dir := initGitRepo(t)

	state, err := EnsureBranchState(dir, "cleanup-test")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	// Write content into all WIP files so they exist
	for _, f := range append(WIP_FILES, "extra.md") {
		if err := os.WriteFile(filepath.Join(state.WipDir, f), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := CleanupBranch(dir)
	if err != nil {
		t.Fatalf("CleanupBranch: %v", err)
	}

	for keep := range KEEP_ON_CLEANUP {
		p := filepath.Join(state.WipDir, keep)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %q to be kept, but it's missing: %v", keep, err)
		}
	}

	for _, del := range result.Deleted {
		if KEEP_ON_CLEANUP[del] {
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
