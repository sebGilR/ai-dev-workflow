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

// TestEvaluate_CaseFoldGate_ThroughFullDecisionChain exercises the MAJOR 3
// fix through Evaluate itself (not just isWithin directly), on both settings
// of caseInsensitiveFS via the package var — a test that only called
// isWithin could pass even if targetOutsideRepo's GOOS-gating were deleted
// entirely, since deleting that gate breaks no test that never calls
// Evaluate along this path. This one would.
//
// Constructing a genuine case-only path difference without a spurious
// symlink-resolution mismatch (e.g. macOS's /tmp -> /private/tmp) requires
// resolving the repo dir once up front and deriving both the "top" and the
// case-flipped "target" from that same resolved base — otherwise
// resolveSymlinksBestEffort's independent per-side resolution (the
// MEDIUM-4-adjacent fix already in this package) can introduce an unrelated
// prefix mismatch that has nothing to do with case-folding.
func TestEvaluate_CaseFoldGate_ThroughFullDecisionChain(t *testing.T) {
	// t.TempDir()'s own leaf component is typically a bare numeric counter
	// ("001") with no letters to case-flip, so nest a lowercase-named repo
	// dir underneath it rather than case-flipping t.TempDir()'s leaf itself.
	parent := t.TempDir()
	raw := filepath.Join(parent, "reponame")
	if err := os.Mkdir(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, raw, "init", "-q", "-b", "main")
	run(t, raw, "config", "user.email", "test@test.com")
	run(t, raw, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(raw, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, raw, "add", ".")
	run(t, raw, "commit", "-q", "-m", "init")

	dir, err := filepath.EvalSymlinks(raw)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", raw, err)
	}
	upperBase := strings.ToUpper(filepath.Base(dir))
	if upperBase == filepath.Base(dir) {
		t.Fatal("expected the repo dir's own base name to contain case-foldable letters")
	}
	caseFlippedDir := filepath.Join(filepath.Dir(dir), upperBase)
	target := filepath.Join(caseFlippedDir, "x.go")
	in := payload(t, map[string]any{"tool_name": "Edit", "cwd": dir, "tool_input": map[string]any{"file_path": target}})

	orig := caseInsensitiveFS
	defer func() { caseInsensitiveFS = orig }()

	caseInsensitiveFS = true
	d := Evaluate(in, noSkipGetenv)
	if d.Allow != false || d.Reason != DenyReason {
		t.Fatalf("with caseInsensitiveFS=true, a case-only target must fall through to normal gate logic (deny, no session); got %+v", d)
	}

	caseInsensitiveFS = false
	d = Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "outside repo") {
		t.Fatalf("with caseInsensitiveFS=false, a case-only difference on a case-sensitive filesystem should be treated as outside repo; got %+v", d)
	}
}

// TestEvaluate_NotebookPathField covers the notebook_path branch of
// targetOutsideRepo, which had zero test coverage despite NotebookEdit being
// one of the three gated tools — every other Task 1 AC exercised file_path.
func TestEvaluate_NotebookPathField(t *testing.T) {
	dir := initGitRepo(t, "main")
	outside := t.TempDir()
	in := payload(t, map[string]any{"tool_name": "NotebookEdit", "cwd": dir, "tool_input": map[string]any{"notebook_path": filepath.Join(outside, "nb.ipynb")}})
	d := Evaluate(in, noSkipGetenv)
	if !d.Allow || !strings.Contains(d.Reason, "outside repo") {
		t.Fatalf("got %+v", d)
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

// TestRenderOutput_AllowIsEmpty is the rendering-contract regression test
// (see spec.md's "Rendering-contract correction"): an allow Decision must
// render to zero bytes, never JSON — empty stdout is Claude Code's
// documented "no opinion, defer to the normal permission flow" signal, and
// any explicit permissionDecision (including "allow") is an affirmative
// decision that overrides the user's own permissions.ask/deny rules. This
// hook must never do that for a case it doesn't actually care about.
func TestRenderOutput_AllowIsEmpty(t *testing.T) {
	out := RenderOutput(Decision{Allow: true, Reason: "some allow reason"})
	if len(out) != 0 {
		t.Fatalf("expected zero bytes for an allow decision, got %q", out)
	}
}

// TestRenderOutput_DenyShape asserts actual field values, not just "is this
// valid JSON" — a value round-trip test that only checks json.Unmarshal
// succeeds would pass even if permissionDecision were inverted or
// hookEventName were dropped.
func TestRenderOutput_DenyShape(t *testing.T) {
	out := RenderOutput(Decision{Allow: false, Reason: DenyReason})
	var v struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("RenderOutput not valid JSON: %v (%s)", err, out)
	}
	if v.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Fatalf("hookEventName = %q, want PreToolUse", v.HookSpecificOutput.HookEventName)
	}
	if v.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("permissionDecision = %q, want deny", v.HookSpecificOutput.PermissionDecision)
	}
	if v.HookSpecificOutput.PermissionDecisionReason != DenyReason {
		t.Fatalf("permissionDecisionReason = %q, want %q", v.HookSpecificOutput.PermissionDecisionReason, DenyReason)
	}
}
