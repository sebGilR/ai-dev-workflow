package install

import (
	"errors"
	"fmt"
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
// findRepoRoot is the pure form of repoRoot: it returns an error instead of
// calling t.Fatal, so it is safe to call from inside a sync.Once closure.
// (t.Fatal triggers runtime.Goexit, and sync.Once marks itself done anyway —
// so a Fatal inside Do would leave every later caller with a silently
// zero-valued result instead of a failure.)
func findRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not locate repo root (no go.mod found)")
		}
		dir = parent
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

var (
	testBinOnce sync.Once
	testBinPath string
	testBinErr  error
)

// TestMain builds the aidw CLI once per test binary run into a private temp
// directory and removes it afterwards, so concurrent `go test` runs (and
// stale binaries from earlier runs) can't race over a shared fixed path.
func TestMain(m *testing.M) {
	code := m.Run()
	if testBinPath != "" {
		_ = os.RemoveAll(filepath.Dir(testBinPath))
	}
	os.Exit(code)
}

// builtAidwBinary builds the aidw CLI once per test run and returns its
// path, so hook-script tests can exercise `aidw resolve-wip` /
// `aidw summarize-context` end-to-end instead of stubbing them out.
func builtAidwBinary(t *testing.T) string {
	t.Helper()
	testBinOnce.Do(func() {
		root, err := findRepoRoot()
		if err != nil {
			testBinErr = err
			return
		}
		// A private temp dir (not t.TempDir(), which is per-test and
		// gets cleaned up under us) since sync.Once only runs once for
		// the whole test binary run. TestMain removes it at the end.
		dir, err := os.MkdirTemp("", "aidw-hooks-test-bin-")
		if err != nil {
			testBinErr = err
			return
		}
		out := filepath.Join(dir, "aidw")
		cmd := exec.Command("go", "build", "-o", out, "./cmd/aidw")
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			testBinErr = fmt.Errorf("%w\n%s", err, data)
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

// TestHookFallsBackToShellResolver_WhenBinaryLacksResolveWip guards against
// exactly the failure mode an old (pre-B1b) `aidw` binary would hit: no
// `resolve-wip` subcommand, so cobra reports "unknown command" and exits
// non-zero. The hook must still recognize this as "binary resolution
// failed" and fall through to the shell resolver — not silently treat an
// empty/garbled answer as "no active work" and skip the snapshot.
//
// The stub fails for EVERY subcommand, so this also covers the work-model
// path's `|| true` safety net end-to-end: `aidw work checkpoint
// --from-hook` exits 1 here and the script must still exit 0 and complete
// the legacy snapshot.
func TestHookFallsBackToShellResolver_WhenBinaryLacksResolveWip(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".claude", "ai-dev-workflow", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stub that mimics cobra's behavior for an unrecognized subcommand:
	// error text on stderr, non-zero exit, nothing on stdout.
	stub := "#!/bin/sh\necho \"Error: unknown command \\\"$1\\\" for \\\"aidw\\\"\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "aidw"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	repo := initHookGitRepo(t, "")
	wipDir := filepath.Join(repo, ".wip", "20260101120000-main")
	if err := os.MkdirAll(wipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wipDir, "status.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runHookScript(t, home, repo, "stop", ""); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	if _, err := os.Stat(filepath.Join(wipDir, "progress.log")); err != nil {
		t.Errorf("expected the shell fallback to still write progress.log when the binary can't resolve-wip: %v", err)
	}
}

// TestHookResolvesRepoFromStdinCwd is the only test that exercises the
// jq/sed `.cwd`-from-stdin-JSON extraction production Claude Code actually
// relies on: every other call site passes empty stdin, so the script falls
// back to $PWD and the extraction branches never run. Here $PWD is an
// unrelated NON-git temp dir — if the JSON cwd were ignored the script
// would exit 0 having written nothing at all.
func TestHookResolvesRepoFromStdinCwd(t *testing.T) {
	home := hookTestEnv(t)
	repo := initHookGitRepo(t, "")
	unrelated := t.TempDir()
	bin := filepath.Join(home, ".claude", "ai-dev-workflow", "bin", "aidw")

	startCmd := exec.Command(bin, "start", ".")
	startCmd.Dir = repo
	startCmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	if out, err := startCmd.CombinedOutput(); err != nil {
		t.Fatalf("aidw start: %v\n%s", err, out)
	}

	stdin := fmt.Sprintf(`{"session_id":"abc123","cwd":%q,"hook_event_name":"Stop"}`, repo)
	if _, _, code := runHookScript(t, home, unrelated, "stop", stdin); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	entries, err := os.ReadDir(filepath.Join(repo, ".wip"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one wip dir, got %v (err=%v)", entries, err)
	}
	wipDir := filepath.Join(repo, ".wip", entries[0].Name())
	if _, err := os.Stat(filepath.Join(wipDir, "progress.log")); err != nil {
		t.Errorf("hook must resolve the repo from the stdin JSON cwd, not $PWD: %v", err)
	}

	// And nothing may have leaked into the unrelated $PWD.
	if leaked, _ := os.ReadDir(unrelated); len(leaked) != 0 {
		t.Errorf("hook wrote into $PWD instead of the JSON cwd: %v", leaked)
	}
}

// TestHookFiresWorkCheckpoint_WithoutLegacyWipDir pins the work-model hook
// integration (G4.1) and, specifically, its placement: the `aidw work
// checkpoint --from-hook` call sits BEFORE the legacy wip_dir resolution, so
// it must fire for a repo that has a work record but no `.wip` directory at
// all (the script exits quietly further down for lack of a wip_dir).
//
// The observable proof that the call actually ran is the session binding the
// checkpoint's step-3 auto-bind writes under the state dir — asserting on
// the record's provenance.updated_at would be worthless here, since
// timestamps are second-granularity.
func TestHookFiresWorkCheckpoint_WithoutLegacyWipDir(t *testing.T) {
	home := hookTestEnv(t)
	repo := initHookGitRepo(t, "")
	bin := filepath.Join(home, ".claude", "ai-dev-workflow", "bin", "aidw")
	// The hook script only ever gets HOME + PATH, so state lands in the
	// default $HOME/.local/state/aidw — already isolated by the temp HOME.
	env := []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	stateDir := filepath.Join(home, ".local", "state", "aidw")

	// `aidw work start` (NOT `aidw start`) — no .wip directory is created.
	startCmd := exec.Command(bin, "work", "start", ".", "--title", "hooked task")
	startCmd.Dir = repo
	startCmd.Env = env
	if out, err := startCmd.CombinedOutput(); err != nil {
		t.Fatalf("aidw work start: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".wip")); !os.IsNotExist(err) {
		t.Fatalf("`work start` must not create .wip (err=%v)", err)
	}

	stdin := fmt.Sprintf(`{"session_id":"hook-sess-1","cwd":%q,"hook_event_name":"Stop"}`, repo)
	if _, stderr, code := runHookScript(t, home, repo, "stop", stdin); code != 0 {
		t.Errorf("expected exit 0, got %d (stderr: %s)", code, stderr)
	}

	binding := filepath.Join(stateDir, "sessions", "hook-sess-1.json")
	if _, err := os.Stat(binding); err != nil {
		t.Fatalf("hook did not fire `work checkpoint --from-hook` (no session binding at %s): %v", binding, err)
	}
	// And it still must not have invented a .wip directory.
	if _, err := os.Stat(filepath.Join(repo, ".wip")); !os.IsNotExist(err) {
		t.Errorf("hook created a .wip directory for a branch that never ran `aidw start` (err=%v)", err)
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
