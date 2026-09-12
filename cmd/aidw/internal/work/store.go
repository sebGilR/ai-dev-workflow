package work

import (
	"fmt"
	"os"
	"path/filepath"

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
		Attachments:   []Attachment{},
		Provenance: Provenance{
			SchemaVersion: CurrentSchemaVersion,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
}

// Load reads work/<workID>/work.json. Returns ErrUnsupportedSchemaVersion
// (wrapped with the found vs. expected version) if Record.SchemaVersion is
// zero or != CurrentSchemaVersion — never silently proceeds, never panics.
func Load(stateDir, workID string) (*Record, error) {
	path := RecordPath(stateDir, workID)
	var r Record
	if err := util.ReadJSON(path, &r); err != nil {
		return nil, fmt.Errorf("load work record %s: %w", workID, err)
	}
	if r.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: found %d, expected %d", ErrUnsupportedSchemaVersion, r.SchemaVersion, CurrentSchemaVersion)
	}
	return &r, nil
}

// Save acquires state.AcquireLock on the record's work.json path (creating
// work/<workID>/ first), sets Provenance.UpdatedAt = util.NowISO(), writes
// both SchemaVersion fields to CurrentSchemaVersion, and calls
// util.WriteJSON for the atomic write. Releases the lock via defer on both
// success and error paths.
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

	r.SchemaVersion = CurrentSchemaVersion
	r.Provenance.SchemaVersion = CurrentSchemaVersion
	r.Provenance.UpdatedAt = util.NowISO()

	if err := util.WriteJSON(path, r); err != nil {
		return fmt.Errorf("save work record %s: write: %w", r.WorkID, err)
	}
	return nil
}

// ListRecords scans work/*/work.json under stateDir. A record directory
// whose work.json is missing or fails to parse is skipped (with a warning
// to stderr) rather than failing the whole scan — a concurrent `work
// start` mid-scan must not break `work list`. No cached index is read or
// written (files are the source of truth).
func ListRecords(stateDir string) ([]*Record, error) {
	workRoot := filepath.Join(stateDir, "work")
	entries, err := os.ReadDir(workRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list work records: %w", err)
	}

	var records []*Record
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		r, err := Load(stateDir, entry.Name())
		if err != nil {
			fmt.Fprintf(os.Stderr, "[aidw] skipping unreadable work record %s: %v\n", entry.Name(), err)
			continue
		}
		records = append(records, r)
	}
	return records, nil
}
