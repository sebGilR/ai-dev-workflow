package cmd

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"

	"aidw/cmd/aidw/internal/hookgate"
)

// runAidwStdin is like runAidw but also feeds stdin — hook-gate reads its
// payload from stdin, not args.
func runAidwStdin(t *testing.T, dir, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(buildAidw(t), args...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewBufferString(stdin)
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

// TestHookGate_EmptyStdin_ProducesNoOutput asserts the rendering-contract
// fix: an allow decision (empty/unparseable stdin falls through to allow)
// must produce zero bytes of stdout, not an explicit "allow" JSON. Empty
// stdout is Claude Code's documented "no opinion, defer to the normal
// permission flow" signal — see hookgate.RenderOutput's doc comment.
func TestHookGate_EmptyStdin_ProducesNoOutput(t *testing.T) {
	dir := initTestGitRepo(t)
	stdout, _, code := runAidwStdin(t, dir, `{}`, "hook-gate")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout for an allow decision, got %q", stdout)
	}
}

func TestHookGate_DenyShapedPayload_ExactReason(t *testing.T) {
	dir := initTestGitRepo(t)
	payload := `{"tool_name":"Edit","cwd":"` + dir + `","tool_input":{"file_path":"` + dir + `/x.go"}}`
	stdout, _, code := runAidwStdin(t, dir, payload, "hook-gate")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var v struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("stdout not valid JSON: %v (%q)", err, stdout)
	}
	if v.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("decision = %q, want deny", v.HookSpecificOutput.PermissionDecision)
	}
	if v.HookSpecificOutput.PermissionDecisionReason != hookgate.DenyReason {
		t.Fatalf("reason = %q", v.HookSpecificOutput.PermissionDecisionReason)
	}
}

func TestHookGate_NoStdin_DoesNotBlock_ProducesNoOutput(t *testing.T) {
	dir := initTestGitRepo(t)
	cmd := exec.Command(buildAidw(t), "hook-gate")
	cmd.Dir = dir
	cmd.Stdin = nil // no pipe attached; the command must not block reading it
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run aidw hook-gate: %v (%s)", err, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("expected empty stdout for an allow decision, got %q", stdout.String())
	}
}
