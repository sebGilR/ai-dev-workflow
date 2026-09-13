package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"aidw/cmd/aidw/internal/state"
	"aidw/cmd/aidw/internal/work"
)

// writeGlobalArchiveEntry creates a .wip/.archive/<name>/ tree under root,
// optionally with a status.json (branch/stage), plus one ordinary file so
// CopyAttachments/HashTree has something to copy.
func writeGlobalArchiveEntry(t *testing.T, root, name string, status *legacyStatus) string {
	t.Helper()
	dir := filepath.Join(root, ".wip", globalArchiveDirName, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if status != nil {
		content := `{"branch":"` + status.Branch + `","stage":"` + status.Stage + `"}`
		if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "context.md"), []byte("# archived context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// --- AC-I2-ARCHIVEMIG-IDENTITY ----------------------------------------------

func TestMigrateArchived_Identity(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	writeGlobalArchiveEntry(t, repo, "20260101-foo", &legacyStatus{Branch: "foo", Stage: "reviewed"})

	summary, err := RunWithOptions(stateDir, []string{repo}, Options{IncludeGlobalArchive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Migrated) != 1 {
		t.Fatalf("expected 1 migrated entry, got %+v", summary)
	}

	records, err := work.ListRecords(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 work record, got %d", len(records))
	}
	r := records[0]

	if r.Lifecycle != work.LifecycleArchived {
		t.Errorf("lifecycle = %q, want %q", r.Lifecycle, work.LifecycleArchived)
	}
	if r.Stage != "reviewed" {
		t.Errorf("stage = %q, want %q", r.Stage, "reviewed")
	}
	if len(r.Attachments) != 1 {
		t.Fatalf("expected exactly 1 attachment, got %d", len(r.Attachments))
	}
	if r.Attachments[0].Branch != "foo" {
		t.Errorf("attachment branch = %q, want %q", r.Attachments[0].Branch, "foo")
	}
	wantRepoID, err := state.RepoIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	if r.Attachments[0].RepoID != wantRepoID {
		t.Errorf("attachment repo_id = %q, want %q", r.Attachments[0].RepoID, wantRepoID)
	}

	attachmentsDir := filepath.Join(work.Dir(stateDir, r.WorkID), "attachments")
	if _, err := os.Stat(filepath.Join(attachmentsDir, "status.json")); err != nil {
		t.Errorf("expected status.json copied into attachments: %v", err)
	}
	if _, err := os.Stat(filepath.Join(attachmentsDir, "context.md")); err != nil {
		t.Errorf("expected context.md copied into attachments: %v", err)
	}
	for k := range r.Provenance.SourceHashes {
		if _, err := os.Stat(filepath.Join(attachmentsDir, k)); err != nil {
			t.Errorf("SourceHashes key %s not present at destination: %v", k, err)
		}
	}
	if len(r.Provenance.SourceHashes) != 2 {
		t.Fatalf("expected 2 SourceHashes entries (status.json, context.md), got %d: %+v", len(r.Provenance.SourceHashes), r.Provenance.SourceHashes)
	}
}

// --- AC-I2-ARCHIVEMIG-SUFFIX -------------------------------------------------

func TestMigrateArchived_CollisionSuffixRetainedNotStripped(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	// No status.json at all — forces the directory-name fallback.
	writeGlobalArchiveEntry(t, repo, "20260101-foo-2", nil)

	summary, err := RunWithOptions(stateDir, []string{repo}, Options{IncludeGlobalArchive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Migrated) != 1 {
		t.Fatalf("expected 1 migrated entry, got %+v", summary)
	}

	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record, got %d, err=%v", len(records), err)
	}
	r := records[0]
	if r.Title != "foo-2" {
		t.Errorf("title = %q, want literal %q (suffix retained, not guess-stripped)", r.Title, "foo-2")
	}
	if len(r.Attachments) != 1 || r.Attachments[0].Branch != "foo-2" {
		t.Fatalf("attachment branch = %+v, want foo-2", r.Attachments)
	}
}

// --- AC-I2-ARCHIVEMIG-HEAD ---------------------------------------------------

func TestMigrateArchived_HeadIsEmptyNeverCurrentHEAD(t *testing.T) {
	stateDir := setStateDir(t)
	// Worktree's CURRENT checked-out branch is "main" — different from the
	// archived branch "foo".
	repo := initTestRepo(t, "main")
	writeGlobalArchiveEntry(t, repo, "20260101-foo", &legacyStatus{Branch: "foo", Stage: "reviewed"})

	if _, err := RunWithOptions(stateDir, []string{repo}, Options{IncludeGlobalArchive: true}); err != nil {
		t.Fatal(err)
	}

	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record, got %d, err=%v", len(records), err)
	}
	r := records[0]
	if len(r.Attachments) != 1 {
		t.Fatalf("expected exactly 1 attachment, got %d", len(r.Attachments))
	}
	if r.Attachments[0].Head != "" {
		t.Errorf("Head = %q, want empty string (never main's current HEAD sha)", r.Attachments[0].Head)
	}
}

// --- AC-I2-ARCHIVEMIG-IDEMPOTENT ---------------------------------------------

func TestMigrateArchived_SecondRunReportsVerifiedNotMigrated(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	archiveDir := writeGlobalArchiveEntry(t, repo, "20260101-foo", &legacyStatus{Branch: "foo", Stage: "reviewed"})

	first, err := RunWithOptions(stateDir, []string{repo}, Options{IncludeGlobalArchive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Migrated) != 1 {
		t.Fatalf("expected 1 migrated on first run, got %+v", first)
	}

	second, err := RunWithOptions(stateDir, []string{repo}, Options{IncludeGlobalArchive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Migrated) != 0 {
		t.Fatalf("expected 0 newly-migrated on second run, got %+v", second)
	}
	if len(second.Verified) != 1 {
		t.Fatalf("expected 1 verified on second run, got %+v", second)
	}

	mapping, err := loadMapping(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapping.Entries) != 1 {
		t.Fatalf("expected exactly 1 mapping entry, got %d: %+v", len(mapping.Entries), mapping.Entries)
	}
	normKey, err := filepath.EvalSymlinks(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mapping.Entries[normKey]; !ok {
		t.Fatalf("mapping missing entry for %s: %+v", normKey, mapping.Entries)
	}

	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected exactly 1 work record after both runs, got %d, err=%v", len(records), err)
	}
}

// --- AC-I2-ARCHIVEMIG-NOAMBIG ------------------------------------------------

func TestMigrateArchived_NoAmbiguityForRelatedCollisionNames(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "foo")
	writeGlobalArchiveEntry(t, repo, "20260101-foo", &legacyStatus{Branch: "foo", Stage: "reviewed"})
	writeGlobalArchiveEntry(t, repo, "20260101-foo-2", &legacyStatus{Branch: "foo-2", Stage: "reviewed"})

	summary, err := RunWithOptions(stateDir, []string{repo}, Options{IncludeGlobalArchive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Migrated) != 2 {
		t.Fatalf("expected 2 migrated entries, got %+v", summary)
	}

	repoID, err := state.RepoIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	normalizedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}

	// Both archived records must be excluded from worktree-match candidacy
	// by resolve.go's Active||Paused filter — Resolve must never return
	// ErrAmbiguousWork naming either of them.
	_, candidates, err := work.Resolve(work.ResolveOptions{
		StateDir:     stateDir,
		RepoID:       repoID,
		Branch:       "foo",
		WorktreePath: normalizedRepo,
	})
	if err == nil {
		t.Fatal("expected no active/paused record for this repo/branch, got a resolved record")
	}
	if err.Error() != work.ErrNoActiveWork.Error() {
		t.Fatalf("expected ErrNoActiveWork (never ErrAmbiguousWork against the archived records), got %v, candidates=%+v", err, candidates)
	}
}

// --- AC-I2-ARCHIVEMIG-STALEPAIR ----------------------------------------------

func TestMigrateArchived_StaleActiveFreshArchivedPairIsAcceptedNotSpecialCased(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "foo")

	// Branch "foo" already migrated while live (an Active record keyed
	// under .wip/foo).
	liveWipDir := filepath.Join(repo, ".wip", "foo")
	writeStatusJSON(t, liveWipDir, "foo", "started")
	firstRun, err := Run(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstRun.Migrated) != 1 {
		t.Fatalf("expected 1 migrated (the live branch dir), got %+v", firstRun)
	}
	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 1 {
		t.Fatalf("setup: expected 1 record, got %d, err=%v", len(records), err)
	}
	liveRecord := records[0]
	if liveRecord.Lifecycle != work.LifecycleActive {
		t.Fatalf("setup: expected the live record's lifecycle to be active, got %q", liveRecord.Lifecycle)
	}

	// "foo" is subsequently legacy-archived (simulated directly — the point
	// under test is the migration's handling, not clear-wip/clear-others'
	// own archiving mechanics) into .wip/.archive/foo.
	writeGlobalArchiveEntry(t, repo, "foo", &legacyStatus{Branch: "foo", Stage: "started"})

	secondRun, err := RunWithOptions(stateDir, []string{repo}, Options{IncludeGlobalArchive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondRun.Migrated) != 1 {
		t.Fatalf("expected 1 newly-migrated (the archive entry), got %+v", secondRun)
	}

	allRecords, err := work.ListRecords(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(allRecords) != 2 {
		t.Fatalf("expected 2 records total (stale active + fresh archived), got %d: %+v", len(allRecords), allRecords)
	}

	var sawActive, sawArchived bool
	for _, r := range allRecords {
		switch r.Lifecycle {
		case work.LifecycleActive:
			sawActive = true
			if r.WorkID != liveRecord.WorkID {
				t.Errorf("the original active record's work_id changed: got %s, want %s", r.WorkID, liveRecord.WorkID)
			}
		case work.LifecycleArchived:
			sawArchived = true
		}
	}
	if !sawActive || !sawArchived {
		t.Fatalf("expected both an active and an archived record, got %+v", allRecords)
	}
}
