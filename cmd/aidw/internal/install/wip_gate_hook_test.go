package install

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

func TestWipGateScript_ActiveWipSession_Allows(t *testing.T) {
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
	decision, _ := permissionDecision(t, stdout)
	if decision != "allow" {
		t.Fatalf("decision = %q, want allow", decision)
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

func TestWipGateScript_BinaryMissing_AllowsHardcodedLiteral(t *testing.T) {
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
	if strings.TrimSpace(stdout) != string(hookgate.AllowOutput()) {
		t.Fatalf("stdout = %q, want the hardcoded allow literal %q", stdout, hookgate.AllowOutput())
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

// TestWipGateScript_AllowJSONLiteralPinnedToAllowOutput is the MINOR 2 fix:
// the script's hardcoded allow_json literal and hookgate.AllowOutput() are
// independently maintained strings that must stay byte-identical, or a
// future edit to one that forgets the other drifts silently.
func TestWipGateScript_AllowJSONLiteralPinnedToAllowOutput(t *testing.T) {
	scriptSrc := filepath.Join(repoRoot(t), "templates", "global", "scripts", "wip-gate.sh")
	data, err := os.ReadFile(scriptSrc)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`allow_json='({.*})'`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatal("could not find allow_json literal in wip-gate.sh")
	}
	if string(m[1]) != string(hookgate.AllowOutput()) {
		t.Fatalf("wip-gate.sh allow_json = %q, hookgate.AllowOutput() = %q", m[1], hookgate.AllowOutput())
	}
}
