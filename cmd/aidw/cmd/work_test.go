package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
