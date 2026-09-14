package hookgate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func noSkipGetenv(string) string { return "" }

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// initGitRepo creates a real temp git repo with one commit on branch, so
// wip.FindBranchState / git.Toplevel / git.CurrentBranch all run against a
// real subprocess, matching this repo's existing hook-test convention of not
// mocking git/wip/policy.
func initGitRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	initArgs := []string{"init", "-q"}
	if branch != "" {
		initArgs = append(initArgs, "-b", branch)
	}
	run(t, dir, initArgs...)
	run(t, dir, "config", "user.email", "test@test.com")
	run(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// initUnbornGitRepo creates a git repo with zero commits (unborn HEAD) — the
// BLOCKER 1 regression case. initGitRepo/initHookGitRepo-style helpers
// elsewhere in this codebase always create an initial commit, so this needs
// its own local helper.
func initUnbornGitRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	initArgs := []string{"init", "-q"}
	if branch != "" {
		initArgs = append(initArgs, "-b", branch)
	}
	run(t, dir, initArgs...)
	return dir
}

func writeActiveWipSession(t *testing.T, repoTop, branch string) {
	t.Helper()
	wipDir := filepath.Join(repoTop, ".wip", "20260101000000-"+branch)
	if err := os.MkdirAll(wipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	status := `{"repo":"x","repo_path":"` + repoTop + `","branch":"` + branch + `","stage":"started"}`
	if err := os.WriteFile(filepath.Join(wipDir, "status.json"), []byte(status), 0o644); err != nil {
		t.Fatal(err)
	}
}

func payload(t *testing.T, m map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestEvaluate_EmptyBody(t *testing.T) {
	d := Evaluate(nil, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "could not parse") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_MalformedJSON(t *testing.T) {
	d := Evaluate([]byte("{not json"), noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "could not parse") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_SkipGateEnv(t *testing.T) {
	dir := initGitRepo(t, "main")
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": filepath.Join(dir, "x.go")}})
	d := Evaluate(in, func(k string) string {
		if k == "AIDW_SKIP_GATE" {
			return "1"
		}
		return ""
	})
	if !d.Allow || !strings.Contains(d.Reason, "AIDW_SKIP_GATE") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_ToolNotGated(t *testing.T) {
	dir := initGitRepo(t, "main")
	in := payload(t, map[string]any{"tool_name": "Bash", "cwd": dir})
	d := Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "not gated") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_SubagentCall(t *testing.T) {
	dir := initGitRepo(t, "main")
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "agent_id": "sub-1"})
	d := Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "subagent") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir})
	d := Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "not a git repo") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_EditTargetOutsideRepo(t *testing.T) {
	dir := initGitRepo(t, "main")
	outside := t.TempDir()
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": filepath.Join(outside, "x.go")}})
	d := Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "outside repo") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_BmadDirAllows(t *testing.T) {
	dir := initGitRepo(t, "main")
	if err := os.MkdirAll(filepath.Join(dir, ".bmad"), 0o755); err != nil {
		t.Fatal(err)
	}
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": filepath.Join(dir, "x.go")}})
	d := Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, ".bmad") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_PolicyWipGateDisabled(t *testing.T) {
	dir := initGitRepo(t, "main")
	if err := os.MkdirAll(filepath.Join(dir, ".aidw"), 0o755); err != nil {
		t.Fatal(err)
	}
	policyJSON := `{"wip_gate":"disabled","rules":[]}`
	if err := os.WriteFile(filepath.Join(dir, ".aidw", "policy.json"), []byte(policyJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": filepath.Join(dir, "x.go")}})
	d := Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "wip_gate disabled") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_PolicyWipGateDisabledMixedCase(t *testing.T) {
	dir := initGitRepo(t, "main")
	if err := os.MkdirAll(filepath.Join(dir, ".aidw"), 0o755); err != nil {
		t.Fatal(err)
	}
	policyJSON := `{"wip_gate":"DISABLED"}`
	if err := os.WriteFile(filepath.Join(dir, ".aidw", "policy.json"), []byte(policyJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": filepath.Join(dir, "x.go")}})
	d := Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "wip_gate disabled") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_ActiveWipSessionAllows(t *testing.T) {
	dir := initGitRepo(t, "main")
	writeActiveWipSession(t, dir, "main")
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": filepath.Join(dir, "x.go")}})
	d := Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "active .wip session") {
		t.Fatalf("got %+v", d)
	}
}

func TestEvaluate_NoWipSessionDenies(t *testing.T) {
	dir := initGitRepo(t, "main")
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": filepath.Join(dir, "x.go")}})
	d := Evaluate(in, noSkipGetenv)
	if d.Allow {
		t.Fatalf("expected deny, got %+v", d)
	}
	if d.Reason != DenyReason {
		t.Fatalf("reason mismatch: %q", d.Reason)
	}
}

func TestEvaluate_DetachedHeadDenies(t *testing.T) {
	dir := initGitRepo(t, "main")
	// Detach HEAD.
	run(t, dir, "checkout", "--detach", "-q")
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": filepath.Join(dir, "x.go")}})
	d := Evaluate(in, noSkipGetenv)
	if d.Allow {
		t.Fatalf("expected deny for detached-head w/ no session, got %+v", d)
	}
}

// TestEvaluate_UnbornHeadDenies is the BLOCKER 1 regression test: without
// resolveGateBranch, a zero-commit repo's branch resolution fails generically
// and the gate incorrectly fails open.
func TestEvaluate_UnbornHeadDenies(t *testing.T) {
	dir := initUnbornGitRepo(t, "feature-x")
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": filepath.Join(dir, "x.go")}})
	d := Evaluate(in, noSkipGetenv)
	if d.Allow {
		t.Fatalf("expected deny for unborn-HEAD repo w/ no session, got %+v", d)
	}
	if d.Reason != DenyReason {
		t.Fatalf("reason mismatch: %q", d.Reason)
	}
}

// TestTargetOutsideRepo_CaseFold exercises the MAJOR 3 fix directly: a
// case-only difference between repoTop and the reported target must not be
// treated as "outside" on darwin/windows. isWithin is the underlying
// comparison helper (GOOS-gating lives in targetOutsideRepo itself), so this
// asserts the fold-case comparison behaves correctly regardless of which
// GOOS actually runs the test.
func TestTargetOutsideRepo_CaseFold(t *testing.T) {
	top := "/tmp/Foo"
	target := "/tmp/foo/x.go"
	if isWithin(target, top, false) {
		t.Fatal("exact-match comparison should not match differing case")
	}
	if !isWithin(target, top, true) {
		t.Fatal("case-folded comparison should match a case-only difference")
	}
}

func TestEvaluate_NonStringFilePathDoesNotPanic(t *testing.T) {
	dir := initGitRepo(t, "main")
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": 42}})
	d := Evaluate(in, noSkipGetenv) // must not panic
	// Falls through to normal gate logic: no session -> deny.
	if d.Allow {
		t.Fatalf("expected deny (fell through to normal gate logic), got %+v", d)
	}
}

func TestRenderOutputAndAllowOutput_RoundTrip(t *testing.T) {
	for _, d := range []Decision{{Allow: true, Reason: "x"}, {Allow: false, Reason: DenyReason}} {
		out := RenderOutput(d)
		var v map[string]any
		if err := json.Unmarshal(out, &v); err != nil {
			t.Fatalf("RenderOutput not valid JSON: %v (%s)", err, out)
		}
	}
	var v map[string]any
	if err := json.Unmarshal(AllowOutput(), &v); err != nil {
		t.Fatalf("AllowOutput not valid JSON: %v", err)
	}
}
