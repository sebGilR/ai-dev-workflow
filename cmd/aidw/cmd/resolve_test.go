package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// The resolve-wip contract is consumed by shell code (save-wip-snapshot.sh and
// the hook wrappers), which parses two exact stdout lines and branches on the
// exit code. These tests must therefore run the real binary in a subprocess:
// the command calls os.Exit directly, and an in-process test would either exit
// the test binary or silently miss the exit-code half of the contract.

var (
	aidwBinOnce sync.Once
	aidwBinPath string
	aidwBinErr  error
)

// buildAidw builds the aidw binary once per test binary run and returns its
// path.
func buildAidw(t *testing.T) string {
	t.Helper()
	aidwBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "aidw-bin")
		if err != nil {
			aidwBinErr = err
			return
		}
		bin := filepath.Join(dir, "aidw")
		cmd := exec.Command("go", "build", "-o", bin, "aidw/cmd/aidw")
		cmd.Dir = repoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			aidwBinErr = &buildError{out: string(out), err: err}
			return
		}
		aidwBinPath = bin
	})
	if aidwBinErr != nil {
		t.Fatalf("build aidw: %v", aidwBinErr)
	}
	return aidwBinPath
}

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.out }

// initTestGitRepo creates a git repo with one commit and no .wip directory.
func initTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

// runAidw runs the built binary and returns stdout, stderr and the exit code.
func runAidw(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(buildAidw(t), args...)
	cmd.Dir = dir
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

// TestResolveWip_NotFound pins the "no active work" half of the contract:
// exit 0 (not an error) with the two exists=false lines the shell parses.
func TestResolveWip_NotFound(t *testing.T) {
	dir := initTestGitRepo(t)

	stdout, _, code := runAidw(t, dir, "resolve-wip", ".")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if want := "exists=false\nwip_dir=\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if _, err := os.Stat(filepath.Join(dir, ".wip")); !os.IsNotExist(err) {
		t.Error("resolve-wip must not create a .wip directory")
	}
}

// TestResolveWip_Found pins the "found" half: exit 0 and the exact wip_dir
// line, whose value the shell assigns directly.
func TestResolveWip_Found(t *testing.T) {
	dir := initTestGitRepo(t)

	// `aidw start` is the sanctioned creator of branch state.
	if _, _, code := runAidw(t, dir, "start", "."); code != 0 {
		t.Fatalf("aidw start exited %d", code)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".wip", "*", "status.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one seeded wip dir, got %v (%v)", matches, err)
	}
	// git rev-parse --show-toplevel resolves symlinks (on macOS /var is a
	// symlink to /private/var), so compare against the resolved path.
	wantDir, err := filepath.EvalSymlinks(filepath.Dir(matches[0]))
	if err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runAidw(t, dir, "resolve-wip", ".")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if want := "exists=true\nwip_dir=" + wantDir + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestResolveWip_NotAGitRepo pins the third state the shell distinguishes:
// a real error is exit 1 with nothing on stdout, so the caller never mistakes
// "couldn't check" for "no active work".
func TestResolveWip_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()

	stdout, _, code := runAidw(t, dir, "resolve-wip", ".")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on error", stdout)
	}
}
