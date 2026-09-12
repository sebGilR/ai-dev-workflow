package work

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

// --- helpers -------------------------------------------------------------

// saveRecord creates and persists a record carrying exactly the given
// attachments, and returns it.
func saveRecord(t *testing.T, stateDir, title string, atts ...Attachment) *Record {
	t.Helper()
	r := New(title, ModeDelivery)
	r.Attachments = append(r.Attachments, atts...)
	if err := Save(stateDir, r); err != nil {
		t.Fatal(err)
	}
	return r
}

// liveWorktree returns a path that exists on disk for the duration of the
// test, standing in for a still-checked-out git worktree. Phase B's
// liveness check only stats the path, so a plain directory is a faithful
// stand-in and keeps these tests free of git process spawning.
func liveWorktree(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// deadWorktree returns a path guaranteed NOT to exist, standing in for a
// worktree that has been removed or relocated.
func deadWorktree(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "gone", name)
}

// writeRawRecord writes a work.json with arbitrary contents under workID,
// bypassing Save's validation, so a test can plant a record the current
// build refuses to load.
func writeRawRecord(t *testing.T, stateDir, workID string, body any) {
	t.Helper()
	if err := os.MkdirAll(Dir(stateDir, workID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteJSON(RecordPath(stateDir, workID), body); err != nil {
		t.Fatal(err)
	}
}

// --- resolver: Phase A repo_id guard (the erratum's core guarantee) -------

// TestResolve_PhaseA_RepoIDMismatchOnReusedPath_NoFalseMatch covers the
// erratum in docs/design/work-model.md §4: `git worktree remove <path> &&
// git worktree add <path>` from an unrelated repo reuses one filesystem
// path, and matching on worktree_path alone would silently resolve onto the
// stale record with no ambiguity ever surfaced. Deleting the repo_id
// equality check from matchByWorktree must fail this test.
func TestResolve_PhaseA_RepoIDMismatchOnReusedPath_NoFalseMatch(t *testing.T) {
	stateDir := t.TempDir()
	reusedPath := liveWorktree(t, "reused")

	saveRecord(t, stateDir, "stale work from the old repo", Attachment{
		RepoID:       "repo-OLD",
		WorktreePath: reusedPath,
		Branch:       "old-branch",
		Head:         "sha-old",
	})

	// Same path, different repo AND different branch: neither phase may match.
	got, candidates, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		RepoID:       "repo-NEW",
		Branch:       "new-branch",
		WorktreePath: reusedPath,
	})
	if !errors.Is(err, ErrNoActiveWork) {
		t.Fatalf("path reuse by an unrelated repo must not match: got record=%+v candidates=%d err=%v", got, len(candidates), err)
	}
}

// --- resolver: Phase B actually exists ------------------------------------

// TestResolve_PhaseB_MovedWorktreeFallbackMatches is the regression test for
// the moved-worktree fallback: Phase A misses (the recorded worktree_path is
// gone), Phase B matches on repo+branch. Deleting Phase B entirely must fail
// this test.
func TestResolve_PhaseB_MovedWorktreeFallbackMatches(t *testing.T) {
	stateDir := t.TempDir()
	oldPath := deadWorktree(t, "old-location")
	newPath := liveWorktree(t, "new-location")

	want := saveRecord(t, stateDir, "moved worktree", Attachment{
		RepoID:       "repoA",
		WorktreePath: oldPath,
		Branch:       "feature",
		Head:         "sha1",
	})

	got, _, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		RepoID:       "repoA",
		Branch:       "feature",
		WorktreePath: newPath,
	})
	if err != nil {
		t.Fatalf("phase B fallback should have matched the moved worktree: %v", err)
	}
	if got.WorkID != want.WorkID {
		t.Fatalf("resolved %s, want %s", got.WorkID, want.WorkID)
	}
}

// --- resolver: Phase A SUPPRESSES Phase B (ordered, not a union) ----------

// TestResolve_PhaseASuppressesPhaseB_NotAUnion pins the precedence itself,
// not merely "Phase A returned something". Two distinct records exist:
// phaseAOnly matches by exact worktree_path, phaseBOnly matches only by
// repo+branch (its own worktree is gone, so Phase B would happily take it).
// Under the correct two-phase rule Phase B never runs, so exactly one
// candidate exists and resolution succeeds. Replacing the two phases with a
// union yields two candidates and ErrAmbiguousWork — the explicitly
// forbidden "manufactured ambiguity" behaviour. The per-record `break` in
// the matching loop cannot make this pass by accident: the two matches come
// from two different records.
func TestResolve_PhaseASuppressesPhaseB_NotAUnion(t *testing.T) {
	stateDir := t.TempDir()
	here := liveWorktree(t, "here")

	phaseAOnly := saveRecord(t, stateDir, "matches by path", Attachment{
		RepoID:       "repoA",
		WorktreePath: here,
		Branch:       "shared-branch",
		Head:         "sha1",
	})
	phaseBOnly := saveRecord(t, stateDir, "matches only by repo+branch", Attachment{
		RepoID:       "repoA",
		WorktreePath: deadWorktree(t, "elsewhere"),
		Branch:       "shared-branch",
		Head:         "sha2",
	})

	got, candidates, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		RepoID:       "repoA",
		Branch:       "shared-branch",
		WorktreePath: here,
	})
	if err != nil {
		t.Fatalf("phase A match must resolve without consulting phase B, got err=%v candidates=%d", err, len(candidates))
	}
	if candidates != nil {
		t.Fatalf("a successful resolution must return no candidate list, got %d", len(candidates))
	}
	if got.WorkID != phaseAOnly.WorkID {
		t.Fatalf("resolved %s (the phase-B-only record is %s), want the phase-A match %s", got.WorkID, phaseBOnly.WorkID, phaseAOnly.WorkID)
	}
}

// --- resolver: Phase B is not path-blind ----------------------------------

// TestResolve_PhaseB_DoesNotStealFromLiveWorktree covers two checkouts of
// one repo that are both still on disk and both on `main`. A query from p2
// must not resolve onto the record attached to the still-live p1: Phase B is
// the MOVED-worktree fallback, and p1 has not moved.
func TestResolve_PhaseB_DoesNotStealFromLiveWorktree(t *testing.T) {
	stateDir := t.TempDir()
	p1 := liveWorktree(t, "wt1")
	p2 := liveWorktree(t, "wt2")

	saveRecord(t, stateDir, "work on the other live worktree", Attachment{
		RepoID:       "repoA",
		WorktreePath: p1,
		Branch:       "main",
		Head:         "sha1",
	})

	got, candidates, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		RepoID:       "repoA",
		Branch:       "main",
		WorktreePath: p2,
	})
	if !errors.Is(err, ErrNoActiveWork) {
		t.Fatalf("phase B stole a still-live worktree's record: got=%+v candidates=%d err=%v", got, len(candidates), err)
	}
}

// TestResolve_PhaseB_StillMatchesWhenWorktreeIsGone is the guard against
// over-correcting the previous test into "Phase B never matches": a record
// whose worktree genuinely no longer exists must still be found.
func TestResolve_PhaseB_StillMatchesWhenWorktreeIsGone(t *testing.T) {
	stateDir := t.TempDir()

	want := saveRecord(t, stateDir, "worktree removed", Attachment{
		RepoID:       "repoA",
		WorktreePath: deadWorktree(t, "removed"),
		Branch:       "main",
		Head:         "sha1",
	})

	got, _, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		RepoID:       "repoA",
		Branch:       "main",
		WorktreePath: liveWorktree(t, "fresh"),
	})
	if err != nil {
		t.Fatalf("phase B must still match an attachment whose worktree is gone: %v", err)
	}
	if got.WorkID != want.WorkID {
		t.Fatalf("resolved %s, want %s", got.WorkID, want.WorkID)
	}
}

// --- resolver: incomplete scans never resolve confidently -----------------

// TestResolve_SkippedRecordForcesUncertainty is the regression test for the
// "unreadable record collapses ambiguity into a confident wrong answer" bug.
// One loadable record matches; a second record exists but carries a schema
// version this build rejects (exactly what happens to every un-migrated
// record the moment a schema bump lands). The resolver must NOT report the
// loadable one as the single confident match — that answer is what
// `checkpoint --from-hook` turns into a permanent session binding.
func TestResolve_SkippedRecordForcesUncertainty(t *testing.T) {
	stateDir := t.TempDir()
	here := liveWorktree(t, "wt")
	att := Attachment{RepoID: "repoA", WorktreePath: here, Branch: "main", Head: "sha1"}

	saveRecord(t, stateDir, "the loadable one", att)

	futureID := "01TESTFUTURESCHEMA0000001"
	writeRawRecord(t, stateDir, futureID, map[string]any{
		"schema_version": CurrentSchemaVersion + 1,
		"work_id":        futureID,
		"lifecycle":      string(LifecycleActive),
		"attachments":    []Attachment{att},
	})

	got, _, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		RepoID:       "repoA",
		Branch:       "main",
		WorktreePath: here,
	})
	if err == nil {
		t.Fatalf("an incomplete scan must not produce a confident resolution, got %+v", got)
	}
	if !errors.Is(err, ErrIncompleteScan) {
		t.Fatalf("expected ErrIncompleteScan, got %v", err)
	}
	// Wrapped alongside ErrAmbiguousWork so existing callers take their
	// no-auto-bind, exit-non-zero branch without needing to be taught a new
	// error.
	if !errors.Is(err, ErrAmbiguousWork) {
		t.Fatalf("incomplete-scan error must also satisfy errors.Is(err, ErrAmbiguousWork), got %v", err)
	}
}

// TestResolve_SkippedRecordSuppressesNoActiveWork covers the quieter half of
// the same bug: with zero candidates and a skipped record, returning
// ErrNoActiveWork makes the hook exit 0 silently, which is the same
// wrong-but-confident collapse.
func TestResolve_SkippedRecordSuppressesNoActiveWork(t *testing.T) {
	stateDir := t.TempDir()

	badID := "01TESTCORRUPTSCAN00000001"
	if err := os.MkdirAll(Dir(stateDir, badID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RecordPath(stateDir, badID), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		RepoID:       "repoA",
		Branch:       "main",
		WorktreePath: liveWorktree(t, "wt"),
	})
	if !errors.Is(err, ErrIncompleteScan) {
		t.Fatalf("expected ErrIncompleteScan rather than a silent no-active-work, got %v", err)
	}
}

// TestResolve_CleanScanStillResolvesConfidently guards the previous two
// tests against over-correction: with nothing skipped, a single match still
// resolves.
func TestResolve_CleanScanStillResolvesConfidently(t *testing.T) {
	stateDir := t.TempDir()
	here := liveWorktree(t, "wt")
	want := saveRecord(t, stateDir, "only one", Attachment{
		RepoID: "repoA", WorktreePath: here, Branch: "main", Head: "sha1",
	})

	got, _, err := Resolve(ResolveOptions{
		StateDir: stateDir, RepoID: "repoA", Branch: "main", WorktreePath: here,
	})
	if err != nil || got.WorkID != want.WorkID {
		t.Fatalf("clean scan with one match must resolve: got=%+v err=%v", got, err)
	}
}

// --- resolver: a broken session binding falls through, never errors out ---

// TestResolve_BindingToPurgedRecord_FallsThroughToWorktree covers the sticky
// exit-1 bug: a session bound to a record that has since been purged made
// every subsequent hook fire die on a raw `open .../work.json: no such file
// or directory`. The binding must be treated as absent and step 3 must
// re-derive the answer.
func TestResolve_BindingToPurgedRecord_FallsThroughToWorktree(t *testing.T) {
	stateDir := t.TempDir()
	here := liveWorktree(t, "wt")

	purged := saveRecord(t, stateDir, "about to be purged", Attachment{
		RepoID: "repoA", WorktreePath: here, Branch: "main", Head: "sha1",
	})
	if err := SaveSessionBinding(stateDir, "session-x", purged.WorkID); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(Dir(stateDir, purged.WorkID)); err != nil {
		t.Fatal(err)
	}

	survivor := saveRecord(t, stateDir, "still here", Attachment{
		RepoID: "repoA", WorktreePath: here, Branch: "main", Head: "sha2",
	})

	got, _, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		SessionID:    "session-x",
		RepoID:       "repoA",
		Branch:       "main",
		WorktreePath: here,
	})
	if err != nil {
		t.Fatalf("a binding to a purged record must fall through to worktree association, got %v", err)
	}
	if got.WorkID != survivor.WorkID {
		t.Fatalf("resolved %s, want the surviving record %s", got.WorkID, survivor.WorkID)
	}
}

// TestResolve_BindingToNeverSavedRecord_FallsThroughToNoActiveWork checks
// the other end of the same fallthrough: with nothing else to associate to,
// the caller gets the documented ErrNoActiveWork contract, not a raw
// "open .../work.json: no such file or directory".
func TestResolve_BindingToNeverSavedRecord_FallsThroughToNoActiveWork(t *testing.T) {
	stateDir := t.TempDir()

	gone := New("gone", ModeDelivery)
	if err := SaveSessionBinding(stateDir, "session-y", gone.WorkID); err != nil {
		t.Fatal(err)
	}

	_, _, err := Resolve(ResolveOptions{
		StateDir:     stateDir,
		SessionID:    "session-y",
		RepoID:       "repoA",
		Branch:       "main",
		WorktreePath: liveWorktree(t, "wt"),
	})
	if !errors.Is(err, ErrNoActiveWork) {
		t.Fatalf("expected ErrNoActiveWork after falling through a dangling binding, got %v", err)
	}
}

// --- dedupeSorted -------------------------------------------------------

// TestDedupeSorted exercises dedupeSorted directly. The current matching
// path cannot feed it a duplicate (each phase `break`s after one match per
// record, and the phases are mutually exclusive), so this white-box test is
// what keeps the dedup half honest rather than silently-dead safety code.
// The sort half is load-bearing on every ambiguous resolution.
func TestDedupeSorted(t *testing.T) {
	a := &Record{WorkID: "AAA"}
	b := &Record{WorkID: "BBB"}
	aDup := &Record{WorkID: "AAA"}

	out := dedupeSorted([]*Record{b, a, aDup})
	if len(out) != 2 {
		t.Fatalf("expected 2 records after dedup, got %d", len(out))
	}
	if out[0].WorkID != "AAA" || out[1].WorkID != "BBB" {
		t.Fatalf("expected WorkID-sorted output, got %s, %s", out[0].WorkID, out[1].WorkID)
	}
	if out[0] != b && out[0] != a && out[0] != aDup {
		t.Fatalf("dedupeSorted returned a record it was not given")
	}
}

// --- UpdateRecord: the read-modify-write critical section -----------------

// TestUpdateRecord_ConcurrentAppendsLoseNothing is the regression test for
// the lost-update race. The old `Load` (unlocked) -> append -> `Save` (locks
// only the write) pattern lets concurrent appenders read the same pre-state
// and clobber each other, persisting one attachment where N were written.
// UpdateRecord holds one lock across the whole load-mutate-save cycle, so
// every attempt that reports success is durably present.
//
// state.AcquireLock is non-blocking by contract, so contenders are retried
// here rather than queued by the lock; the assertion is that successes and
// persisted attachments agree exactly, and that all N eventually land.
func TestUpdateRecord_ConcurrentAppendsLoseNothing(t *testing.T) {
	stateDir := t.TempDir()
	base := saveRecord(t, stateDir, "concurrent target")

	const n = 12
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			att := Attachment{
				RepoID:       "repoA",
				WorktreePath: fmt.Sprintf("/wt/%d", i),
				Branch:       fmt.Sprintf("branch-%d", i),
				Head:         fmt.Sprintf("sha-%d", i),
			}
			// Bounded retry: AcquireLock fails fast on contention.
			var lastErr error
			for attempt := 0; attempt < 200; attempt++ {
				_, err := UpdateRecord(stateDir, base.WorkID, func(r *Record) error {
					r.Attachments = append(r.Attachments, att)
					return nil
				})
				if err == nil {
					return
				}
				lastErr = err
				time.Sleep(time.Millisecond)
			}
			errs[i] = lastErr
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d never acquired the lock: %v", i, err)
		}
	}

	got, err := Load(stateDir, base.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Attachments) != n {
		t.Fatalf("lost update: %d attachments persisted, want %d", len(got.Attachments), n)
	}
	seen := map[string]bool{}
	for _, a := range got.Attachments {
		if seen[a.Head] {
			t.Fatalf("attachment %s persisted twice", a.Head)
		}
		seen[a.Head] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d distinct attachments, got %d", n, len(seen))
	}
}

// TestUpdateRecord_MutateErrorWritesNothing: a failed mutation must leave
// the on-disk record byte-identical, not half-applied and not merely
// updated_at-bumped.
func TestUpdateRecord_MutateErrorWritesNothing(t *testing.T) {
	stateDir := t.TempDir()
	r := saveRecord(t, stateDir, "untouched")
	path := RecordPath(stateDir, r.WorkID)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("nope")
	_, err = UpdateRecord(stateDir, r.WorkID, func(rec *Record) error {
		rec.Title = "should not persist"
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the mutate error to propagate, got %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("a failed mutation wrote to disk:\nbefore=%s\nafter=%s", before, after)
	}
}

// TestUpdateRecord_UnknownWorkIDFails: UpdateRecord is not a create path.
// `work attach --work <bogus>` must still fail loudly.
func TestUpdateRecord_UnknownWorkIDFails(t *testing.T) {
	stateDir := t.TempDir()
	called := false
	_, err := UpdateRecord(stateDir, "01TESTNOSUCHRECORD0000001", func(*Record) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected an error for an unknown work id")
	}
	if called {
		t.Fatal("mutate must not run when the record cannot be loaded")
	}
}

// TestUpdateRecord_SeesConcurrentlyPersistedState proves the load half
// happens inside the lock: a write that lands between an earlier Load and
// the UpdateRecord call is visible to mutate and is not clobbered.
func TestUpdateRecord_SeesConcurrentlyPersistedState(t *testing.T) {
	stateDir := t.TempDir()
	r := saveRecord(t, stateDir, "stale snapshot holder")

	stale, err := Load(stateDir, r.WorkID) // the caller's stale in-memory copy
	if err != nil {
		t.Fatal(err)
	}

	// Someone else appends an attachment in the meantime.
	if _, err := UpdateRecord(stateDir, r.WorkID, func(rec *Record) error {
		rec.Attachments = append(rec.Attachments, Attachment{RepoID: "repoA", Branch: "other"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(stale.Attachments) != 0 {
		t.Fatalf("test setup: stale copy should have no attachments, got %d", len(stale.Attachments))
	}

	updated, err := UpdateRecord(stateDir, r.WorkID, func(rec *Record) error {
		rec.Attachments = append(rec.Attachments, Attachment{RepoID: "repoA", Branch: "mine"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Attachments) != 2 {
		t.Fatalf("UpdateRecord read a stale snapshot: %d attachments, want 2", len(updated.Attachments))
	}
}

// --- provenance / JSON shape ---------------------------------------------

// TestLoad_UnsupportedProvenanceSchemaVersion: provenance.schema_version is
// the field the design doc names for rejecting a future schema, so it is
// validated too — otherwise it is documentation-only dead weight.
func TestLoad_UnsupportedProvenanceSchemaVersion(t *testing.T) {
	stateDir := t.TempDir()
	workID := "01TESTBADPROVVERSION00001"
	writeRawRecord(t, stateDir, workID, map[string]any{
		"schema_version": CurrentSchemaVersion,
		"work_id":        workID,
		"provenance":     map[string]any{"schema_version": 99},
	})

	if _, err := Load(stateDir, workID); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("expected ErrUnsupportedSchemaVersion for provenance.schema_version 99, got %v", err)
	}
}

// TestNew_SerializesEmptyCollectionsNotNull: the design doc's §2 example
// shows `"source_hashes": {}` and `[]`-shaped lists. Go nil slices/maps
// serialize as JSON null, which breaks consumers that iterate those keys.
func TestNew_SerializesEmptyCollectionsNotNull(t *testing.T) {
	data, err := json.Marshal(New("t", ModeDelivery))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["attachments"]) != "[]" {
		t.Fatalf("attachments serialized as %s, want []", raw["attachments"])
	}

	var prov map[string]json.RawMessage
	if err := json.Unmarshal(raw["provenance"], &prov); err != nil {
		t.Fatal(err)
	}
	if string(prov["source_hashes"]) != "{}" {
		t.Fatalf("provenance.source_hashes serialized as %s, want {}", prov["source_hashes"])
	}

	var ctx map[string]json.RawMessage
	if err := json.Unmarshal(raw["context"], &ctx); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"constraints", "decisions", "open_questions"} {
		if string(ctx[k]) != "[]" {
			t.Fatalf("context.%s serialized as %s, want []", k, ctx[k])
		}
	}
}

// --- id.go ---------------------------------------------------------------

// TestNewID_ConcurrentUniqueness formalizes the ad-hoc concurrency probe:
// many goroutines generating IDs simultaneously must never collide, and the
// generator must be race-clean under -race.
func TestNewID_ConcurrentUniqueness(t *testing.T) {
	const goroutines = 32
	const perGoroutine = 500

	var wg sync.WaitGroup
	out := make([][]string, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ids := make([]string, perGoroutine)
			for i := range ids {
				ids[i] = NewID()
			}
			out[g] = ids
		}(g)
	}
	wg.Wait()

	seen := make(map[string]bool, goroutines*perGoroutine)
	for _, ids := range out {
		for _, id := range ids {
			if len(id) != 26 {
				t.Fatalf("expected a 26-char ULID, got %q", id)
			}
			if seen[id] {
				t.Fatalf("duplicate ULID generated concurrently: %s", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != goroutines*perGoroutine {
		t.Fatalf("expected %d unique ids, got %d", goroutines*perGoroutine, len(seen))
	}
}

// --- session bindings ----------------------------------------------------

// TestSessionBindings_SameWorkIDTwoSessions covers the design's "bindings
// must not overwrite each other" contract for the case the existing test
// does not: ONE work item bound by two different sessions. Bindings are
// keyed by session_id and are additive, so both files must survive and both
// must resolve.
func TestSessionBindings_SameWorkIDTwoSessions(t *testing.T) {
	stateDir := t.TempDir()
	r := saveRecord(t, stateDir, "one work item, two sessions")

	if err := SaveSessionBinding(stateDir, "session-a", r.WorkID); err != nil {
		t.Fatal(err)
	}
	beforeA, err := os.ReadFile(SessionPath(stateDir, "session-a"))
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveSessionBinding(stateDir, "session-b", r.WorkID); err != nil {
		t.Fatal(err)
	}

	afterA, err := os.ReadFile(SessionPath(stateDir, "session-a"))
	if err != nil {
		t.Fatalf("session-a's binding disappeared when session-b bound the same work id: %v", err)
	}
	if !bytes.Equal(beforeA, afterA) {
		t.Fatalf("binding session-b overwrote session-a's binding file:\nbefore=%s\nafter=%s", beforeA, afterA)
	}

	for _, s := range []string{"session-a", "session-b"} {
		got, _, err := Resolve(ResolveOptions{StateDir: stateDir, SessionID: s})
		if err != nil {
			t.Fatalf("%s failed to resolve: %v", s, err)
		}
		if got.WorkID != r.WorkID {
			t.Fatalf("%s resolved to %s, want %s", s, got.WorkID, r.WorkID)
		}
	}
}

// TestResolve_EmptyRecordDirIsAbsentNotSkipped guards the failure mode where
// one stray record directory bricks every resolution in the state dir. A
// directory with no work.json is produced by an ordinary `work attach --work
// <typo>` (which locks and then fails to load) and by the window inside a
// concurrent `work start`. Treating it as an incomplete scan would make
// every later `work status` and every `checkpoint --from-hook` in that state
// dir fail until someone deleted it by hand.
func TestResolve_EmptyRecordDirIsAbsentNotSkipped(t *testing.T) {
	stateDir := t.TempDir()
	here := liveWorktree(t, "wt")

	want := saveRecord(t, stateDir, "the real record", Attachment{
		RepoID: "repoA", WorktreePath: here, Branch: "main", Head: "sha1",
	})

	// Exactly what a mistyped --work leaves behind: a directory (and a lock
	// file) with no work.json.
	if _, err := UpdateRecord(stateDir, "01TESTTYPOWORKID000000001", nil); err == nil {
		t.Fatal("expected UpdateRecord on an unknown id to fail")
	}

	records, skipped, err := ScanRecords(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("an empty record dir must not count as a skipped record, got %v", skipped)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	got, _, err := Resolve(ResolveOptions{
		StateDir: stateDir, RepoID: "repoA", Branch: "main", WorktreePath: here,
	})
	if err != nil {
		t.Fatalf("a stray empty record dir must not poison resolution: %v", err)
	}
	if got.WorkID != want.WorkID {
		t.Fatalf("resolved %s, want %s", got.WorkID, want.WorkID)
	}
}
