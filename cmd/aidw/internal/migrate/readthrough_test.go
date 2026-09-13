package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"aidw/cmd/aidw/internal/wip"
)

// TestReadThrough_WipFindBranchStateStillResolvesAfterMigration is D3's
// proof: `aidw migrate-state` never deletes a source .wip directory, so
// wip.go's own exported lookup entry point (wip.FindBranchState) must
// continue to resolve the legacy directory and its status.json content
// correctly after a migration run — read-through holds by construction,
// verified here with a real assertion rather than a new dispatch layer.
// This test does not modify internal/wip in any way; it only imports its
// existing exported entry point.
func TestReadThrough_WipFindBranchStateStillResolvesAfterMigration(t *testing.T) {
	stateDir := setStateDir(t)
	repo := initTestRepo(t, "feature-readthrough")

	wipDir := filepath.Join(repo, ".wip", "20260101000000-feature-readthrough")
	writeStatusJSON(t, wipDir, "feature-readthrough", "planned")
	if err := os.WriteFile(filepath.Join(wipDir, "plan.md"), []byte("# Plan\n\nSome real plan content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := Run(stateDir, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Migrated) != 1 {
		t.Fatalf("expected 1 migrated entry, got %+v", summary)
	}

	got, err := wip.FindBranchState(repo, "feature-readthrough")
	if err != nil {
		t.Fatalf("wip.FindBranchState failed after migration (read-through broken): %v", err)
	}
	if got.WipDir != wipDir {
		t.Fatalf("wip.FindBranchState resolved %s, want %s", got.WipDir, wipDir)
	}
	if got.Status.Branch != "feature-readthrough" {
		t.Fatalf("status.Branch = %s, want feature-readthrough", got.Status.Branch)
	}
	if got.Status.Stage != "planned" {
		t.Fatalf("status.Stage = %s, want planned", got.Status.Stage)
	}

	// The source plan.md itself is still directly readable exactly as
	// written — migration never deleted or moved it.
	data, err := os.ReadFile(filepath.Join(wipDir, "plan.md"))
	if err != nil {
		t.Fatalf("source plan.md no longer readable: %v", err)
	}
	if string(data) != "# Plan\n\nSome real plan content.\n" {
		t.Fatalf("source plan.md content changed: %q", data)
	}
}
