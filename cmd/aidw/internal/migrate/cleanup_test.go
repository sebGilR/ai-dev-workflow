package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"aidw/cmd/aidw/internal/work"
)

// --- AC-CLEANUP-DELETE, AC-CLEANUP-DRYRUN ---

func TestPlanCleanup_VerifiedCopiedSource_IsCandidate(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")

	if _, err := Run(stateDir, []string{repo}); err != nil {
		t.Fatal(err)
	}

	candidates, blocked, err := PlanCleanup(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 0 {
		t.Fatalf("expected 0 blocked, got %+v", blocked)
	}
	if len(candidates) != 1 || candidates[0] != wipDir {
		t.Fatalf("expected 1 candidate %s, got %+v", wipDir, candidates)
	}

	// AC-CLEANUP-DRYRUN: PlanCleanup itself never deletes or modifies
	// anything — it only classifies. Confirm the source is untouched.
	if _, err := os.Stat(wipDir); err != nil {
		t.Fatalf("PlanCleanup must not delete the source: %v", err)
	}

	// Now actually delete via DeleteSources and confirm removal + mapping
	// entry survives untouched (Lane D's addendum: cleanup never mutates
	// wip-paths.json).
	mappingBefore, err := loadMapping(stateDir)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := DeleteSources(candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != wipDir {
		t.Fatalf("expected deleted=[%s], got %+v", wipDir, deleted)
	}
	if _, err := os.Stat(wipDir); !os.IsNotExist(err) {
		t.Fatalf("expected source dir to be deleted, stat err=%v", err)
	}

	mappingAfter, err := loadMapping(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(mappingAfter.Entries) != len(mappingBefore.Entries) {
		t.Fatalf("mapping entry count changed after cleanup: before=%d after=%d", len(mappingBefore.Entries), len(mappingAfter.Entries))
	}
	for k, v := range mappingBefore.Entries {
		if mappingAfter.Entries[k] != v {
			t.Fatalf("mapping entry for %s changed after cleanup: before=%+v after=%+v", k, v, mappingAfter.Entries[k])
		}
	}

	// The migrated work record (and its already-copied attachments) is
	// unaffected by source deletion.
	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record to still exist, got %d, err=%v", len(records), err)
	}
}

// TestDeleteSources_PartialFailure_StillReportsWhatWasDeleted pins
// review.md #5: an undeletable candidate mid-loop must not discard the
// record of what was already permanently removed before it.
func TestDeleteSources_PartialFailure_StillReportsWhatWasDeleted(t *testing.T) {
	tmp := t.TempDir()
	before := filepath.Join(tmp, "before")
	after := filepath.Join(tmp, "after")
	for _, d := range []string{before, after} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A NUL byte makes the underlying syscall fail with EINVAL on every
	// platform this repo targets, without relying on filesystem
	// permission semantics that vary by OS/user.
	undeletable := filepath.Join(tmp, "bad\x00path")

	deleted, err := DeleteSources([]string{before, undeletable, after})
	if err == nil {
		t.Fatal("expected an error from the undeletable candidate")
	}
	if len(deleted) != 2 || deleted[0] != before || deleted[1] != after {
		t.Fatalf("expected deleted=[%s %s] despite the mid-loop failure, got %+v", before, after, deleted)
	}
	if _, statErr := os.Stat(before); !os.IsNotExist(statErr) {
		t.Fatalf("expected %s to be deleted, stat err=%v", before, statErr)
	}
	if _, statErr := os.Stat(after); !os.IsNotExist(statErr) {
		t.Fatalf("expected %s to be deleted, stat err=%v", after, statErr)
	}
}

// --- AC-CLEANUP-BLOCKED ---

func TestPlanCleanup_UnmigratedSource_IsBlockedNeverCandidate(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")
	// Deliberately never run Run() — no mapping entry exists.

	candidates, blocked, err := PlanCleanup(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates for an unmigrated source, got %+v", candidates)
	}
	if _, ok := blocked[wipDir]; !ok {
		t.Fatalf("expected %s to be blocked, got %+v", wipDir, blocked)
	}
}

func TestPlanCleanup_DanglingMappingEntry_IsBlockedNeverCandidate(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")

	if _, err := Run(stateDir, []string{repo}); err != nil {
		t.Fatal(err)
	}
	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 1 {
		t.Fatalf("setup: expected 1 record, got %d, err=%v", len(records), err)
	}
	// Simulate a purged record — mapping entry now dangles.
	if err := os.Remove(work.RecordPath(stateDir, records[0].WorkID)); err != nil {
		t.Fatal(err)
	}

	candidates, blocked, err := PlanCleanup(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("dangling entry must never be a candidate, got %+v", candidates)
	}
	if _, ok := blocked[wipDir]; !ok {
		t.Fatalf("expected %s to be blocked, got %+v", wipDir, blocked)
	}
}

func TestPlanCleanup_DivergentDestination_IsBlockedNeverCandidate(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")

	if _, err := Run(stateDir, []string{repo}); err != nil {
		t.Fatal(err)
	}
	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 1 {
		t.Fatalf("setup: expected 1 record, got %d, err=%v", len(records), err)
	}
	attachmentsDir := filepath.Join(work.Dir(stateDir, records[0].WorkID), "attachments")
	if err := os.WriteFile(filepath.Join(attachmentsDir, "status.json"), []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}

	candidates, blocked, err := PlanCleanup(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("divergent destination must never be a candidate, got %+v", candidates)
	}
	if _, ok := blocked[wipDir]; !ok {
		t.Fatalf("expected %s to be blocked, got %+v", wipDir, blocked)
	}

	// Confirm the source itself is untouched by a PlanCleanup call.
	if _, err := os.Stat(filepath.Join(wipDir, "status.json")); err != nil {
		t.Fatalf("PlanCleanup must not touch the source: %v", err)
	}
}

// TestPlanCleanup_DivergentSource_IsBlockedNeverCandidate closes a
// mutation-testing gap found in Batch 3, R1: a mutation that made
// PlanCleanup compare only the DESTINATION side (never the source) stayed
// green, because no fixture modified the source after migration — only the
// destination. The two-sided check exists in cleanup.go today; this test
// exists so a future regression on the source-side half is actually caught.
func TestPlanCleanup_DivergentSource_IsBlockedNeverCandidate(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")

	if _, err := Run(stateDir, []string{repo}); err != nil {
		t.Fatal(err)
	}

	// Simulate drift on the SOURCE side only (destination is untouched).
	if err := os.WriteFile(filepath.Join(wipDir, "status.json"), []byte("drifted"), 0o644); err != nil {
		t.Fatal(err)
	}

	candidates, blocked, err := PlanCleanup(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("divergent source must never be a candidate, got %+v", candidates)
	}
	if _, ok := blocked[wipDir]; !ok {
		t.Fatalf("expected %s to be blocked, got %+v", wipDir, blocked)
	}
}

// TestPlanCleanup_SourceFileDeleted_IsBlockedNeverCandidate closes a second
// mutation-testing gap found in Batch 3, R1: hashesEqual's len(a) != len(b)
// guard (migrate.go) was itself unexercised by any fixture — every existing
// divergence test MODIFIES a file's content, never deletes one, so a set
// comparison that forgot to check set SIZE would still pass every existing
// test. A deleted file shrinks the hash map without changing any surviving
// entry's digest, which only the length guard catches.
func TestPlanCleanup_SourceFileDeleted_IsBlockedNeverCandidate(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "main")
	wipDir := filepath.Join(repo, ".wip", "20260101000000-main")
	writeStatusJSON(t, wipDir, "main", "started")
	if err := os.WriteFile(filepath.Join(wipDir, "plan.md"), []byte("plan content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(stateDir, []string{repo}); err != nil {
		t.Fatal(err)
	}

	// Delete a file from the source after migration — every SURVIVING
	// file's digest is unchanged, only the set's size shrank.
	if err := os.Remove(filepath.Join(wipDir, "plan.md")); err != nil {
		t.Fatal(err)
	}

	candidates, blocked, err := PlanCleanup(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("a source with a deleted file must never be a candidate, got %+v", candidates)
	}
	if _, ok := blocked[wipDir]; !ok {
		t.Fatalf("expected %s to be blocked, got %+v", wipDir, blocked)
	}
}

func TestPlanCleanup_MixedBatch_EligibleAndBlockedBothProcessed(t *testing.T) {
	stateDir := setStateDir(t)
	repoGood := initTestRepo(t, "branch-good")
	repoBad := initTestRepo(t, "branch-bad")
	wipGood := filepath.Join(repoGood, ".wip", "20260101000000-branch-good")
	wipBad := filepath.Join(repoBad, ".wip", "20260101000000-branch-bad")
	writeStatusJSON(t, wipGood, "branch-good", "started")
	writeStatusJSON(t, wipBad, "branch-bad", "started")

	if _, err := Run(stateDir, []string{repoGood, repoBad}); err != nil {
		t.Fatal(err)
	}
	records, err := work.ListRecords(stateDir)
	if err != nil || len(records) != 2 {
		t.Fatalf("setup: expected 2 records, got %d, err=%v", len(records), err)
	}
	for _, r := range records {
		for _, a := range r.Attachments {
			if a.Branch == "branch-bad" {
				attachmentsDir := filepath.Join(work.Dir(stateDir, r.WorkID), "attachments")
				if err := os.WriteFile(filepath.Join(attachmentsDir, "status.json"), []byte("corrupted"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	candidates, blocked, err := PlanCleanup(stateDir, []string{repoGood, repoBad})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0] != wipGood {
		t.Fatalf("expected exactly the good source as candidate, got %+v", candidates)
	}
	if _, ok := blocked[wipBad]; !ok {
		t.Fatalf("expected the bad source to be blocked, got %+v", blocked)
	}
}

// --- AC-CLEANUP-REFUSE is exercised at the CLI layer (cmd package), since
// PlanCleanup itself never refuses — it only classifies; the refusal is
// runCleanupSources's job in migrate_state.go. ---
