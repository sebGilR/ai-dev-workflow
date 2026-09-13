package migrate

import (
	"fmt"
	"os"
	"path/filepath"

	"aidw/cmd/aidw/internal/work"
)

// PlanCleanup discovers every source dir under roots and classifies each one
// as a deletion candidate or blocked. A source is a candidate only if:
//   - it has a mapping entry in wip-paths.json, AND
//   - work.Load succeeds for that entry's WorkID (not a dangling pointer,
//     §2a Q3 — a load failure NEVER makes a source a candidate, regardless
//     of how recent verified_at is), AND
//   - recomputed sha256 hashes over BOTH the current source files and the
//     current destination attachment files match record.Provenance.SourceHashes
//     exactly (the same two-sided rigor as migrate.go's reverify — a
//     source-only comparison cannot detect a corrupted/modified
//     destination).
//
// Every other source is blocked, with a reason recorded. PlanCleanup never
// deletes anything itself — it only classifies. It performs no mutation of
// wip-paths.json or any work record.
func PlanCleanup(stateDir string, roots []string) (candidates []string, blocked map[string]string, err error) {
	sources, err := Discover(roots)
	if err != nil {
		return nil, nil, fmt.Errorf("discover: %w", err)
	}

	blocked = map[string]string{}

	for _, src := range sources {
		normKey, err := filepath.EvalSymlinks(src.SourceWipDir)
		if err != nil {
			blocked[src.SourceWipDir] = fmt.Sprintf("resolve source path: %v", err)
			continue
		}

		mapping, err := loadMapping(stateDir)
		if err != nil {
			return nil, nil, fmt.Errorf("load mapping: %w", err)
		}
		entry, exists := mapping.Entries[normKey]
		if !exists {
			blocked[src.SourceWipDir] = "not yet migrated: no wip-paths.json entry"
			continue
		}

		record, err := work.Load(stateDir, entry.WorkID)
		if err != nil {
			// Dangling pointer (§2a Q3): never trust verified_at alone when
			// the mapped work_id has no corresponding work.json.
			blocked[src.SourceWipDir] = fmt.Sprintf("dangling mapping entry (work_id %s): %v", entry.WorkID, err)
			continue
		}

		srcHashes, err := HashTree(src.SourceWipDir)
		if err != nil {
			blocked[src.SourceWipDir] = fmt.Sprintf("hash source: %v", err)
			continue
		}
		attachmentsDir := filepath.Join(work.Dir(stateDir, entry.WorkID), "attachments")
		destHashes, err := HashTree(attachmentsDir)
		if err != nil {
			blocked[src.SourceWipDir] = fmt.Sprintf("hash destination: %v", err)
			continue
		}

		if !hashesEqual(srcHashes, record.Provenance.SourceHashes) || !hashesEqual(destHashes, record.Provenance.SourceHashes) {
			blocked[src.SourceWipDir] = "checksum mismatch against the migrated record: source or destination has drifted since migration"
			continue
		}

		candidates = append(candidates, src.SourceWipDir)
	}

	return candidates, blocked, nil
}

// DeleteSources permanently removes every path in candidates via
// os.RemoveAll. Callers MUST have obtained candidates from PlanCleanup
// immediately prior in the same run — DeleteSources trusts its input
// completely and performs no re-verification of its own. It never touches
// wip-paths.json: a mapping entry's work_id/repo_id pointer remains valid
// and useful after its source is deleted, so cleanup never mutates the
// mapping.
func DeleteSources(candidates []string) error {
	for _, c := range candidates {
		if err := os.RemoveAll(c); err != nil {
			return fmt.Errorf("delete %s: %w", c, err)
		}
	}
	return nil
}
