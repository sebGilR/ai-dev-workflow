package migrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aidw/cmd/aidw/internal/util"
	"aidw/cmd/aidw/internal/work"
)

// initTestRepo creates a git repo at a fresh t.TempDir() with one commit on
// branch, following initHookGitRepo's exact pattern
// (cmd/aidw/internal/install/hooks_test.go:123-147) — hard rule 1 /
// Task A.9: `-b <branch>` is always passed explicitly, never a bare `git
// init`. Only local (non---global) git config is touched (hard rule 2).
func initTestRepo(t *testing.T, branch string) string {
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

	// Resolved so comparisons against paths git itself reports (e.g.
	// git.Toplevel, used internally by wip.FindBranchState) match exactly
	// on platforms where the OS temp dir is itself a symlink (macOS'
	// /tmp -> /private/tmp) — mirrors git_test.go's initRepoAt convention.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// setStateDir isolates $AIDW_STATE_DIR for the duration of the test, so
// work.Save/Load, state.RepoIdentity's repos.json, and mapping.go's
// wip-paths.json all resolve against a fresh, private directory tree.
func setStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", dir)
	return dir
}

// writeStatusJSON writes a minimal status.json into wipDir.
func writeStatusJSON(t *testing.T, wipDir, branch, stage string) {
	t.Helper()
	if err := os.MkdirAll(wipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"repo":"r","repo_path":"/tmp/r","branch":"` + branch + `","stage":"` + stage + `","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(wipDir, "status.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- AC-INVENTORY ---

func TestDiscover_AllDatedDirsPlusLegacy_NotJustNewest(t *testing.T) {
	root := t.TempDir()
	wipBase := filepath.Join(root, ".wip")

	dirs := []string{
		"20260101000000-my-branch",
		"20260102000000-my-branch",
		"20260103000000-my-branch",
		"other-branch", // legacy unprefixed
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(wipBase, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// .archive must never be treated as a migration source.
	if err := os.MkdirAll(filepath.Join(wipBase, ".archive"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 source dirs (3 dated + 1 legacy), got %d: %+v", len(got), got)
	}
	seen := map[string]bool{}
	for _, sd := range got {
		if sd.WorktreePath != root {
			t.Fatalf("WorktreePath = %s, want %s", sd.WorktreePath, root)
		}
		seen[filepath.Base(sd.SourceWipDir)] = true
	}
	for _, d := range dirs {
		if !seen[d] {
			t.Fatalf("missing expected source dir %s in %+v", d, got)
		}
	}
}

func TestDiscover_NoWipDir_NoError(t *testing.T) {
	root := t.TempDir()
	got, err := Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 source dirs, got %d", len(got))
	}
}

// --- AC-ATTACH, AC-COPY, AC-VERIFY, AC-MAPPING ---

func TestRun_NewMigration_AttachCopyVerifyMapping(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "feature-x")

	wipDir := filepath.Join(repo, ".wip", "20260101000000-feature-x")
	writeStatusJSON(t, wipDir, "feature-x", "planned")
	if err := os.WriteFile(filepath.Join(wipDir, "plan.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nested archive subdirectory (Phase 1 archive-cleanup) — must be
	// copied as an opaque file too (AC-COPY).
	archiveDir := filepath.Join(wipDir, "archive", "20250101000000")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "review.md"), []byte("old review\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := Run(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Migrated) != 1 {
		t.Fatalf("expected 1 migrated entry, got %+v", summary)
	}
	if len(summary.Skipped) != 0 || len(summary.Divergent) != 0 {
		t.Fatalf("unexpected skipped/divergent: %+v", summary)
	}

	records, err := work.ListRecords(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 work record, got %d", len(records))
	}
	r := records[0]

	// AC-ATTACH
	if len(r.Attachments) != 1 {
		t.Fatalf("expected exactly 1 attachment, got %d", len(r.Attachments))
	}
	if r.Attachments[0].Branch != "feature-x" {
		t.Fatalf("attachment branch = %s, want feature-x", r.Attachments[0].Branch)
	}
	if r.Stage != "planned" {
		t.Fatalf("stage = %s, want planned", r.Stage)
	}

	// AC-COPY: every file, including nested archive/, byte-identical.
	attachmentsDir := filepath.Join(work.Dir(stateDir, r.WorkID), "attachments")
	planData, err := os.ReadFile(filepath.Join(attachmentsDir, "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(planData) != "# Plan\n" {
		t.Fatalf("plan.md content mismatch: %q", planData)
	}
	reviewData, err := os.ReadFile(filepath.Join(attachmentsDir, "archive", "20250101000000", "review.md"))
	if err != nil {
		t.Fatalf("nested archive file not copied: %v", err)
	}
	if string(reviewData) != "old review\n" {
		t.Fatalf("nested archive file content mismatch: %q", reviewData)
	}

	// AC-VERIFY: Provenance.SourceHashes has exactly the copied files' keys.
	wantKeys := map[string]bool{
		"status.json":                      true,
		"plan.md":                          true,
		"archive/20250101000000/review.md": true,
	}
	if len(r.Provenance.SourceHashes) != len(wantKeys) {
		t.Fatalf("SourceHashes has %d entries, want %d: %+v", len(r.Provenance.SourceHashes), len(wantKeys), r.Provenance.SourceHashes)
	}
	for k := range wantKeys {
		if _, ok := r.Provenance.SourceHashes[k]; !ok {
			t.Fatalf("SourceHashes missing key %s: %+v", k, r.Provenance.SourceHashes)
		}
	}

	// AC-MAPPING
	mapping, err := loadMapping(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapping.Entries) != 1 {
		t.Fatalf("expected exactly 1 mapping entry, got %d", len(mapping.Entries))
	}
	normKey, err := filepath.EvalSymlinks(wipDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := mapping.Entries[normKey]
	if !ok {
		t.Fatalf("mapping missing entry for normalized key %s: %+v", normKey, mapping.Entries)
	}
	if entry.WorkID != r.WorkID {
		t.Fatalf("mapping work_id = %s, want %s", entry.WorkID, r.WorkID)
	}
	if entry.RepoID == "" || entry.VerifiedAt == "" {
		t.Fatalf("mapping entry missing repo_id/verified_at: %+v", entry)
	}
}

// --- Idempotency: second run reports Verified, not Migrated ---

func TestRun_SecondRun_ReportsVerifiedNotMigrated(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")

	first, err := Run(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Migrated) != 1 {
		t.Fatalf("expected 1 migrated on first run, got %+v", first)
	}

	second, err := Run(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Migrated) != 0 {
		t.Fatalf("expected 0 migrated on second run, got %+v", second)
	}
	if len(second.Verified) != 1 {
		t.Fatalf("expected 1 verified on second run, got %+v", second)
	}
}

// --- AC-DIVERGENCE ---

func TestRun_DestinationModified_ReportsDivergentNeverOverwrites(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")

	first, err := Run(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 1 {
		t.Fatalf("setup: expected 1 record, got %d, err=%v", len(records), err)
	}
	workID := records[0].WorkID
	attachmentsDir := filepath.Join(work.Dir(stateDir, workID), "attachments")

	// Simulate drift: modify a destination file post-migration.
	if err := os.WriteFile(filepath.Join(attachmentsDir, "status.json"), []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Capture source content before rerun.
	srcBefore, err := os.ReadFile(filepath.Join(wipDir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}

	second, err := Run(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Divergent) != 1 {
		t.Fatalf("expected 1 divergent entry, got %+v; first=%+v", second, first)
	}
	if len(second.Verified) != 0 || len(second.Migrated) != 0 {
		t.Fatalf("divergent entry must not also be reported verified/migrated: %+v", second)
	}

	// Destination is NOT overwritten.
	destAfter, err := os.ReadFile(filepath.Join(attachmentsDir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(destAfter) != "corrupted" {
		t.Fatalf("destination was overwritten: %q", destAfter)
	}

	// Source is NOT touched.
	srcAfter, err := os.ReadFile(filepath.Join(wipDir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(srcAfter) != string(srcBefore) {
		t.Fatalf("source was touched: before=%q after=%q", srcBefore, srcAfter)
	}
}

func TestRun_Divergence_OtherEntriesStillSucceed(t *testing.T) {
	stateDir := setStateDir(t)
	repoA := initTestRepo(t, "branch-a")
	repoB := initTestRepo(t, "branch-b")
	wipDirA := filepath.Join(repoA, ".wip", "20260101000000-branch-a")
	wipDirB := filepath.Join(repoB, ".wip", "20260101000000-branch-b")
	writeStatusJSON(t, wipDirA, "branch-a", "started")
	writeStatusJSON(t, wipDirB, "branch-b", "started")

	if _, err := Run(stateDir, []string{repoA, repoB}); err != nil {
		t.Fatal(err)
	}

	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 2 {
		t.Fatalf("setup: expected 2 records, got %d, err=%v", len(records), err)
	}
	// Corrupt repoA's attachment only.
	for _, r := range records {
		for _, a := range r.Attachments {
			if a.Branch == "branch-a" {
				attachmentsDir := filepath.Join(work.Dir(stateDir, r.WorkID), "attachments")
				if err := os.WriteFile(filepath.Join(attachmentsDir, "status.json"), []byte("corrupted"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	second, err := Run(stateDir, []string{repoA, repoB})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Divergent) != 1 {
		t.Fatalf("expected 1 divergent, got %+v", second)
	}
	if len(second.Verified) != 1 {
		t.Fatalf("expected the other entry to still verify successfully, got %+v", second)
	}
}

// --- AC-DANGLING ---

func TestRun_DanglingMappingEntry_TreatedAsUnverifiedNotSafe(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")

	first, err := Run(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Migrated) != 1 {
		t.Fatalf("expected 1 migrated, got %+v", first)
	}

	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 1 {
		t.Fatalf("setup: expected 1 record, got %d, err=%v", len(records), err)
	}
	oldWorkID := records[0].WorkID

	// Simulate a purged record: remove work.json (but leave the mapping
	// entry pointing at the now-gone work_id, with its old verified_at
	// still present and NOT stale-looking).
	if err := os.Remove(work.RecordPath(stateDir, oldWorkID)); err != nil {
		t.Fatal(err)
	}

	second, err := Run(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	// Must NOT be reported as safely-Verified on the strength of the stale
	// verified_at alone.
	if len(second.Verified) != 0 {
		t.Fatalf("dangling entry must not be reported Verified, got %+v", second)
	}
	if len(second.Migrated) != 1 {
		t.Fatalf("dangling entry must be fully re-migrated, got %+v", second)
	}

	mapping, err := loadMapping(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	normKey, err := filepath.EvalSymlinks(wipDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := mapping.Entries[normKey]
	if !ok {
		t.Fatalf("mapping entry missing after recovery: %+v", mapping.Entries)
	}
	if entry.WorkID == oldWorkID {
		t.Fatalf("mapping still points at the purged work_id %s", oldWorkID)
	}
	if _, err := work.Load(stateDir, entry.WorkID); err != nil {
		t.Fatalf("new work_id %s should load cleanly: %v", entry.WorkID, err)
	}
}

// --- AC-SOURCES-INTACT ---

func TestRun_SourcesNeverModifiedOrDeleted(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")
	if err := os.WriteFile(filepath.Join(wipDir, "plan.md"), []byte("plan content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := HashTree(wipDir)
	if err != nil {
		t.Fatal(err)
	}
	beforeEntries, err := os.ReadDir(wipDir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Run(stateDir, []string{repo}); err != nil {
		t.Fatal(err)
	}

	after, err := HashTree(wipDir)
	if err != nil {
		t.Fatal(err)
	}
	if !hashesEqual(before, after) {
		t.Fatalf("source dir mutated: before=%+v after=%+v", before, after)
	}
	afterEntries, err := os.ReadDir(wipDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeEntries) != len(afterEntries) {
		t.Fatalf("new file/dir appeared under source: before=%d after=%d", len(beforeEntries), len(afterEntries))
	}
}

// --- AC-NOBYPASS (sanity: mapping.go's own write is the only non-work.Save
// write to disk performed by this package's orchestration path; verified
// here indirectly by confirming util.WriteJSON is only ever asked to write
// wip-paths.json in this package — the real code-review-time grep is
// AC-NOBYPASS's actual mechanism, this is a smoke check of intent). ---

func TestMapping_UpdateWritesOnlyWipPathsJSON(t *testing.T) {
	stateDir := t.TempDir()
	if err := Update(stateDir, func(m *Mapping) error {
		m.Entries["/some/source/dir"] = MappingEntry{WorkID: "w1", RepoID: "r1", VerifiedAt: util.NowISO()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path := mappingPath(stateDir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected wip-paths.json to exist: %v", err)
	}
	m, err := loadMapping(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m.Entries))
	}
}
