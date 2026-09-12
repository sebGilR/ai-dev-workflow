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
