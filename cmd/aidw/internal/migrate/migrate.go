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

// errConcurrentlyMapped is returned by migrateNew's Update mutate closure
// (and surfaces wrapped through migrateNew's own return) when normKey is
// found already present in the mapping at the moment the closure runs —
// i.e. another migrateNew call (in this process or a concurrent one) won
// the race to map this same source first. See the duplicate-mint race fix
// below and AC-I2-MIGRATE-NORACE.
var errConcurrentlyMapped = errors.New("migrated concurrently by another run")

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

// verifyTwoSided is the single shared implementation of this cluster's most
// safety-critical rule: a source is only "still good" if BOTH the current
// source files AND the current destination attachment files match the
// record's stored Provenance.SourceHashes. Comparing only the source side
// would miss destination-side corruption/drift entirely, since an
// unmodified source always still matches the digest that was itself
// computed from that same source — this is what stands between
// --cleanup-sources and deleting a source whose copy is corrupt (review.md
// #7: this exact duplication, maintained by hand across reverify and
// PlanCleanup, was already the proximate cause of one Batch 3 R1 test-gap
// finding).
//
// It deliberately does NOT call work.Load or classify a dangling pointer —
// reverify and PlanCleanup disagree on purpose about how to handle a load
// failure (reverify re-migrates on ErrNotFound specifically, per Batch 3 R3
// finding 2; PlanCleanup blocks on ANY load failure) and folding that
// decision in here would either reintroduce the auto-repoint bug or change
// PlanCleanup's policy. Callers load the record themselves and pass in only
// what they already have.
//
// err is returned only for an I/O failure while hashing either side — the
// two callers each route that to their own distinct failure bucket
// (Summary.Skipped vs. a blocked reason). ok/reason report a genuine
// checksum mismatch, which the two callers also route differently
// (Summary.Divergent vs. a blocked reason) — the split is preserved by
// leaving both decisions at the call site.
func verifyTwoSided(sourceWipDir, attachmentsDir string, want map[string]string) (ok bool, reason string, err error) {
	srcHashes, err := HashTree(sourceWipDir)
	if err != nil {
		return false, "", fmt.Errorf("hash source %s: %w", sourceWipDir, err)
	}
	destHashes, err := HashTree(attachmentsDir)
	if err != nil {
		return false, "", fmt.Errorf("hash destination %s: %w", attachmentsDir, err)
	}
	if !hashesEqual(srcHashes, want) || !hashesEqual(destHashes, want) {
		return false, "checksum mismatch against the migrated record: source or destination has drifted since migration", nil
	}
	return true, "", nil
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
// Options configures RunWithOptions. The zero value matches Run's own
// historical, unconditional-branch-dir-only behavior.
type Options struct {
	// IncludeGlobalArchive, when true, additionally discovers and migrates
	// .wip/.archive/ entries (whole condemned branch trees) via
	// DiscoverArchived/ConvertArchived, born Lifecycle: Archived. Opt-in,
	// default false — this does not change plain migrate-state's default
	// behavior.
	IncludeGlobalArchive bool
}

// A lock-contention failure on the target work.json after retrying is
// recorded into Summary.Skipped with a reason, and the run continues to the
// next source dir. Run's caller (migrate_state.go) exits non-zero if
// Summary.Divergent or Summary.Skipped is non-empty.
func Run(stateDir string, roots []string) (*Summary, error) {
	return RunWithOptions(stateDir, roots, Options{})
}

// RunWithOptions is Run, with §2.4's --include-global-archive behavior
// folded in when opts.IncludeGlobalArchive is set. Every existing Run
// caller/test keeps compiling and behaving unchanged (Run is now a thin
// wrapper calling this with the zero-value Options).
func RunWithOptions(stateDir string, roots []string, opts Options) (*Summary, error) {
	sources, err := Discover(roots)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}
	if opts.IncludeGlobalArchive {
		archived, err := DiscoverArchived(roots)
		if err != nil {
			return nil, fmt.Errorf("discover archived: %w", err)
		}
		sources = append(sources, archived...)
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
	var record *work.Record
	var err error
	if src.Kind == SourceKindGlobalArchive {
		record, err = ConvertArchived(src)
	} else {
		record, err = Convert(src)
	}
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
		// Duplicate-mint race fix (R6, AC-I2-MIGRATE-NORACE): re-check the
		// precondition under the SAME lock Update already holds, right
		// before writing. Two concurrent whole-process migrate-state runs
		// discovering the same unmapped source could otherwise both reach
		// this point (both having already converted+copied), race on this
		// write, and let the loser mint a permanently orphaned duplicate
		// work.Record for the same source — since both records parse
		// cleanly, nothing in ScanRecords surfaces the duplication and it
		// silently wedges work.Resolve behind ErrAmbiguousWork. This
		// existence check closes that: the loser's migrateNew call now
		// fails before work.Save ever runs, leaving only a stray,
		// already-orphaned attachments/ copy on disk (no phantom
		// work.Record) — the accepted, documented cost of the fix.
		//
		// Scoped to !withRetry (the genuinely-new-entry path, called only
		// from Run's `!exists` branch) — withRetry==true is reverify's
		// dangling-pointer recovery, which is deliberately called BECAUSE a
		// stale mapping entry already exists and is legitimately being
		// replaced; applying this same existence check there would refuse
		// every ordinary dangling-pointer recovery, not just a genuine
		// concurrent racer.
		if _, exists := m.Entries[normKey]; exists && !withRetry {
			return errConcurrentlyMapped
		}
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

	attachmentsDir := filepath.Join(work.Dir(stateDir, entry.WorkID), "attachments")
	ok, _, err := verifyTwoSided(src.SourceWipDir, attachmentsDir, record.Provenance.SourceHashes)
	if err != nil {
		return err
	}

	if ok {
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
