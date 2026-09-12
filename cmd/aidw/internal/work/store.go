package work

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"aidw/cmd/aidw/internal/state"
	"aidw/cmd/aidw/internal/util"
)

// Dir returns the on-disk directory for one work record.
func Dir(stateDir, workID string) string {
	return filepath.Join(stateDir, "work", workID)
}

// RecordPath returns the on-disk path to a work record's work.json.
func RecordPath(stateDir, workID string) string {
	return filepath.Join(Dir(stateDir, workID), "work.json")
}

// New constructs a Record with a fresh ULID, both schema_version fields set
// to CurrentSchemaVersion, lifecycle=active, and provenance timestamps set
// via util.NowISO(). Does not save.
func New(title string, mode Mode) *Record {
	now := util.NowISO()
	return &Record{
		SchemaVersion: CurrentSchemaVersion,
		WorkID:        NewID(),
		Title:         title,
		Mode:          mode,
		Lifecycle:     LifecycleActive,
		// Every slice/map field is initialized non-nil so the serialized
		// work.json matches the design doc's §2 example ([] and {}), never
		// JSON null: consumers that iterate these keys must not have to
		// special-case null.
		Context: Context{
			Constraints:   []string{},
			Decisions:     []string{},
			OpenQuestions: []string{},
		},
		Attachments: []Attachment{},
		Provenance: Provenance{
			SchemaVersion: CurrentSchemaVersion,
			CreatedAt:     now,
			UpdatedAt:     now,
			SourceHashes:  map[string]string{},
		},
	}
}

// Load reads work/<workID>/work.json. Returns ErrUnsupportedSchemaVersion
// (wrapped with the found vs. expected version) if EITHER Record.SchemaVersion
// or Provenance.SchemaVersion is zero or != CurrentSchemaVersion — never
// silently proceeds, never panics.
//
// Both fields are checked because the design doc (§2, "Field notes") names
// provenance.schema_version as the field that "lets work.json readers detect
// and reject (or upgrade) a record written by a future incompatible schema".
// Validating only the top-level copy would make that field dead weight, and
// a record whose two version fields disagree is by definition not a record
// this build knows how to interpret.
func Load(stateDir, workID string) (*Record, error) {
	path := RecordPath(stateDir, workID)
	var r Record
	if err := util.ReadJSON(path, &r); err != nil {
		return nil, fmt.Errorf("load work record %s: %w", workID, err)
	}
	if r.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: found %d, expected %d", ErrUnsupportedSchemaVersion, r.SchemaVersion, CurrentSchemaVersion)
	}
	if r.Provenance.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: provenance.schema_version found %d, expected %d", ErrUnsupportedSchemaVersion, r.Provenance.SchemaVersion, CurrentSchemaVersion)
	}
	return &r, nil
}

// Save acquires state.AcquireLock on the record's work.json path (creating
// work/<workID>/ first), sets Provenance.UpdatedAt = util.NowISO(), writes
// both SchemaVersion fields to CurrentSchemaVersion, and calls
// util.WriteJSON for the atomic write. Releases the lock via defer on both
// success and error paths.
//
// Save writes whatever in-memory Record it is handed, blind to what is
// currently on disk. Any caller whose update is a function of the CURRENT
// on-disk contents (append an attachment, edit context, bump a stage) must
// use UpdateRecord instead — a Load outside the lock followed by a Save
// inside it is a read-modify-write race that silently drops the other
// writer's change.
func Save(stateDir string, r *Record) error {
	path := RecordPath(stateDir, r.WorkID)
	if err := os.MkdirAll(Dir(stateDir, r.WorkID), 0o755); err != nil {
		return fmt.Errorf("save work record %s: mkdir: %w", r.WorkID, err)
	}

	release, err := state.AcquireLock(path)
	if err != nil {
		return fmt.Errorf("save work record %s: %w", r.WorkID, err)
	}
	defer release()

	return saveLocked(path, r)
}

// saveLocked performs the stamp-and-write half of Save. The caller must
// already hold state.AcquireLock on path. Kept separate so UpdateRecord can
// write inside the same lock hold it loaded under: state.AcquireLock is
// flock(2)-backed and per open file description, so a nested AcquireLock in
// the same process contends with itself rather than re-entering.
func saveLocked(path string, r *Record) error {
	r.SchemaVersion = CurrentSchemaVersion
	r.Provenance.SchemaVersion = CurrentSchemaVersion
	r.Provenance.UpdatedAt = util.NowISO()

	if err := util.WriteJSON(path, r); err != nil {
		return fmt.Errorf("save work record %s: write: %w", r.WorkID, err)
	}
	return nil
}

// UpdateRecord performs a load-mutate-save cycle as one atomic critical
// section: it acquires state.AcquireLock on work/<workID>/work.json, reads
// the CURRENT on-disk record, hands it to mutate, writes the result, and
// only then releases the lock. It returns the saved record.
//
// This is the only correct way to apply an update that depends on existing
// state. The `Load` (unlocked) → mutate → `Save` (locks only the write)
// pattern it replaces loses updates: two concurrent `work attach` runs each
// read the same pre-state and the second write clobbers the first, so one
// attachment silently disappears.
//
// If mutate returns an error, nothing is written and that error is returned
// (wrapped). A load failure — missing record, corrupt JSON, unsupported
// schema version — is propagated as-is, so `--work <bogus-id>` still fails
// loudly rather than creating a record.
//
// Contention behaviour matches Save: state.AcquireLock is non-blocking, so a
// second concurrent updater of the SAME record gets an "is held" error
// immediately rather than queueing. Loud failure is the point — the previous
// behaviour was a silent lost update. Callers that want to survive
// contention should retry.
func UpdateRecord(stateDir, workID string, mutate func(*Record) error) (*Record, error) {
	path := RecordPath(stateDir, workID)
	if err := os.MkdirAll(Dir(stateDir, workID), 0o755); err != nil {
		return nil, fmt.Errorf("update work record %s: mkdir: %w", workID, err)
	}

	release, err := state.AcquireLock(path)
	if err != nil {
		return nil, fmt.Errorf("update work record %s: %w", workID, err)
	}
	defer release()

	r, err := Load(stateDir, workID)
	if err != nil {
		return nil, err
	}
	if mutate != nil {
		if err := mutate(r); err != nil {
			return nil, fmt.Errorf("update work record %s: %w", workID, err)
		}
	}
	if err := saveLocked(path, r); err != nil {
		return nil, err
	}
	return r, nil
}

// ListRecords scans work/*/work.json under stateDir. A record directory
// whose work.json is missing or fails to parse is skipped (with a warning
// to stderr) rather than failing the whole scan — a concurrent `work
// start` mid-scan must not break `work list`. No cached index is read or
// written (files are the source of truth).
func ListRecords(stateDir string) ([]*Record, error) {
	records, _, err := ScanRecords(stateDir)
	return records, err
}

// ScanRecords is ListRecords plus the second fact ListRecords throws away:
// the ids of the record directories that exist but could NOT be read during
// this scan — unparseable JSON, or a schema version this build rejects.
// A directory with NO work.json at all is not skipped, it is simply absent
// (see the inline note below).
//
// A skipped record is NOT the same as an absent record, and any caller that
// makes a decision from the SHAPE of the result set — "exactly one match,
// therefore resolve confidently" — must know the scan was incomplete.
// Silently collapsing a skipped record into "doesn't exist" turns genuine
// ambiguity into a confident wrong answer (see Resolve).
//
// A skip is still not a hard error: a concurrent `work start` mid-scan must
// not break `work list`, which is why ListRecords keeps its lenient
// signature and remains the right call for pure listing.
func ScanRecords(stateDir string) (records []*Record, skipped []string, err error) {
	workRoot := filepath.Join(stateDir, "work")
	entries, err := os.ReadDir(workRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("list work records: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		r, loadErr := Load(stateDir, entry.Name())
		if loadErr != nil {
			// A MISSING work.json is an absent record, not a skipped one.
			// Two ordinary situations produce a record directory with no
			// work.json: a concurrent `work start` between its MkdirAll and
			// its (atomic) first write, and a directory left behind by a
			// command that errored before writing — e.g. `work attach
			// --work <typo>`. util.WriteJSON writes via temp+rename, so an
			// existing record's work.json is never transiently missing;
			// missing therefore means "not a record", and counting it as an
			// incomplete scan would let one stray empty directory poison
			// every resolution in the state dir indefinitely.
			if errors.Is(loadErr, fs.ErrNotExist) {
				continue
			}
			fmt.Fprintf(os.Stderr, "[aidw] skipping unreadable work record %s: %v\n", entry.Name(), loadErr)
			skipped = append(skipped, entry.Name())
			continue
		}
		records = append(records, r)
	}
	sort.Strings(skipped)
	return records, skipped, nil
}
