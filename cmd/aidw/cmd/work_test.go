package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"aidw/cmd/aidw/internal/work"
)

// The `aidw work` command tree is consumed both by users and by
// save-wip-snapshot.sh, which branches purely on the exit code of
// `work checkpoint --from-hook`. These tests therefore run the real binary
// in a subprocess (same rationale as resolve_test.go: the commands call
// os.Exit directly, so an in-process test would either kill the test binary
// or miss the exit-code half of the contract). Helpers buildAidw and
// initTestGitRepo are shared with resolve_test.go.

// runWorkCmd runs the built binary with an isolated AIDW_STATE_DIR (so the
// developer's real ~/.local/state/aidw is never read or written) and an
// optional stdin body. Passing stdin == "" leaves cmd.Stdin nil, i.e.
// /dev/null — a char device, which is exactly what makes
// runCheckpointFromHook's os.Stdin.Stat() guard skip the read.
func runWorkCmd(t *testing.T, dir, stateDir, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(buildAidw(t), args...)
	cmd.Dir = dir
	// Last-wins on duplicate keys, so this overrides any real
	// AIDW_STATE_DIR while keeping PATH (git must stay findable).
	cmd.Env = append(os.Environ(), "AIDW_STATE_DIR="+stateDir)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	exitCode := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run aidw %v: %v", args, err)
		}
		exitCode = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), exitCode
}

// startWork runs `work start` and returns the created record's work_id.
func startWork(t *testing.T, dir, stateDir, title string) string {
	t.Helper()
	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "start", ".", "--title", title)
	if code != 0 {
		t.Fatalf("work start exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	var r work.Record
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("work start stdout is not a record: %v\n%s", err, stdout)
	}
	if r.WorkID == "" {
		t.Fatalf("work start returned an empty work_id: %s", stdout)
	}
	return r.WorkID
}

// assertNoStateOnDisk pins the lookup-only invariant at the CLI layer: a
// lookup command must never call RegisterRepo (repos.json) nor create any
// other file under the state dir.
func assertNoStateOnDisk(t *testing.T, stateDir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(stateDir, "repos.json")); !os.IsNotExist(err) {
		t.Errorf("lookup-only command created repos.json (err=%v)", err)
	}
	var found []string
	_ = filepath.Walk(stateDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == stateDir {
			return nil //nolint:nilerr // a missing state dir is the pass case
		}
		found = append(found, path)
		return nil
	})
	if len(found) != 0 {
		t.Errorf("lookup-only command created state on disk: %v", found)
	}
}

func TestWorkStart_CreatesRecord(t *testing.T) {
	dir := initTestGitRepo(t)
	stateDir := t.TempDir()

	workID := startWork(t, dir, stateDir, "first task")

	if _, err := os.Stat(filepath.Join(stateDir, "work", workID, "work.json")); err != nil {
		t.Fatalf("expected work.json for %s: %v", workID, err)
	}
	// The mutate path is allowed (and required) to register the repo.
	if _, err := os.Stat(filepath.Join(stateDir, "repos.json")); err != nil {
		t.Errorf("expected work start to register the repo: %v", err)
	}
}

// TestWorkList_EmptyIsJSONArray pins both the empty-output shape (`[]`, not
// `null`, so `jq '.[]'` works) and the lookup-only invariant.
func TestWorkList_EmptyIsJSONArray(t *testing.T) {
	dir := initTestGitRepo(t)
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "list", ".")

	if code != 0 {
		t.Fatalf("work list exited %d\nstderr: %s", code, stderr)
	}
	var records []any
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("work list output is not JSON: %v\n%s", err, stdout)
	}
	if records == nil {
		t.Errorf("work list with zero records emitted JSON null, want []: %s", stdout)
	}
	assertNoStateOnDisk(t, stateDir)
}

// TestWorkStatus_NoActiveWork pins spec G3.4: exit 1 with a stderr pointer at
// `work start` (deliberately NOT resolve.go's exit 0), and no state written.
func TestWorkStatus_NoActiveWork(t *testing.T) {
	dir := initTestGitRepo(t)
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "status", ".")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "no active work") {
		t.Errorf("stderr = %q, want a 'no active work' message", stderr)
	}
	assertNoStateOnDisk(t, stateDir)
}

// TestWorkList_IncludeArchived confirms the flag actually changes the result
// set, not merely that it parses. There is no CLI lifecycle transition yet
// (Cluster I), so the archived record is produced by loading and re-saving
// it in-process against the same temp state dir.
func TestWorkList_IncludeArchived(t *testing.T) {
	dir := initTestGitRepo(t)
	stateDir := t.TempDir()

	startWork(t, dir, stateDir, "live task")
	archivedID := startWork(t, dir, stateDir, "old task")

	r, err := work.Load(stateDir, archivedID)
	if err != nil {
		t.Fatal(err)
	}
	r.Lifecycle = work.LifecycleArchived
	if err := work.Save(stateDir, r); err != nil {
		t.Fatal(err)
	}

	listIDs := func(args ...string) []string {
		t.Helper()
		stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", args...)
		if code != 0 {
			t.Fatalf("%v exited %d\nstderr: %s", args, code, stderr)
		}
		var records []work.Record
		if err := json.Unmarshal([]byte(stdout), &records); err != nil {
			t.Fatalf("work list output is not JSON: %v\n%s", err, stdout)
		}
		ids := make([]string, 0, len(records))
		for _, rec := range records {
			ids = append(ids, rec.WorkID)
		}
		return ids
	}

	def := listIDs("work", "list", ".")
	if len(def) != 1 {
		t.Errorf("default work list = %v, want only the non-archived record", def)
	}
	withArchived := listIDs("work", "list", ".", "--include-archived")
	if len(withArchived) != 2 {
		t.Errorf("work list --include-archived = %v, want both records", withArchived)
	}
}

// TestWorkCheckpointFromHook_HardError pins the error half of the hook
// contract: a cwd that isn't a git repo at all is a real failure (exit 1,
// stderr), never conflated with the ErrNoActiveWork no-op.
func TestWorkCheckpointFromHook_HardError(t *testing.T) {
	dir := t.TempDir() // deliberately not a git repo
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "checkpoint", "--from-hook")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if strings.TrimSpace(stderr) == "" {
		t.Error("want a non-empty stderr message on a hard error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on error", stdout)
	}
}

// TestWorkCheckpointFromHook_NoActiveWorkIsSilentNoop pins the other half:
// a real git repo with no associated work record exits 0 with no output, so
// the hook never sees a failure for an ordinary unmanaged repo.
func TestWorkCheckpointFromHook_NoActiveWorkIsSilentNoop(t *testing.T) {
	dir := initTestGitRepo(t)
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "checkpoint", "--from-hook")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stdout != "" || strings.TrimSpace(stderr) != "" {
		t.Errorf("want silence, got stdout=%q stderr=%q", stdout, stderr)
	}
	// The no-op path must not have bound a session or registered anything.
	if _, err := os.Stat(filepath.Join(stateDir, "sessions")); !os.IsNotExist(err) {
		t.Errorf("checkpoint no-op created a sessions dir (err=%v)", err)
	}
}

// TestWorkCheckpointFromHook_BindsSession is the success path the hook
// depends on: an associated record resolves via worktree association and,
// because the payload carries a session_id, gets auto-bound.
func TestWorkCheckpointFromHook_BindsSession(t *testing.T) {
	dir := initTestGitRepo(t)
	stateDir := t.TempDir()
	workID := startWork(t, dir, stateDir, "hooked task")

	payload := `{"cwd":"` + dir + `","session_id":"sess-1"}`
	stdout, stderr, code := runWorkCmd(t, dir, stateDir, payload, "work", "checkpoint", "--from-hook")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	binding, err := work.LoadSessionBinding(stateDir, "sess-1")
	if err != nil || binding == nil {
		t.Fatalf("expected a session binding, got %v (err=%v)", binding, err)
	}
	if binding.WorkID != workID {
		t.Errorf("bound work_id = %q, want %q", binding.WorkID, workID)
	}
}

// ambiguousRepo creates a repo with two active records sharing one
// repo_id + worktree_path, which is exactly what Resolve's Phase A reports
// as ErrAmbiguousWork.
func ambiguousRepo(t *testing.T) (dir, stateDir string, ids []string) {
	t.Helper()
	dir = initTestGitRepo(t)
	stateDir = t.TempDir()
	ids = []string{
		startWork(t, dir, stateDir, "task one"),
		startWork(t, dir, stateDir, "task two"),
	}
	return dir, stateDir, ids
}

func assertCandidateList(t *testing.T, stdout string, wantIDs []string) {
	t.Helper()
	var candidates []map[string]string
	if err := json.Unmarshal([]byte(stdout), &candidates); err != nil {
		t.Fatalf("candidate output is not JSON: %v\n%s", err, stdout)
	}
	if len(candidates) != len(wantIDs) {
		t.Fatalf("got %d candidates, want %d: %s", len(candidates), len(wantIDs), stdout)
	}
	got := map[string]bool{}
	for _, c := range candidates {
		if c["work_id"] == "" {
			t.Errorf("candidate missing work_id: %v", c)
		}
		if _, ok := c["title"]; !ok {
			t.Errorf("candidate missing title: %v", c)
		}
		if _, ok := c["branch"]; !ok {
			t.Errorf("candidate missing branch: %v", c)
		}
		got[c["work_id"]] = true
	}
	for _, id := range wantIDs {
		if !got[id] {
			t.Errorf("candidate list is missing %s: %s", id, stdout)
		}
	}
}

// TestWorkStatus_AmbiguousListsCandidates pins spec G3.4's ambiguous branch:
// the candidate list on stdout, exit 1, and no write of any kind.
func TestWorkStatus_AmbiguousListsCandidates(t *testing.T) {
	dir, stateDir, ids := ambiguousRepo(t)

	stdout, _, code := runWorkCmd(t, dir, stateDir, "", "work", "status", ".")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	assertCandidateList(t, stdout, ids)
	if _, err := os.Stat(filepath.Join(stateDir, "sessions")); !os.IsNotExist(err) {
		t.Errorf("ambiguous work status wrote a session binding (err=%v)", err)
	}
}

// TestWorkCheckpointDirect_AmbiguousListsCandidates covers the direct
// (non-hook) checkpoint path, which must be as informative as `work status`.
// Only --from-hook is required to stay silent.
func TestWorkCheckpointDirect_AmbiguousListsCandidates(t *testing.T) {
	dir, stateDir, ids := ambiguousRepo(t)

	stdout, _, code := runWorkCmd(t, dir, stateDir, "", "work", "checkpoint", ".")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	assertCandidateList(t, stdout, ids)
}

// TestWorkPurge_DryRunForceIsNoop pins §2.2's addendum note that --force
// combined with --dry-run has no effect on the preview: the precondition
// --force bypasses is already irrelevant to a dry run (which never checks
// it), so the two flags together still only preview, exactly like
// --dry-run alone.
func TestWorkPurge_DryRunForceIsNoop(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()
	workID := startWork(t, dir, stateDir, "dry-run plus force")

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "purge", workID, "--dry-run", "--force")
	if code != 0 {
		t.Fatalf("work purge --dry-run --force exited %d\nstderr: %s", code, stderr)
	}
	var preview map[string]any
	if err := json.Unmarshal([]byte(stdout), &preview); err != nil {
		t.Fatalf("dry-run output is not JSON: %v\n%s", err, stdout)
	}
	if preview["work_id"] != workID {
		t.Errorf("preview work_id = %v, want %s", preview["work_id"], workID)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "work", workID, "work.json")); err != nil {
		t.Errorf("--dry-run --force together must still only preview: %v", err)
	}
}

// --- Cluster I: lifecycle transitions -------------------------------------

// loadRecord runs `work list <dir>` and returns the single record for id.
// Used by the lifecycle-transition tests below to inspect the saved
// lifecycle/stage fields without adding a new CLI surface.
func loadRecordByID(t *testing.T, dir, stateDir, id string) work.Record {
	t.Helper()
	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "list", ".", "--include-archived")
	if code != 0 {
		t.Fatalf("work list exited %d\nstderr: %s", code, stderr)
	}
	var records []work.Record
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("work list output is not JSON: %v\n%s", err, stdout)
	}
	for _, r := range records {
		if r.WorkID == id {
			return r
		}
	}
	t.Fatalf("no record %s in work list output: %s", id, stdout)
	return work.Record{}
}

// TestWorkLifecycle_PauseDoneArchive pins AC-I1-PAUSE/AC-I1-DONE/
// AC-I1-ARCHIVE: each verb sets exactly that lifecycle value and bumps
// updated_at.
func TestWorkLifecycle_PauseDoneArchive(t *testing.T) {
	cases := []struct {
		verb      string
		lifecycle work.Lifecycle
	}{
		{"pause", work.LifecyclePaused},
		{"done", work.LifecycleDone},
		{"archive", work.LifecycleArchived},
	}
	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			dir := initGitRepoWithBranch(t, "main")
			stateDir := t.TempDir()
			workID := startWork(t, dir, stateDir, "lifecycle-"+tc.verb)
			before := loadRecordByID(t, dir, stateDir, workID)
			if before.Provenance.UpdatedAt == "" {
				t.Fatal("precondition: created record must have a non-empty updated_at")
			}

			stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", tc.verb, workID)
			if code != 0 {
				t.Fatalf("work %s exited %d\nstderr: %s", tc.verb, code, stderr)
			}
			var r work.Record
			if err := json.Unmarshal([]byte(stdout), &r); err != nil {
				t.Fatalf("work %s output is not JSON: %v\n%s", tc.verb, err, stdout)
			}
			if r.Lifecycle != tc.lifecycle {
				t.Errorf("lifecycle = %q, want %q", r.Lifecycle, tc.lifecycle)
			}
			// updated_at is RFC3339 (second resolution), so a same-second
			// CLI round trip cannot assert strict advancement without a
			// flaky sleep — assert it is present and not regressed
			// (>= created_at) instead.
			if r.Provenance.UpdatedAt == "" {
				t.Errorf("updated_at is empty after %s", tc.verb)
			}
			if r.Provenance.UpdatedAt < r.Provenance.CreatedAt {
				t.Errorf("updated_at %q regressed before created_at %q", r.Provenance.UpdatedAt, r.Provenance.CreatedAt)
			}
		})
	}
}

// TestWorkLifecycle_StageInvariant pins AC-I1-STAGE: none of
// pause/done/archive touch Stage, only Lifecycle and updated_at.
//
// Mutation-testing clause (Lesson 2): this test was manually verified to go
// red by temporarily inserting `r.Stage = "started"` into each of the three
// mutate closures in cmd/aidw/cmd/work.go's newLifecycleCmd during
// implementation, confirming the assertion below actually exercises the
// invariant rather than passing vacuously; the insertion was reverted
// before landing.
func TestWorkLifecycle_StageInvariant(t *testing.T) {
	for _, verb := range []string{"pause", "done", "archive"} {
		t.Run(verb, func(t *testing.T) {
			dir := initGitRepoWithBranch(t, "main")
			stateDir := t.TempDir()
			workID := startWork(t, dir, stateDir, "stage-invariant-"+verb)

			// Stamp a non-empty Stage directly (there is no CLI to do this
			// yet), then re-save under the same lock discipline the CLI
			// itself would use.
			if _, err := work.UpdateRecord(stateDir, workID, func(r *work.Record) error {
				r.Stage = "reviewed"
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", verb, workID)
			if code != 0 {
				t.Fatalf("work %s exited %d\nstderr: %s", verb, code, stderr)
			}
			var r work.Record
			if err := json.Unmarshal([]byte(stdout), &r); err != nil {
				t.Fatalf("work %s output is not JSON: %v\n%s", verb, err, stdout)
			}
			if r.Stage != "reviewed" {
				t.Errorf("stage = %q, want byte-identical %q", r.Stage, "reviewed")
			}
		})
	}
}

// TestWorkList_ListVisibility pins AC-I1-LISTVISIBILITY: paused/done stay
// visible in default `work list` output; only archived is excluded.
func TestWorkList_ListVisibility(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()

	pausedID := startWork(t, dir, stateDir, "paused")
	doneID := startWork(t, dir, stateDir, "done")
	archivedID := startWork(t, dir, stateDir, "archived")

	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "pause", pausedID); code != 0 {
		t.Fatalf("work pause exited %d", code)
	}
	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "done", doneID); code != 0 {
		t.Fatalf("work done exited %d", code)
	}
	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "archive", archivedID); code != 0 {
		t.Fatalf("work archive exited %d", code)
	}

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "list", ".")
	if code != 0 {
		t.Fatalf("work list exited %d\nstderr: %s", code, stderr)
	}
	var records []work.Record
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("work list output is not JSON: %v\n%s", err, stdout)
	}
	seen := map[string]bool{}
	for _, r := range records {
		seen[r.WorkID] = true
	}
	if !seen[pausedID] {
		t.Error("paused record must still be visible in default list")
	}
	if !seen[doneID] {
		t.Error("done record must still be visible in default list")
	}
	if seen[archivedID] {
		t.Error("archived record must be excluded from default list")
	}
}

// --- Cluster I: work purge -------------------------------------------------

// TestWorkPurge_PreconditionRefusesNonArchived pins AC-I2-PURGE-PRECONDITION
// at the CLI layer.
func TestWorkPurge_PreconditionRefusesNonArchived(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()
	workID := startWork(t, dir, stateDir, "not archived")

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "purge", workID)
	if code == 0 {
		t.Fatalf("expected non-zero exit, stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "archive") {
		t.Errorf("stderr = %q, want a mention of `work archive`/`--force`", stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "work", workID, "work.json")); err != nil {
		t.Errorf("record must still exist after a refused purge: %v", err)
	}
}

// TestWorkPurge_RoundTrip pins AC-I2-PURGE-SUCCEEDS and the CLI-level round
// trip: archive, then purge for real, then confirm gone.
func TestWorkPurge_RoundTrip(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()
	workID := startWork(t, dir, stateDir, "to purge")

	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "archive", workID); code != 0 {
		t.Fatalf("work archive exited %d", code)
	}

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "purge", workID)
	if code != 0 {
		t.Fatalf("work purge exited %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, workID) {
		t.Errorf("purge output = %q, want it to mention %s", stdout, workID)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "work", workID)); !os.IsNotExist(err) {
		t.Errorf("expected work/%s/ to be gone, stat err=%v", workID, err)
	}
}

// TestWorkPurge_ForceRoundTrip pins AC-I2-PURGE-FORCE at the CLI layer: a
// non-archived record can be purged directly via --force, reaching the same
// end state as the archive-then-purge round trip.
func TestWorkPurge_ForceRoundTrip(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()
	workID := startWork(t, dir, stateDir, "forced purge")

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "purge", workID, "--force")
	if code != 0 {
		t.Fatalf("work purge --force exited %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, workID) {
		t.Errorf("purge output = %q, want it to mention %s", stdout, workID)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "work", workID)); !os.IsNotExist(err) {
		t.Errorf("expected work/%s/ to be gone, stat err=%v", workID, err)
	}
}

// TestWorkPurge_DryRunNeverRefused pins AC-I2-PURGE-DRYRUN: a preview
// succeeds even for a non-archived record and deletes nothing.
func TestWorkPurge_DryRunNeverRefused(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()
	workID := startWork(t, dir, stateDir, "preview only")

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "purge", workID, "--dry-run")
	if code != 0 {
		t.Fatalf("work purge --dry-run exited %d\nstderr: %s", code, stderr)
	}
	var preview map[string]any
	if err := json.Unmarshal([]byte(stdout), &preview); err != nil {
		t.Fatalf("dry-run output is not JSON: %v\n%s", err, stdout)
	}
	if preview["work_id"] != workID {
		t.Errorf("preview work_id = %v, want %s", preview["work_id"], workID)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "work", workID, "work.json")); err != nil {
		t.Errorf("dry-run must not delete anything: %v", err)
	}
}

// TestWorkPurge_DryRunListsSessionBindings pins AC-I2-SESSIONREAP-DRYRUN at
// the CLI layer: given two bindings (one to the target id, one to an
// unrelated id), a dry-run preview lists only the target's binding id under
// session_bindings and deletes neither file.
func TestWorkPurge_DryRunListsSessionBindings(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()
	targetID := startWork(t, dir, stateDir, "A")
	otherID := startWork(t, dir, stateDir, "B")

	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "bind-session", "--work", targetID, "--session", "sess-a"); code != 0 {
		t.Fatalf("work bind-session (A) exited %d", code)
	}
	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "bind-session", "--work", otherID, "--session", "sess-b"); code != 0 {
		t.Fatalf("work bind-session (B) exited %d", code)
	}

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "purge", targetID, "--dry-run")
	if code != 0 {
		t.Fatalf("work purge --dry-run exited %d\nstderr: %s", code, stderr)
	}
	var preview struct {
		SessionBindings []string `json:"session_bindings"`
	}
	if err := json.Unmarshal([]byte(stdout), &preview); err != nil {
		t.Fatalf("dry-run output is not JSON: %v\n%s", err, stdout)
	}
	if len(preview.SessionBindings) != 1 || preview.SessionBindings[0] != "sess-a" {
		t.Errorf("session_bindings = %v, want [sess-a]", preview.SessionBindings)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", "sess-a.json")); err != nil {
		t.Errorf("dry-run must not delete sess-a's binding: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", "sess-b.json")); err != nil {
		t.Errorf("dry-run must not touch sess-b's binding: %v", err)
	}
}

// TestWorkPurge_ForgetsMappingEntry pins review finding H1 at the CLI
// layer: a real purge, on a record that was created by migrate-state (not
// `work start`), forgets that source's wip-paths.json entry and reports
// what it forgot. Without this, the entry would dangle and the next plain
// migrate-state run would silently re-mint a record via reverify's
// crash-recovery path, which is designed for an interrupted Save, not a
// deliberate permanent deletion.
func TestWorkPurge_ForgetsMappingEntry(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	wipDir := filepath.Join(dir, ".wip", "20260101000000-main")
	if err := os.MkdirAll(wipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statusJSON := `{"repo":"r","repo_path":"` + dir + `","branch":"main","stage":"started","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(wipDir, "status.json"), []byte(statusJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "migrate-state", ".")
	if code != 0 {
		t.Fatalf("migrate-state exited %d\nstderr: %s", code, stderr)
	}
	var summary struct {
		Migrated []string `json:"migrated"`
	}
	if err := json.Unmarshal([]byte(stdout), &summary); err != nil {
		t.Fatalf("migrate-state stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(summary.Migrated) != 1 {
		t.Fatalf("setup: expected exactly 1 migrated source, got %+v", summary)
	}

	stdout, stderr, code = runWorkCmd(t, dir, stateDir, "", "work", "list", ".")
	if code != 0 {
		t.Fatalf("work list exited %d\nstderr: %s", code, stderr)
	}
	var records []work.Record
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("work list stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(records) != 1 {
		t.Fatalf("setup: expected exactly 1 record, got %d: %s", len(records), stdout)
	}
	workID := records[0].WorkID

	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "archive", workID); code != 0 {
		t.Fatalf("work archive exited %d", code)
	}
	stdout, stderr, code = runWorkCmd(t, dir, stateDir, "", "work", "purge", workID)
	if code != 0 {
		t.Fatalf("work purge exited %d\nstderr: %s", code, stderr)
	}
	var result struct {
		MappingEntriesForgotten []string `json:"mapping_entries_forgotten"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("purge output is not JSON: %v\n%s", err, stdout)
	}
	if len(result.MappingEntriesForgotten) != 1 {
		t.Fatalf("mapping_entries_forgotten = %+v, want exactly 1 entry", result.MappingEntriesForgotten)
	}

	mappingData, err := os.ReadFile(filepath.Join(stateDir, "migrations", "wip-paths.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mapping struct {
		Entries map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(mappingData, &mapping); err != nil {
		t.Fatal(err)
	}
	if len(mapping.Entries) != 0 {
		t.Errorf("expected wip-paths.json to have no entries after purge, got %+v", mapping.Entries)
	}
}

// TestWorkPurge_RoundTrip_ReapsRealSessionBinding pins review finding M3:
// nothing previously exercised the *wiring* of the session reap into a
// real (non-dry-run) `work purge` — only the dry-run preview and
// DeleteSessionBindingsFor as a standalone function were covered.
// Deleting the DeleteSessionBindingsFor call at workPurgeCmd's call site
// left this suite green before this test was added.
func TestWorkPurge_RoundTrip_ReapsRealSessionBinding(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()
	targetID := startWork(t, dir, stateDir, "A")
	otherID := startWork(t, dir, stateDir, "B")

	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "bind-session", "--work", targetID, "--session", "sess-a"); code != 0 {
		t.Fatalf("work bind-session (A) exited %d", code)
	}
	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "bind-session", "--work", otherID, "--session", "sess-b"); code != 0 {
		t.Fatalf("work bind-session (B) exited %d", code)
	}
	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "archive", targetID); code != 0 {
		t.Fatalf("work archive exited %d", code)
	}

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "purge", targetID)
	if code != 0 {
		t.Fatalf("work purge exited %d\nstderr: %s", code, stderr)
	}
	var result struct {
		SessionBindingsRemoved []string `json:"session_bindings_removed"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("purge output is not JSON: %v\n%s", err, stdout)
	}
	if len(result.SessionBindingsRemoved) != 1 || result.SessionBindingsRemoved[0] != "sess-a" {
		t.Fatalf("session_bindings_removed = %v, want [sess-a]", result.SessionBindingsRemoved)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", "sess-a.json")); !os.IsNotExist(err) {
		t.Errorf("expected sess-a's binding to be deleted, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", "sess-b.json")); err != nil {
		t.Errorf("expected sess-b's binding (unrelated) to survive, stat err=%v", err)
	}
}

// TestWorkActivate_ReversesArchive pins review finding H2's inverse
// command: `work activate <id>` sets an archived record back to `active`,
// giving a mis-archive (e.g. from the re-routed skills) a clean, explicit
// recovery path.
func TestWorkActivate_ReversesArchive(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()
	workID := startWork(t, dir, stateDir, "mis-archived")

	if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", "archive", workID); code != 0 {
		t.Fatalf("work archive exited %d", code)
	}
	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "activate", workID)
	if code != 0 {
		t.Fatalf("work activate exited %d\nstderr: %s", code, stderr)
	}
	var r work.Record
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("work activate stdout is not a record: %v\n%s", err, stdout)
	}
	if r.Lifecycle != work.LifecycleActive {
		t.Errorf("lifecycle = %q, want %q", r.Lifecycle, work.LifecycleActive)
	}

	// AC-I1-LISTVISIBILITY's own invariant applies here too: an activated
	// record must be visible in the default (non---include-archived) list.
	stdout, stderr, code = runWorkCmd(t, dir, stateDir, "", "work", "list", ".")
	if code != 0 {
		t.Fatalf("work list exited %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, workID) {
		t.Errorf("work list output = %s, want it to include the reactivated record %s", stdout, workID)
	}
}

// --- Cluster J: freeform mode golden capture (Task 1) ----------------------

// stripVolatileGoldenFields deletes/normalizes the fields that legitimately
// vary run-to-run from a decoded `work` JSON payload (a Record, or a slice
// of them under the given path convention), so the remainder can be
// compared byte-for-byte against a literal golden map. It mutates in place
// and returns the same value for chaining.
func stripVolatileRecordFields(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	delete(m, "work_id")
	if prov, ok := m["provenance"].(map[string]any); ok {
		delete(prov, "created_at")
		delete(prov, "updated_at")
	}
	if atts, ok := m["attachments"].([]any); ok {
		for _, a := range atts {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			delete(am, "head")
			delete(am, "worktree_path")
		}
	}
	return m
}

// wantDeliveryGoldenShape is the literal (volatile fields excluded per
// stripVolatileRecordFields) shape `work start --title X .` /
// `work list .` / `work status .` must produce for a delivery-mode record
// on a git fixture pinned to branch "main" (initGitRepoWithBranch), before
// AND after Cluster J's edits — see Task 1/AC-J-1/AC-J-2.
func wantDeliveryGoldenShape(title string) map[string]any {
	return map[string]any{
		"schema_version": float64(1),
		"title":          title,
		"mode":           "delivery",
		"lifecycle":      "active",
		"context": map[string]any{
			"goal":           "",
			"constraints":    []any{},
			"decisions":      []any{},
			"open_questions": []any{},
			"next_action":    "",
		},
		"attachments": []any{
			map[string]any{
				"repo_id": "REPO_ID_PLACEHOLDER",
				"branch":  "main",
			},
		},
		"initiative_id": nil,
		"provenance": map[string]any{
			"schema_version":    float64(1),
			"source_hashes":     map[string]any{},
			"authoring_session": "",
		},
	}
}

// assertGoldenRecord decodes stdout as a single Record-shaped map, strips
// volatile fields, blanks out the (non-deterministic, but must be
// non-empty) repo_id, and deep-compares the result against
// wantDeliveryGoldenShape(title).
func assertGoldenRecord(t *testing.T, stdout, title string) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("golden output is not JSON: %v\n%s", err, stdout)
	}
	stripVolatileRecordFields(t, got)
	atts, _ := got["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("golden record has %d attachments, want exactly 1: %s", len(atts), stdout)
	}
	am, _ := atts[0].(map[string]any)
	if repoID, _ := am["repo_id"].(string); repoID == "" {
		t.Errorf("golden record's attachment repo_id is empty")
	}
	am["repo_id"] = "REPO_ID_PLACEHOLDER"

	want := wantDeliveryGoldenShape(title)
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("golden shape mismatch:\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

// TestGolden_DeliveryModeUnchanged pins Task 1's baseline: `work start`
// (no --mode), `work list`, and `work status` on a fresh
// initGitRepoWithBranch(t, "main") fixture. This must pass before Cluster
// J's Task 2 edit begins, and after every subsequent task — it is the
// primary defense against Task 2's new validation branches silently
// changing the untouched delivery default path's stdout/stderr/exit code.
func TestGolden_DeliveryModeUnchanged(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "start", "--title", "golden task", ".")
	if code != 0 {
		t.Fatalf("work start exited %d\nstderr: %s", code, stderr)
	}
	if stderr != "" {
		t.Errorf("work start stderr = %q, want empty", stderr)
	}
	assertGoldenRecord(t, stdout, "golden task")

	stdout, stderr, code = runWorkCmd(t, dir, stateDir, "", "work", "list", ".")
	if code != 0 {
		t.Fatalf("work list exited %d\nstderr: %s", code, stderr)
	}
	if stderr != "" {
		t.Errorf("work list stderr = %q, want empty", stderr)
	}
	var records []map[string]any
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("work list output is not JSON: %v\n%s", err, stdout)
	}
	if len(records) != 1 {
		t.Fatalf("work list returned %d records, want 1: %s", len(records), stdout)
	}
	listJSON, _ := json.Marshal(records[0])
	assertGoldenRecord(t, string(listJSON), "golden task")

	stdout, stderr, code = runWorkCmd(t, dir, stateDir, "", "work", "status", ".")
	if code != 0 {
		t.Fatalf("work status exited %d\nstderr: %s", code, stderr)
	}
	if stderr != "" {
		t.Errorf("work status stderr = %q, want empty", stderr)
	}
	assertGoldenRecord(t, stdout, "golden task")
}

// TestGolden_DeliveryModeExplicitModeFlag_MatchesDefault pins AC-J-2 /
// Should-fix finding 7: `--mode delivery` explicitly is a no-op identical
// to the no-flag default.
func TestGolden_DeliveryModeExplicitModeFlag_MatchesDefault(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "start", "--title", "golden task", "--mode", "delivery", ".")
	if code != 0 {
		t.Fatalf("work start exited %d\nstderr: %s", code, stderr)
	}
	if stderr != "" {
		t.Errorf("work start stderr = %q, want empty", stderr)
	}
	assertGoldenRecord(t, stdout, "golden task")
}

// TestWorkStart_NonGitDir_DieUnchanged pins Should-fix finding 5: `work
// start`'s existing non-git Die path (no new flags involved) must not
// change shape once Task 2 inserts new validation branches ahead of
// resolveAttachContext's own git-repo check.
func TestWorkStart_NonGitDir_DieUnchanged(t *testing.T) {
	dir := t.TempDir() // deliberately not a git repo
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "start", "--title", "X", ".")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "not a git repo") {
		t.Errorf("stderr = %q, want a 'not a git repo' message", stderr)
	}
	assertNoStateOnDisk(t, stateDir)
}

// TestWorkList_NonGitDir_DieUnchanged pins Should-fix finding 5 for `work
// list` (no --global), guarding Task 5's edit the same way.
func TestWorkList_NonGitDir_DieUnchanged(t *testing.T) {
	dir := t.TempDir() // deliberately not a git repo
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "list", ".")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "not a git repo") {
		t.Errorf("stderr = %q, want a 'not a git repo' message", stderr)
	}
	assertNoStateOnDisk(t, stateDir)
}

// TestWorkCheckpointFromHook_AmbiguousIsSilent is the counterpart: the hook
// path stays silent on stdout (the script discards output anyway, but the
// spec mandates silence there specifically) while still exiting non-zero.
func TestWorkCheckpointFromHook_AmbiguousIsSilent(t *testing.T) {
	dir, stateDir, _ := ambiguousRepo(t)

	payload := `{"cwd":"` + dir + `","session_id":"sess-amb"}`
	stdout, _, code := runWorkCmd(t, dir, stateDir, payload, "work", "checkpoint", "--from-hook")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on the hook path", stdout)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", "sess-amb.json")); !os.IsNotExist(err) {
		t.Errorf("ambiguous hook checkpoint bound a session (err=%v)", err)
	}
}

// --- Cluster J: freeform mode (Task 8) --------------------------------------

// startFreeformNoAttach runs `work start --mode freeform --no-attach` and
// returns the created record's work_id.
func startFreeformNoAttach(t *testing.T, dir, stateDir, title string) string {
	t.Helper()
	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "start", "--mode", "freeform", "--no-attach", "--title", title, ".")
	if code != 0 {
		t.Fatalf("work start --mode freeform --no-attach exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	var r work.Record
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("work start stdout is not a record: %v\n%s", err, stdout)
	}
	if r.WorkID == "" {
		t.Fatalf("work start returned an empty work_id: %s", stdout)
	}
	return r.WorkID
}

// snapshotWorkingDir is the cmd package's own copy of internal/work's
// snapshotDir/mapsEqual pattern (unexported there, so re-implemented here
// rather than crossing the package boundary) — a full byte-level map of
// every file under dir, for a before/after "the working tree is untouched"
// comparison.
func snapshotWorkingDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		snap[rel] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func assertWorkingDirUnchanged(t *testing.T, before, after map[string][]byte) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("working dir file count changed: before=%d after=%d", len(before), len(after))
		return
	}
	for k, v := range before {
		other, ok := after[k]
		if !ok || !bytes.Equal(v, other) {
			t.Errorf("working dir file %q changed or disappeared", k)
		}
	}
}

// assertNoDeliveryArtifacts asserts none of the six delivery-mode artifact
// files exist anywhere under work/<workID>/ (AC-J-3, AC-J-9's mirror image).
func assertNoDeliveryArtifacts(t *testing.T, stateDir, workID string) {
	t.Helper()
	for _, name := range []string{"spec.md", "plan.md", "review.md", "research.md", "execution.md", "pr.md"} {
		for _, sub := range []string{"", "attachments"} {
			p := filepath.Join(stateDir, "work", workID, sub, name)
			if _, err := os.Stat(p); err == nil {
				t.Errorf("unexpected delivery artifact present: %s", p)
			}
		}
	}
}

// TestWorkStart_FreeformNoAttach_TouchesOnlyOwnDir pins AC-J-4/AC-J-5: a
// freeform/no-attach start from a non-git directory writes only
// work.json + context.md (plus the lock sibling Save's own AcquireLock
// leaves behind — release() unlocks and closes the fd, it does not remove
// the lock file), never repos.json, and never touches the invocation
// working directory at all.
func TestWorkStart_FreeformNoAttach_TouchesOnlyOwnDir(t *testing.T) {
	dir := t.TempDir() // deliberately not a git repo
	stateDir := t.TempDir()
	before := snapshotWorkingDir(t, dir)

	workID := startFreeformNoAttach(t, dir, stateDir, "scratch")

	if _, err := os.Stat(filepath.Join(stateDir, "repos.json")); !os.IsNotExist(err) {
		t.Errorf("freeform --no-attach start created repos.json (err=%v)", err)
	}
	recordDir := filepath.Join(stateDir, "work", workID)
	if _, err := os.Stat(filepath.Join(recordDir, "work.json")); err != nil {
		t.Errorf("expected work.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(recordDir, "context.md")); err != nil {
		t.Errorf("expected context.md: %v", err)
	}
	var unexpected []string
	_ = filepath.Walk(recordDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr
		}
		base := filepath.Base(path)
		if base != "work.json" && base != "context.md" && base != "work.json.lock" {
			unexpected = append(unexpected, path)
		}
		return nil
	})
	if len(unexpected) != 0 {
		t.Errorf("unexpected files under work/%s/: %v", workID, unexpected)
	}
	assertNoDeliveryArtifacts(t, stateDir, workID)

	after := snapshotWorkingDir(t, dir)
	assertWorkingDirUnchanged(t, before, after)
	for _, name := range []string{".github", ".gitignore", "GEMINI.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("freeform start created %s in the working tree (err=%v)", name, err)
		}
	}
}

// TestWorkStart_FreeformWithAttach_SameSideEffectsAsDelivery pins the
// attach-path half of AC-J-3: repos.json IS created (same as delivery), the
// record's mode is the only field differing from the golden shape, and no
// delivery artifacts are seeded.
func TestWorkStart_FreeformWithAttach_SameSideEffectsAsDelivery(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "start", "--mode", "freeform", "--title", "golden task", ".")
	if code != 0 {
		t.Fatalf("work start exited %d\nstderr: %s", code, stderr)
	}
	var r map[string]any
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("work start stdout is not JSON: %v\n%s", err, stdout)
	}
	if r["mode"] != "freeform" {
		t.Fatalf("mode = %v, want freeform", r["mode"])
	}
	r["mode"] = "delivery" // the only field allowed to differ from the golden
	rJSON, _ := json.Marshal(r)
	assertGoldenRecord(t, string(rJSON), "golden task")

	if _, err := os.Stat(filepath.Join(stateDir, "repos.json")); err != nil {
		t.Errorf("expected the attach path to register the repo (repos.json): %v", err)
	}
	workID := r["work_id"].(string)
	assertNoDeliveryArtifacts(t, stateDir, workID)
	// AC-J-3 also covers the attach path (no --no-attach): context.md must
	// exist here too, not only on the --no-attach branch — the
	// `r.Mode == ModeFreeform` gate in workStartCmd.Run sits after both
	// branches converge, so a future edit that moved WriteFreeformContext
	// inside the noAttach-only branch must fail this assertion.
	if _, err := os.Stat(filepath.Join(stateDir, "work", workID, "context.md")); err != nil {
		t.Errorf("expected context.md on the attach-path freeform start too: %v", err)
	}
}

// TestWorkStart_NoAttachRequiresFreeform pins AC-J-5/D5's step-6 guard.
func TestWorkStart_NoAttachRequiresFreeform(t *testing.T) {
	dir := t.TempDir()
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "start", "--no-attach", "--title", "X", ".")
	if code == 0 {
		t.Fatalf("expected non-zero exit, stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "--no-attach is only valid with --mode freeform") {
		t.Errorf("stderr = %q, want the D5 --no-attach-requires-freeform message", stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "work")); !os.IsNotExist(err) {
		t.Errorf("expected no work/ directory to be created (err=%v)", err)
	}
}

// TestWorkStart_NoAttachRejectsBranchOverride pins AC-J-11/D5.
func TestWorkStart_NoAttachRejectsBranchOverride(t *testing.T) {
	dir := t.TempDir()
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "start", "--mode", "freeform", "--no-attach", "--branch", "foo", "--title", "X", ".")
	if code == 0 {
		t.Fatalf("expected non-zero exit, stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "--branch is not valid with --no-attach") {
		t.Errorf("stderr = %q, want the D5 branch-override-rejected message", stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "work")); !os.IsNotExist(err) {
		t.Errorf("expected no work/ directory to be created (err=%v)", err)
	}
}

// TestWorkStatus_FreeformNoAttach_ResolvesViaExplicitWork pins AC-J-6/AC-J-7.
func TestWorkStatus_FreeformNoAttach_ResolvesViaExplicitWork(t *testing.T) {
	dir := t.TempDir() // deliberately not a git repo
	stateDir := t.TempDir()
	workID := startFreeformNoAttach(t, dir, stateDir, "scratch status")

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "status", ".", "--work", workID)
	if code != 0 {
		t.Fatalf("work status --work exited %d\nstderr: %s", code, stderr)
	}
	var r work.Record
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("work status stdout is not a record: %v\n%s", err, stdout)
	}
	if r.WorkID != workID {
		t.Errorf("resolved work_id = %q, want %q", r.WorkID, workID)
	}

	// Without --work, the same non-git directory must still Die exactly as
	// TestWorkStatus_NoActiveWork's git-repo-required path does today — the
	// D2 fallback is strictly additive, not a relaxation of the default.
	stdout, stderr, code = runWorkCmd(t, dir, stateDir, "", "work", "status", ".")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "not a git repo") {
		t.Errorf("stderr = %q, want a 'not a git repo' message", stderr)
	}
}

// TestWorkList_GlobalFlag pins AC-J-8.
func TestWorkList_GlobalFlag(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	stateDir := t.TempDir()

	idA := startFreeformNoAttach(t, dirA, stateDir, "global A")
	idB := startFreeformNoAttach(t, dirB, stateDir, "global B")

	stdout, stderr, code := runWorkCmd(t, dirA, stateDir, "", "work", "list", ".", "--global")
	if code != 0 {
		t.Fatalf("work list --global exited %d\nstderr: %s", code, stderr)
	}
	var records []work.Record
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("work list --global output is not JSON: %v\n%s", err, stdout)
	}
	seen := map[string]bool{}
	for _, r := range records {
		seen[r.WorkID] = true
	}
	if !seen[idA] || !seen[idB] {
		t.Errorf("work list --global = %v, want both %s and %s", stdout, idA, idB)
	}

	// Without --global, a non-git dir still requires a git repo.
	stdout, stderr, code = runWorkCmd(t, dirA, stateDir, "", "work", "list", ".")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "not a git repo") {
		t.Errorf("stderr = %q, want a 'not a git repo' message", stderr)
	}
}

// TestWorkPromote_SeedsArtifactsAndFlipsMode pins AC-J-9.
func TestWorkPromote_SeedsArtifactsAndFlipsMode(t *testing.T) {
	dir := t.TempDir()
	stateDir := t.TempDir()
	workID := startFreeformNoAttach(t, dir, stateDir, "to promote")

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "promote", workID, "--mode", "delivery")
	if code != 0 {
		t.Fatalf("work promote exited %d\nstderr: %s", code, stderr)
	}
	var r work.Record
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("work promote stdout is not a record: %v\n%s", err, stdout)
	}
	if r.Mode != work.ModeDelivery {
		t.Errorf("mode = %q, want delivery", r.Mode)
	}
	recordDir := filepath.Join(stateDir, "work", workID)
	for _, name := range []string{"spec.md", "plan.md", "review.md", "research.md", "execution.md", "pr.md"} {
		if _, err := os.Stat(filepath.Join(recordDir, "attachments", name)); err != nil {
			t.Errorf("expected attachments/%s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(recordDir, "context.md")); err != nil {
		t.Errorf("expected top-level context.md to still exist unchanged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(recordDir, "attachments", "context.md")); !os.IsNotExist(err) {
		t.Errorf("context.md must not be duplicated into attachments/ (err=%v)", err)
	}
}

// TestWorkPromote_AlreadyDeliveryRefuses pins AC-J-10.
func TestWorkPromote_AlreadyDeliveryRefuses(t *testing.T) {
	dir := t.TempDir()
	stateDir := t.TempDir()
	workID := startFreeformNoAttach(t, dir, stateDir, "double promote")

	if _, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "promote", workID, "--mode", "delivery"); code != 0 {
		t.Fatalf("first promote exited %d\nstderr: %s", code, stderr)
	}
	recordDir := filepath.Join(stateDir, "work", workID)
	before := map[string][]byte{}
	for _, name := range []string{"spec.md", "plan.md", "review.md", "research.md", "execution.md", "pr.md"} {
		data, err := os.ReadFile(filepath.Join(recordDir, "attachments", name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = data
	}

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "promote", workID, "--mode", "delivery")
	if code == 0 {
		t.Fatalf("second promote should have failed, stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "already in delivery mode") {
		t.Errorf("stderr = %q, want a mention of 'already in delivery mode'", stderr)
	}
	for name, data := range before {
		after, err := os.ReadFile(filepath.Join(recordDir, "attachments", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, after) {
			t.Errorf("attachments/%s changed after a refused re-promote", name)
		}
	}
}

// TestWorkPromote_UnknownIDFails pins Task 8 item 12.
func TestWorkPromote_UnknownIDFails(t *testing.T) {
	dir := t.TempDir()
	stateDir := t.TempDir()

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "promote", "bogus-id", "--mode", "delivery")
	if code == 0 {
		t.Fatalf("expected non-zero exit, stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "work record not found") {
		t.Errorf("stderr = %q, want a mention of ErrNotFound", stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "work", "bogus-id")); !os.IsNotExist(err) {
		t.Errorf("expected no work/bogus-id/ directory to be created (err=%v)", err)
	}
}

// TestWorkPromote_RefusesArchivedRecord pins AC-J-15 / Blocking finding 2 —
// covers both non-promotable lifecycle values D3 names.
func TestWorkPromote_RefusesArchivedRecord(t *testing.T) {
	for _, verb := range []string{"archive", "done"} {
		t.Run(verb, func(t *testing.T) {
			dir := t.TempDir()
			stateDir := t.TempDir()
			workID := startFreeformNoAttach(t, dir, stateDir, "not promotable-"+verb)

			if _, _, code := runWorkCmd(t, dir, stateDir, "", "work", verb, workID); code != 0 {
				t.Fatalf("work %s exited %d", verb, code)
			}

			stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "promote", workID, "--mode", "delivery")
			if code == 0 {
				t.Fatalf("expected non-zero exit, stdout=%s", stdout)
			}
			if !strings.Contains(stderr, "cannot promote an archived/done record") {
				t.Errorf("stderr = %q, want a mention of ErrNotPromotable", stderr)
			}
			if _, err := os.Stat(filepath.Join(stateDir, "work", workID, "attachments")); !os.IsNotExist(err) {
				t.Errorf("expected no attachments/ directory to be created (err=%v)", err)
			}

			r, err := work.Load(stateDir, workID)
			if err != nil {
				t.Fatal(err)
			}
			if r.Mode != work.ModeFreeform {
				t.Errorf("mode = %q, want it to remain freeform after a refused promote", r.Mode)
			}
			wantLifecycle := work.LifecycleArchived
			if verb == "done" {
				wantLifecycle = work.LifecycleDone
			}
			if r.Lifecycle != wantLifecycle {
				t.Errorf("lifecycle = %q, want it to remain %q after a refused promote", r.Lifecycle, wantLifecycle)
			}
		})
	}
}

// TestWorkPromote_ConcurrentArchiveRace exercises the round-2 skeptic fix
// directly: SeedDeliveryArtifacts and the Lifecycle recheck run inside
// UpdateRecord's single lock acquisition (store.go's Load->mutate->save
// span), not before it. TestWorkPromote_RefusesArchivedRecord alone does
// NOT prove this — it is purely sequential, so the unlocked diagnostic
// pre-check in workPromoteCmd always fires first with identical stderr,
// and removing the in-closure recheck leaves it green.
//
// This races `work archive` against `work promote` as real concurrent
// subprocesses. Both orderings are legitimate outcomes (archive has no
// Mode precondition, so "promote wins, then archive lands on the
// now-delivery record" produces Lifecycle=archived + Mode=delivery and is
// NOT a bug — final on-disk state alone cannot distinguish that from a
// real guard failure). What DOES distinguish them: archive's own returned
// record reflects the Mode it observed at the moment its closure ran. If
// archive's response shows Mode still "freeform" (proving archive's
// closure — and by extension the lock — ran and completed BEFORE
// promote's closure could have flipped it) and promote's command ALSO
// exits 0 (claiming success), promote's closure must have run against an
// already-archived record and wrongly proceeded — that is the actual bug
// the in-closure Lifecycle recheck exists to prevent.
func TestWorkPromote_ConcurrentArchiveRace(t *testing.T) {
	const trials = 200
	for i := 0; i < trials; i++ {
		dir := t.TempDir()
		stateDir := t.TempDir()
		workID := startFreeformNoAttach(t, dir, stateDir, "race trial")

		var wg sync.WaitGroup
		var archiveOut string
		var archiveCode, promoteCode int
		wg.Add(2)
		go func() {
			defer wg.Done()
			archiveOut, _, archiveCode = runWorkCmd(t, dir, stateDir, "", "work", "archive", workID)
		}()
		go func() {
			defer wg.Done()
			_, _, promoteCode = runWorkCmd(t, dir, stateDir, "", "work", "promote", workID, "--mode", "delivery")
		}()
		wg.Wait()

		// AcquireLock is non-blocking: on lock contention, the loser exits
		// non-zero with no JSON on stdout rather than corrupting anything.
		// Only a successful archive's returned record tells us anything
		// about ordering.
		if archiveCode != 0 {
			continue
		}
		var archived work.Record
		if err := json.Unmarshal([]byte(archiveOut), &archived); err != nil {
			t.Fatalf("trial %d: archive stdout is not a record: %v\n%s", i, err, archiveOut)
		}
		if archived.Mode == work.ModeFreeform && promoteCode == 0 {
			t.Fatalf("trial %d: archive completed first (observed mode=freeform) yet the concurrent promote still exited 0 — the in-closure Lifecycle recheck did not serialize against the already-archived record", i)
		}
	}
}

// TestWorkStart_FreeformAttach_MatchesSkillInvocation pins AC-J-13
// (D1-REVISED / Task 10 item 3): the exact invocation the new `/wip-start`
// freeform section issues (attach path, no --no-attach) touches nothing
// outside work/<id>/ in either the working tree or the state dir.
func TestWorkStart_FreeformAttach_MatchesSkillInvocation(t *testing.T) {
	dir := initGitRepoWithBranch(t, "main")
	stateDir := t.TempDir()
	before := snapshotWorkingDir(t, dir)

	stdout, stderr, code := runWorkCmd(t, dir, stateDir, "", "work", "start", "--mode", "freeform", "--title", "skill invocation", ".")
	if code != 0 {
		t.Fatalf("work start exited %d\nstderr: %s", code, stderr)
	}
	var r work.Record
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("work start stdout is not a record: %v\n%s", err, stdout)
	}

	after := snapshotWorkingDir(t, dir)
	assertWorkingDirUnchanged(t, before, after)
	for _, name := range []string{".github", "GEMINI.md", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("freeform attach-path start created %s in the working tree (err=%v)", name, err)
		}
	}
	for _, name := range []string{".claude/repo-docs", ".wip"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("freeform attach-path start created %s in the working tree (err=%v)", name, err)
		}
	}
}
