package migrate

import (
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"time"

	"aidw/cmd/aidw/internal/util"
	"aidw/cmd/aidw/internal/work"
)

// Summary reports the outcome of one Run.
type Summary struct {
	Migrated  []string `json:"migrated"`  // sourceWipDir values, newly migrated this run
	Verified  []string `json:"verified"`  // sourceWipDir values, already-mapped, re-verified clean
	Divergent []string `json:"divergent"` // sourceWipDir values, already-mapped, checksum mismatch — reported, skipped
	Skipped   []string `json:"skipped"`   // sourceWipDir values skipped for any other reason

	// SkippedReasons maps each Skipped sourceWipDir to why it was skipped.
	// Kept as a side map (rather than folding the reason into the Skipped
	// string itself) so Skipped stays a plain list of sourceWipDir values,
	// matching every other Summary field's shape.
	SkippedReasons map[string]string `json:"skipped_reasons,omitempty"`
}

func newSummary() *Summary {
	return &Summary{
		Migrated:       []string{},
		Verified:       []string{},
		Divergent:      []string{},
		Skipped:        []string{},
		SkippedReasons: map[string]string{},
	}
}

func (s *Summary) recordSkipped(sourceWipDir, reason string) {
	s.Skipped = append(s.Skipped, sourceWipDir)
	s.SkippedReasons[sourceWipDir] = reason
}

// lockRetryAttempts / lockRetryBackoff bound the retry used ONLY on the
// re-verify path's dangling-pointer recovery (§2a Q2) — never wrapped
// around every work.json write by default. A fresh migration target is a
// brand-new work_id from work.New; nothing else in the system could hold a
// lock on a work.json path that doesn't exist yet, so the ordinary new-entry
// path does not use this.
const (
	lockRetryAttempts = 8
	lockRetryBackoff  = 15 * time.Millisecond
)

// saveRecordWithRetry wraps work.Save with a bounded jittered retry,
// mirroring state.acquireReposLock's shape (re-implemented here, not called
// into — acquireReposLock is unexported, §2a Q2). Used only where a
// concurrent `aidw work checkpoint` could plausibly be contending for the
// same work.json (the re-verify path's dangling-pointer recovery).
func saveRecordWithRetry(stateDir string, r *work.Record) error {
	var lastErr error
	for attempt := 0; attempt < lockRetryAttempts; attempt++ {
		err := work.Save(stateDir, r)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == lockRetryAttempts-1 {
			break
		}
		jitter := time.Duration(rand.Int63n(int64(lockRetryBackoff)))
		time.Sleep(lockRetryBackoff + jitter)
	}
	return lastErr
}

// hashesEqual reports whether a and b contain exactly the same set of
// (relative path -> digest) pairs.
func hashesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// Run discovers every source dir under roots, and for each one:
//   - if wip-paths.json has no entry for it (normalized) yet: Convert +
//     copy + verify + work.Save the new record (never a raw file write,
//     hard rule 5) + Update the mapping with a fresh entry. Never deletes
//     src.SourceWipDir or anything in it.
//   - if wip-paths.json already has an entry: re-verify per §2a Q3's
//     "dangling pointer" handling — work.Load the mapped work_id; if that
//     fails for ANY reason, treat as unverified (never trust the stale
//     verified_at) and re-run the full new-entry pipeline as if the source
//     were unmapped, replacing the stale mapping entry with a fresh
//     work_id. If the load succeeds, recompute sha256 over every file
//     currently under src.SourceWipDir and compare against the loaded
//     record's Provenance.SourceHashes as a SET. A match records into
//     Summary.Verified with a refreshed verified_at. A mismatch (missing
//     file, extra file, or digest difference) is R4: report into
//     Summary.Divergent, skip this entry, continue to the next one, never
//     auto-resolve, never overwrite either side.
//
// A lock-contention failure on the target work.json after retrying is
// recorded into Summary.Skipped with a reason, and the run continues to the
// next source dir. Run's caller (migrate_state.go) exits non-zero if
// Summary.Divergent or Summary.Skipped is non-empty.
func Run(stateDir string, roots []string) (*Summary, error) {
	sources, err := Discover(roots)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}

	summary := newSummary()

	for _, src := range sources {
		normKey, err := filepath.EvalSymlinks(src.SourceWipDir)
		if err != nil {
			summary.recordSkipped(src.SourceWipDir, fmt.Sprintf("resolve source path: %v", err))
			continue
		}

		mapping, err := loadMapping(stateDir)
		if err != nil {
			// A read failure here must not discard the Summary already
			// earned by every source processed earlier in this loop
			// (Batch 3, R3 finding 8) — record this one entry as skipped
			// and continue to the next source, matching the "every other
			// entry still completes" guarantee this function makes for
			// every other kind of per-entry failure.
			summary.recordSkipped(src.SourceWipDir, fmt.Sprintf("load mapping: %v", err))
			continue
		}
		entry, exists := mapping.Entries[normKey]

		var procErr error
		if !exists {
			procErr = migrateNew(stateDir, src, normKey, summary, false)
		} else {
			procErr = reverify(stateDir, src, normKey, entry, summary)
		}
		if procErr != nil {
			summary.recordSkipped(src.SourceWipDir, procErr.Error())
		}
	}

	return summary, nil
}

// migrateNew converts, copies+verifies, maps, then saves one previously-
// unmapped source dir. withRetry selects work.Save (ordinary, fail-fast —
// nothing could be holding this brand-new work_id's lock) vs
// saveRecordWithRetry (used only when this is really the dangling-pointer
// recovery path inside reverify, which is executing "as if new" but for a
// sourceWipDir that WAS previously mapped and could concurrently be
// checkpointed against under its old, now-stale, work_id).
//
// The mapping write happens BEFORE work.Save, deliberately reversed from a
// naive "save then map" order (Batch 3, R3 finding 1). Any interruption
// between the two steps — lock contention on wip-paths.json exhausting its
// bounded retry, a crash, a process kill — must never leave a COMPLETE,
// VALID, unmapped work.json lying around: the next Run would see no mapping
// entry for this source and mint a second, duplicate record for it via a
// fresh work.New ULID, and since both records parse cleanly neither lands in
// ScanRecords' skipped list, so nothing surfaces the duplication — it just
// silently wedges work.Resolve behind ErrAmbiguousWork for that worktree.
// Mapping-then-save makes the failure mode a dangling pointer instead: if
// Update succeeds but Save fails, work.Load(record.WorkID) on the next run
// gets ErrNotFound (nothing was ever written there), which reverify's
// existing dangling-pointer recovery already re-migrates correctly as new.
// If Update itself fails, this function returns before Save ever runs, so no
// orphan work.json is created in that failure case either.
func migrateNew(stateDir string, src SourceDir, normKey string, summary *Summary, withRetry bool) error {
	record, err := Convert(src)
	if err != nil {
		return fmt.Errorf("convert %s: %w", src.SourceWipDir, err)
	}

	attachmentsDir := filepath.Join(work.Dir(stateDir, record.WorkID), "attachments")
	hashes, err := CopyAttachments(src.SourceWipDir, attachmentsDir)
	if err != nil {
		return fmt.Errorf("copy attachments for %s: %w", src.SourceWipDir, err)
	}
	record.Provenance.SourceHashes = hashes

	repoID := ""
	if len(record.Attachments) > 0 {
		repoID = record.Attachments[0].RepoID
	}

	if err := Update(stateDir, func(m *Mapping) error {
		m.Entries[normKey] = MappingEntry{
			WorkID:     record.WorkID,
			RepoID:     repoID,
			VerifiedAt: util.NowISO(),
		}
		return nil
	}); err != nil {
		return fmt.Errorf("update mapping for %s: %w", src.SourceWipDir, err)
	}

	saveFn := work.Save
	if withRetry {
		saveFn = saveRecordWithRetry
	}
	if err := saveFn(stateDir, record); err != nil {
		return fmt.Errorf("save work record for %s: %w", src.SourceWipDir, err)
	}

	summary.Migrated = append(summary.Migrated, src.SourceWipDir)
	return nil
}

// reverify handles an already-mapped source dir: §2a Q3's dangling-pointer
// rule (work.Load failure => treat as unverified, never trust verified_at)
// plus R4's divergence detection for a mapped entry whose backing record
// still exists.
//
// Full verification checks BOTH sides against the record's stored
// Provenance.SourceHashes (captured from the source at migration time): the
// CURRENT source files (catches source-side drift since migration) and the
// CURRENT destination attachment files (catches destination-side
// corruption/drift, e.g. a hand-edited or truncated attachment blob) — a
// mismatch on either side is R4 divergence. Comparing only the source
// against the stored digest would miss destination corruption entirely,
// since an unmodified source always still matches the digest that was
// itself computed from that same source.
func reverify(stateDir string, src SourceDir, normKey string, entry MappingEntry, summary *Summary) error {
	record, err := work.Load(stateDir, entry.WorkID)
	if err != nil {
		if errors.Is(err, work.ErrNotFound) {
			// Dangling pointer (§2a Q3): the mapped work_id has no
			// corresponding work.json at all. verified_at is NOT trusted
			// on its own — re-run the full migration pipeline against the
			// source files as if this entry were new, replacing the stale
			// mapping entry. Safe to auto-repoint: nothing valid is being
			// orphaned, since there was never a record here to begin with.
			return migrateNew(stateDir, src, normKey, summary, true)
		}
		// Present but unreadable for some OTHER reason (e.g.
		// ErrUnsupportedSchemaVersion, corrupt JSON) — this is NOT a
		// dangling pointer (Batch 3, R3 finding 2) and must not be
		// silently repointed: the mapped work.json still exists on disk
		// and may be recoverable (e.g. by a future schema migration).
		// Auto-repointing here would orphan a real record (unmapped, but
		// still present — it would surface as a skipped/unreadable entry
		// in ScanRecords) while also minting a second live record for the
		// same source. Report and leave the mapping untouched instead —
		// a human needs to resolve this, not this migration.
		return fmt.Errorf("mapped work record %s is present but unreadable, not re-migrating: %w", entry.WorkID, err)
	}

	srcHashes, err := HashTree(src.SourceWipDir)
	if err != nil {
		return fmt.Errorf("hash source %s: %w", src.SourceWipDir, err)
	}
	attachmentsDir := filepath.Join(work.Dir(stateDir, entry.WorkID), "attachments")
	destHashes, err := HashTree(attachmentsDir)
	if err != nil {
		return fmt.Errorf("hash destination %s: %w", attachmentsDir, err)
	}

	if hashesEqual(srcHashes, record.Provenance.SourceHashes) && hashesEqual(destHashes, record.Provenance.SourceHashes) {
		if err := Update(stateDir, func(m *Mapping) error {
			entry.VerifiedAt = util.NowISO()
			m.Entries[normKey] = entry
			return nil
		}); err != nil {
			return fmt.Errorf("refresh mapping for %s: %w", src.SourceWipDir, err)
		}
		summary.Verified = append(summary.Verified, src.SourceWipDir)
		return nil
	}

	// R4: a mismatch is reported and skipped — never auto-resolved, never
	// overwriting either side.
	summary.Divergent = append(summary.Divergent, src.SourceWipDir)
	return nil
}
