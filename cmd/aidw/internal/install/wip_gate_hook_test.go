package install

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aidw/cmd/aidw/internal/hookgate"
)

func runWipGateScript(t *testing.T, home, cwd, path, stdin string) (stdout string, exitCode int) {
	t.Helper()
	script := filepath.Join(home, ".claude", "wip-gate.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = cwd
	cmd.Env = []string{"HOME=" + home, "PATH=" + path}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	outB, err := cmd.Output()
	stdout = string(outB)
	exitCode = 0
	if ee, ok := err.(*exec.ExitError); ok {
		exitCode = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run wip-gate.sh: %v", err)
	}
	return stdout, exitCode
}

func permissionDecision(t *testing.T, stdout string) (decision, reason string) {
	t.Helper()
	var v struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("stdout not valid JSON: %v (%q)", err, stdout)
	}
	return v.HookSpecificOutput.PermissionDecision, v.HookSpecificOutput.PermissionDecisionReason
}

// wipGateTestEnv wires up an isolated $HOME with both the managed aidw
// binary (via hookTestEnv) and the wip-gate.sh wrapper script extracted
// alongside it, so the script's binary-resolution path matches production.
func wipGateTestEnv(t *testing.T) (home string) {
	t.Helper()
	home = hookTestEnv(t)
	src := filepath.Join(repoRoot(t), "templates", "global", "scripts", "wip-gate.sh")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".claude", "wip-gate.sh")
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// TestWipGateScript_ActiveWipSession_ProducesNoOutput: an active .wip
// session is an allow decision, which — per the rendering-contract
// correction (see hookgate.RenderOutput's doc comment) — must produce zero
// bytes of stdout, not an explicit "allow" JSON. Empty stdout is Claude
// Code's documented "no opinion, defer to the normal permission flow"
// signal; this hook only ever has an opinion in the no-session deny case.
func TestWipGateScript_ActiveWipSession_ProducesNoOutput(t *testing.T) {
	home := wipGateTestEnv(t)
	dir := initHookGitRepo(t, "main")

	aidwBin := filepath.Join(home, ".claude", "ai-dev-workflow", "bin", "aidw")
	startCmd := exec.Command(aidwBin, "start", ".")
	startCmd.Dir = dir
	if out, err := startCmd.CombinedOutput(); err != nil {
		t.Fatalf("aidw start: %v\n%s", err, out)
	}

	payload := `{"tool_name":"Edit","cwd":"` + dir + `","tool_input":{"file_path":"` + dir + `/x.go"}}`
	stdout, code := runWipGateScript(t, home, dir, "/usr/bin:/bin", payload)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout for an allow decision, got %q", stdout)
	}
}

func TestWipGateScript_NoWipSession_Denies(t *testing.T) {
	home := wipGateTestEnv(t)
	dir := initHookGitRepo(t, "main")

	payload := `{"tool_name":"Edit","cwd":"` + dir + `","tool_input":{"file_path":"` + dir + `/x.go"}}`
	stdout, code := runWipGateScript(t, home, dir, "/usr/bin:/bin", payload)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	decision, reason := permissionDecision(t, stdout)
	if decision != "deny" {
		t.Fatalf("decision = %q, want deny", decision)
	}
	if reason != hookgate.DenyReason {
		t.Fatalf("reason = %q", reason)
	}
}

// TestWipGateScript_BinaryMissing_ProducesNoOutput: when aidw can't be
// resolved at all, the script must fail open by producing NO output (empty
// stdout, exit 0) — never an explicit "allow" JSON, which would be an
// affirmative decision overriding the user's own permission rules rather
// than a neutral "this hook has no opinion."
func TestWipGateScript_BinaryMissing_ProducesNoOutput(t *testing.T) {
	home := t.TempDir() // no aidw binary anywhere under this HOME
	scriptSrc := filepath.Join(repoRoot(t), "templates", "global", "scripts", "wip-gate.sh")
	scriptData, err := os.ReadFile(scriptSrc)
	if err != nil {
		t.Fatal(err)
	}
	scriptDest := filepath.Join(home, ".claude", "wip-gate.sh")
	if err := os.MkdirAll(filepath.Dir(scriptDest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptDest, scriptData, 0o755); err != nil {
		t.Fatal(err)
	}

	emptyPathDir := t.TempDir() // no aidw resolvable on PATH either
	stdout, code := runWipGateScript(t, home, home, emptyPathDir, `{"tool_name":"Edit","cwd":"/tmp"}`)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout when aidw is unresolvable, got %q", stdout)
	}
}

func TestBootstrap_ExtractsWipGateScript(t *testing.T) {
	home := t.TempDir()
	claudeHome := filepath.Join(home, ".claude")
	copilotHome := filepath.Join(home, ".copilot")
	if _, _, err := extractEmbedded(claudeHome, copilotHome, os.Stderr); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(claudeHome, "wip-gate.sh")
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("wip-gate.sh not extracted: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("wip-gate.sh not executable: mode=%v", info.Mode())
	}
}

// TestWipGateScript_NeverHardcodesAPermissionDecision is the MINOR 2 fix,
// revised for the rendering-contract correction: the script used to carry
// its own hardcoded allow_json literal (pinned against hookgate.AllowOutput()
// by an earlier version of this test), which this correction deleted
// entirely — every allow path is now silence, not JSON. Pin the absence
// instead of a literal that no longer exists: the script's source must
// contain no "permissionDecision" string of its own. The only place that
// string may ever appear in this hook's output is inside JSON produced by
// `aidw hook-gate` itself (the deny path), which this script always execs
// into rather than constructing output by hand.
func TestWipGateScript_NeverHardcodesAPermissionDecision(t *testing.T) {
	scriptSrc := filepath.Join(repoRoot(t), "templates", "global", "scripts", "wip-gate.sh")
	data, err := os.ReadFile(scriptSrc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "permissionDecision") {
		t.Fatalf("wip-gate.sh must never hardcode a permissionDecision literal of its own — every decision must come from `aidw hook-gate`'s output:\n%s", data)
	}
}
