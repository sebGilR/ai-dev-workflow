package migrate

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"aidw/cmd/aidw/internal/state"
	"aidw/cmd/aidw/internal/util"
)

// Mapping is the on-disk shape of $AIDW_STATE_DIR/migrations/wip-paths.json
// (§2a Q3): a pointer schema, not digest duplication. Per-file checksums are
// NOT stored here — Provenance.SourceHashes on the referenced work.json is
// the single source of truth for those; duplicating them into this file
// would create a second copy that can silently drift from the record it
// describes.
type Mapping struct {
	SchemaVersion int                     `json:"schema_version"`
	Entries       map[string]MappingEntry `json:"entries"` // key: normalized sourceWipDir
}

// MappingEntry records which work_id/repo_id a given (normalized)
// sourceWipDir was migrated into, and when it was last verified clean.
//
// verified_at records when verification last succeeded, not a standing
// guarantee: a MappingEntry whose WorkID has no corresponding work.json on
// disk (work.Load fails) MUST be treated as unverified, never as "safe" on
// the strength of this field alone (§2a Q3's dangling-pointer handling).
type MappingEntry struct {
	WorkID     string `json:"work_id"`
	RepoID     string `json:"repo_id"`
	VerifiedAt string `json:"verified_at"`
}

// mappingPath returns $stateDir/migrations/wip-paths.json. Computed inline
// here (no new state package surface needed — state.StateDir() is already
// exported and sufficient).
func mappingPath(stateDir string) string {
	return filepath.Join(stateDir, "migrations", "wip-paths.json")
}

// loadMapping reads wip-paths.json under stateDir. A missing file loads as
// an empty Mapping{SchemaVersion: 1, Entries: {}}, not an error (mirrors
// state.LoadRepos's missing-file convention).
func loadMapping(stateDir string) (*Mapping, error) {
	path := mappingPath(stateDir)
	var m Mapping
	if err := util.ReadJSON(path, &m); err != nil {
		if os.IsNotExist(err) {
			return &Mapping{SchemaVersion: 1, Entries: map[string]MappingEntry{}}, nil
		}
		return nil, fmt.Errorf("load wip-paths.json: %w", err)
	}
	if m.Entries == nil {
		m.Entries = map[string]MappingEntry{}
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = 1
	}
	return &m, nil
}

// mappingLockAttempts / mappingLockBackoff bound the retry around
// wip-paths.json, mirroring state.acquireReposLock's shape exactly
// (state/repos.go:52-55) — same attempt count, same base backoff.
const (
	mappingLockAttempts = 8
	mappingLockBackoff  = 15 * time.Millisecond
)

// acquireMappingLock wraps state.AcquireLock (the exported primitive; hard
// rule 6) with a small jittered retry, re-implemented here rather than
// calling into state.acquireReposLock, which is unexported (§2a Q2).
// wip-paths.json is — like repos.json — a single global file with
// potentially many concurrent writers across unrelated directories, so the
// same "retry, don't fail fast" shape applies here as it does there.
func acquireMappingLock(target string) (func(), error) {
	var lastErr error
	for attempt := 0; attempt < mappingLockAttempts; attempt++ {
		release, err := state.AcquireLock(target)
		if err == nil {
			return release, nil
		}
		lastErr = err
		if attempt == mappingLockAttempts-1 {
			break
		}
		jitter := time.Duration(rand.Int63n(int64(mappingLockBackoff)))
		time.Sleep(mappingLockBackoff + jitter)
	}
	return nil, fmt.Errorf("wip-paths.json lock busy after %d attempts: %w", mappingLockAttempts, lastErr)
}

// Update performs a load-mutate-save cycle on wip-paths.json under a single
// state.AcquireLock hold (via acquireMappingLock's bounded retry), mirroring
// work.UpdateRecord's shape (store.go:145) — the same
// load-outside-the-lock-then-save-inside-it race that shape exists to
// prevent applies here too, since wip-paths.json is a single shared file
// across concurrent migrate-state invocations.
func Update(stateDir string, mutate func(*Mapping) error) error {
	path := mappingPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("update wip-paths.json: mkdir: %w", err)
	}

	release, err := acquireMappingLock(path)
	if err != nil {
		return fmt.Errorf("update wip-paths.json: %w", err)
	}
	defer release()

	m, err := loadMapping(stateDir)
	if err != nil {
		return err
	}
	if mutate != nil {
		if err := mutate(m); err != nil {
			return fmt.Errorf("update wip-paths.json: %w", err)
		}
	}
	if err := util.WriteJSON(path, m); err != nil {
		return fmt.Errorf("update wip-paths.json: write: %w", err)
	}
	return nil
}

// SourcesFor returns every wip-paths.json (normalized) sourceWipDir key
// whose entry points at workID, without deleting anything — a read-only
// preview used by `work purge <id> --dry-run` to show what ForgetSource
// would remove. Mirrors SessionIDsBoundTo's role for
// DeleteSessionBindingsFor.
func SourcesFor(stateDir, workID string) ([]string, error) {
	m, err := loadMapping(stateDir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for key, entry := range m.Entries {
		if entry.WorkID == workID {
			out = append(out, key)
		}
	}
	return out, nil
}

// ForgetSource removes every wip-paths.json entry whose WorkID equals
// workID, returning the normalized sourceWipDir keys that were removed.
//
// Exists to close a gap Cluster I's review found: work purge deletes
// work/<id>/ but has no knowledge of (and correctly no access to)
// wip-paths.json — internal/work never imports internal/migrate. Without
// this, a purged record's mapping entry survives pointing at a work_id that
// no longer exists; the next plain migrate-state run finds that entry,
// routes to reverify, gets ErrNotFound, and — per §2a Q3's
// dangling-pointer-recovery rule, designed for a crash between Update and
// Save, not for deliberate permanent deletion — re-mints a brand-new record
// from the still-on-disk legacy source. cmd/aidw/cmd/work.go's workPurgeCmd
// calls this immediately after DeleteRecord succeeds, exactly parallel to
// how session-binding reap is wired there: single-responsibility, called
// from the command that has visibility into both packages, not folded into
// DeleteRecord itself (internal/work must not import internal/migrate).
func ForgetSource(stateDir, workID string) (removed []string, err error) {
	// Accumulated inside the closure, only surfaced to the caller once
	// Update has actually committed the write — if WriteJSON fails after
	// mutate ran, nothing was persisted, so the caller must not be told
	// entries were removed that are, in fact, still on disk.
	var deleted []string
	err = Update(stateDir, func(m *Mapping) error {
		for key, entry := range m.Entries {
			if entry.WorkID == workID {
				delete(m.Entries, key)
				deleted = append(deleted, key)
			}
		}
		return nil
	})
	if err != nil {
		return []string{}, fmt.Errorf("forget source for %s: %w", workID, err)
	}
	if deleted == nil {
		deleted = []string{}
	}
	return deleted, nil
}
