package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// repoRoot locates the checkout root by walking up from this test file's
// directory to the first ancestor containing go.mod — the same technique
// the E2 mirrors drift test uses, so hook-script tests can find
// templates/global/scripts/*.sh regardless of the working directory `go
// test` was invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (no go.mod found)")
		}
		dir = parent
	}
}

var (
	testBinOnce sync.Once
	testBinPath string
	testBinErr  error
)

// builtAidwBinary builds the aidw CLI once per test run and returns its
// path, so hook-script tests can exercise `aidw resolve-wip` /
// `aidw summarize-context` end-to-end instead of stubbing them out.
func builtAidwBinary(t *testing.T) string {
	t.Helper()
	testBinOnce.Do(func() {
		root := repoRoot(t)
		// Build into a fixed temp dir (not t.TempDir(), which is
		// per-test and gets cleaned up) since sync.Once only runs once
		// for the whole test binary run.
		dir := filepath.Join(os.TempDir(), "aidw-hooks-test-bin")
		_ = os.MkdirAll(dir, 0o755)
		out := filepath.Join(dir, "aidw")
		cmd := exec.Command("go", "build", "-o", out, "./cmd/aidw")
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			testBinErr = err
			t.Logf("go build output: %s", data)
			return
		}
		testBinPath = out
	})
	if testBinErr != nil {
		t.Fatalf("build aidw binary: %v", testBinErr)
	}
	return testBinPath
}

// hookTestEnv sets up an isolated $HOME with the managed aidw binary in the
// expected location, so the hook script's resolver finds it.
func hookTestEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	binDir := filepath.Join(home, ".claude", "ai-dev-workflow", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := builtAidwBinary(t)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "aidw"), data, 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func initHookGitRepo(t *testing.T, branch string) string {
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
	initArgs := []string{"init", "-q"}
	if branch != "" {
		initArgs = append(initArgs, "-b", branch)
	}
	run(initArgs...)
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func runHookScript(t *testing.T, home, cwd, level, stdin string) (stdout, stderr string, exitCode int) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "templates", "global", "scripts", "save-wip-snapshot.sh")
	cmd := exec.Command("bash", script, level)
	cmd.Dir = cwd
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	outB, err := cmd.Output()
	stdout = string(outB)
	exitCode = 0
	if ee, ok := err.(*exec.ExitError); ok {
		exitCode = ee.ExitCode()
		stderr = string(ee.Stderr)
	} else if err != nil {
		t.Fatalf("run hook script: %v", err)
	}
	return stdout, stderr, exitCode
}

func TestHookNoop_NonGitDirectory(t *testing.T) {
	home := hookTestEnv(t)
	nonGit := t.TempDir()

	if _, _, code := runHookScript(t, home, nonGit, "stop", ""); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if entries, _ := os.ReadDir(nonGit); len(entries) != 0 {
		t.Errorf("expected no files created in non-git dir, got %v", entries)
	}
}

func TestHookNoop_NoActiveWork(t *testing.T) {
	home := hookTestEnv(t)
	repo := initHookGitRepo(t, "")

	if _, _, code := runHookScript(t, home, repo, "precompact", ""); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(repo, ".wip")); !os.IsNotExist(err) {
		t.Error("hook must not create .wip when there is no active work")
	}
}

func TestHookDottedSlugNoCollision(t *testing.T) {
	home := hookTestEnv(t)
	repo := initHookGitRepo(t, "release.1")

	// A dir for the WRONG (unescaped-collision) slug already exists.
	wrong := filepath.Join(repo, ".wip", "20260101120000-releaseX1")
	if err := os.MkdirAll(wrong, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrong, "status.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runHookScript(t, home, repo, "stop", ""); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	if _, err := os.Stat(filepath.Join(wrong, "progress.log")); err == nil {
		t.Error("hook must not write into the mismatched-slug dir (release.1 vs releaseX1)")
	}

	// No other .wip dir should have been spontaneously created either.
	entries, _ := os.ReadDir(filepath.Join(repo, ".wip"))
	if len(entries) != 1 {
		t.Errorf("expected exactly the pre-existing dir, got %v", entries)
	}
}

func TestHookWritesSnapshot_WhenActiveWorkExists(t *testing.T) {
	home := hookTestEnv(t)
	repo := initHookGitRepo(t, "")
	bin := filepath.Join(home, ".claude", "ai-dev-workflow", "bin", "aidw")

	startCmd := exec.Command(bin, "start", ".")
	startCmd.Dir = repo
	startCmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	if out, err := startCmd.CombinedOutput(); err != nil {
		t.Fatalf("aidw start: %v\n%s", err, out)
	}

	if _, _, code := runHookScript(t, home, repo, "precompact", ""); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	entries, err := os.ReadDir(filepath.Join(repo, ".wip"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one wip dir, got %v (err=%v)", entries, err)
	}
	wipDir := filepath.Join(repo, ".wip", entries[0].Name())

	if _, err := os.Stat(filepath.Join(wipDir, "handoff.md")); err != nil {
		t.Errorf("expected handoff.md to be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wipDir, "progress.log")); err != nil {
		t.Errorf("expected progress.log to be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wipDir, "context-summary.md")); err != nil {
		t.Errorf("expected precompact to trigger a context-summary refresh: %v", err)
	}
}
