package work

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"aidw/cmd/aidw/internal/util"
)

func TestRecordLoadSave_RoundTrip(t *testing.T) {
	stateDir := t.TempDir()

	r := New("test title", ModeDelivery)
	r.Attachments = append(r.Attachments, Attachment{
		RepoID:       "repo1",
		WorktreePath: "/some/path",
		Branch:       "main",
		Head:         "abc123",
	})
	if err := Save(stateDir, r); err != nil {
		t.Fatal(err)
	}

	got, err := Load(stateDir, r.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkID != r.WorkID || got.Title != r.Title {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, r)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].RepoID != "repo1" {
		t.Fatalf("attachments not round-tripped: %+v", got.Attachments)
	}
	if got.SchemaVersion != CurrentSchemaVersion || got.Provenance.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("schema version not written on both fields: %+v", got)
	}
}

func TestLoad_UnsupportedSchemaVersion(t *testing.T) {
	stateDir := t.TempDir()
	workID := "01TESTBADVERSION0000000001"
	dir := Dir(stateDir, workID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := RecordPath(stateDir, workID)
	if err := util.WriteJSON(path, map[string]any{
		"schema_version": 99,
		"work_id":        workID,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := Load(stateDir, workID)
	if !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("expected ErrUnsupportedSchemaVersion for version 99, got %v", err)
	}
}

func TestLoad_MissingSchemaVersionField(t *testing.T) {
	stateDir := t.TempDir()
	workID := "01TESTNOVERSION00000000001"
	dir := Dir(stateDir, workID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := RecordPath(stateDir, workID)
	if err := util.WriteJSON(path, map[string]any{
		"work_id": workID,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := Load(stateDir, workID)
	if !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("expected ErrUnsupportedSchemaVersion when field omitted, got %v", err)
	}
}

func TestListRecords_SkipsCorruptRecordDir(t *testing.T) {
	stateDir := t.TempDir()

	good := New("good", ModeDelivery)
	if err := Save(stateDir, good); err != nil {
		t.Fatal(err)
	}

	corruptID := "01TESTCORRUPT000000000001"
	corruptDir := Dir(stateDir, corruptID)
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RecordPath(stateDir, corruptID), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := ListRecords(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].WorkID != good.WorkID {
		t.Fatalf("expected only the good record, got %+v", records)
	}
}

func TestResolve_TwoWorkItemsSameBranch_SessionBindingsDisambiguate(t *testing.T) {
	stateDir := t.TempDir()

	att := Attachment{RepoID: "repoA", WorktreePath: "/repo/checkout", Branch: "main", Head: "sha1"}

	r1 := New("work one", ModeDelivery)
	r1.Attachments = append(r1.Attachments, att)
	if err := Save(stateDir, r1); err != nil {
		t.Fatal(err)
	}

	r2 := New("work two", ModeDelivery)
	r2.Attachments = append(r2.Attachments, att)
	if err := Save(stateDir, r2); err != nil {
		t.Fatal(err)
	}

	if err := SaveSessionBinding(stateDir, "session-1", r1.WorkID); err != nil {
		t.Fatal(err)
	}
	if err := SaveSessionBinding(stateDir, "session-2", r2.WorkID); err != nil {
		t.Fatal(err)
	}

	resolved1, _, err := Resolve(ResolveOptions{StateDir: stateDir, SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved1.WorkID != r1.WorkID {
		t.Fatalf("session-1 resolved to %s, want %s", resolved1.WorkID, r1.WorkID)
	}

	resolved2, _, err := Resolve(ResolveOptions{StateDir: stateDir, SessionID: "session-2"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved2.WorkID != r2.WorkID {
		t.Fatalf("session-2 resolved to %s, want %s", resolved2.WorkID, r2.WorkID)
	}

	// Read r2's raw bytes before a save through session-1's resolved record.
	r2Path := RecordPath(stateDir, r2.WorkID)
	before, err := os.ReadFile(r2Path)
	if err != nil {
		t.Fatal(err)
	}

	resolved1.Title = "work one, updated"
	if err := Save(stateDir, resolved1); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(r2Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("saving through session-1's resolved record modified r2's work.json on disk")
	}
}

func TestResolve_AmbiguousWorktreeAssociation_ListsCandidatesNoState(t *testing.T) {
	stateDir := t.TempDir()

	att := Attachment{RepoID: "repoA", WorktreePath: "/shared/checkout", Branch: "main", Head: "sha1"}

	r1 := New("work one", ModeDelivery)
	r1.Attachments = append(r1.Attachments, att)
	if err := Save(stateDir, r1); err != nil {
		t.Fatal(err)
	}
	r2 := New("work two", ModeDelivery)
	r2.Attachments = append(r2.Attachments, att)
	if err := Save(stateDir, r2); err != nil {
		t.Fatal(err)
	}

	before := snapshotDir(t, stateDir)

	_, candidates, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		SessionID:    "",
		RepoID:       "repoA",
		Branch:       "main",
		WorktreePath: "/shared/checkout",
	})
	if !errors.Is(err, ErrAmbiguousWork) {
		t.Fatalf("expected ErrAmbiguousWork, got %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(candidates), candidates)
	}

	after := snapshotDir(t, stateDir)
	if !mapsEqual(before, after) {
		t.Fatalf("Resolve on ambiguous match wrote state:\nbefore=%v\nafter=%v", before, after)
	}
}

// snapshotDir returns a map of relative path -> file contents for every
// file under dir, for a full byte-level before/after comparison (not an
// mtime comparison, which is too coarse at second granularity).
func snapshotDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snap[rel] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func mapsEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		other, ok := b[k]
		if !ok || !bytes.Equal(v, other) {
			return false
		}
	}
	return true
}

func TestListRecords_EmptyWhenStateDirMissing(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "does-not-exist")
	records, err := ListRecords(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no records, got %d", len(records))
	}
}

func TestNewID_ProducesValidJSON(t *testing.T) {
	id := NewID()
	if len(id) != 26 {
		t.Fatalf("expected a 26-char ULID, got %q (len %d)", id, len(id))
	}
	// Sanity-check it round-trips through JSON like any other string field.
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	var back string
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back != id {
		t.Fatalf("ULID did not round-trip through JSON: %q vs %q", back, id)
	}
}
