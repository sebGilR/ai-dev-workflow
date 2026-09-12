package state

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/util"
)

func initGitRepo(t *testing.T) string {
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
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestExecutionRoot_DelegatesToGitToplevel(t *testing.T) {
	dir := initGitRepo(t)

	want, err := git.Toplevel(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExecutionRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ExecutionRoot() = %q, want %q (git.Toplevel)", got, want)
	}
}

func TestStateDir_RespectsEnvOverride(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", "/tmp/custom-aidw-state")
	if got := StateDir(); got != "/tmp/custom-aidw-state" {
		t.Fatalf("StateDir() = %q, want override", got)
	}
}

func TestStateDir_FallsBackToXDG(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	if got := StateDir(); got != filepath.Join("/tmp/xdg-state", "aidw") {
		t.Fatalf("StateDir() = %q, want XDG-based path", got)
	}
}

func TestStateDir_FallsBackToHomeLocalState(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/fake-home")
	want := filepath.Join("/tmp/fake-home", ".local", "state", "aidw")
	if got := StateDir(); got != want {
		t.Fatalf("StateDir() = %q, want %q", got, want)
	}
}

func TestRepoIdentity_SameAcrossWorktrees(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", stateDir)

	repo := initGitRepo(t)
	worktreeDir := filepath.Join(t.TempDir(), "wt")

	cmd := exec.Command("git", "worktree", "add", worktreeDir, "-b", "wt-branch")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	resolvedWorktree, err := filepath.EvalSymlinks(worktreeDir)
	if err != nil {
		t.Fatal(err)
	}

	idMain, err := RepoIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	idWorktree, err := RepoIdentity(resolvedWorktree)
	if err != nil {
		t.Fatal(err)
	}
	if idMain != idWorktree {
		t.Fatalf("RepoIdentity mismatch across worktrees: %q vs %q", idMain, idWorktree)
	}
}

func TestRepoIdentity_DifferentForIndependentRepos(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", stateDir)

	base := t.TempDir()
	repoA := filepath.Join(base, "repo")
	repoB := filepath.Join(base, "other", "repo")
	if err := os.MkdirAll(repoA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repoB, 0o755); err != nil {
		t.Fatal(err)
	}

	initRepoAt := func(dir string) string {
		t.Helper()
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
		if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", ".")
		run("commit", "-m", "init")
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		return resolved
	}

	rA := initRepoAt(repoA)
	rB := initRepoAt(repoB)

	idA, err := RepoIdentity(rA)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := RepoIdentity(rB)
	if err != nil {
		t.Fatal(err)
	}
	if idA == idB {
		t.Fatalf("RepoIdentity should differ for independent repos with same basename, got %q for both", idA)
	}
}

func TestRepoIdentity_NeverWritesReposJSON(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", stateDir)

	repo := initGitRepo(t)
	if _, err := RepoIdentity(repo); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(stateDir, "repos.json")); !os.IsNotExist(err) {
		t.Fatalf("RepoIdentity must not write repos.json, stat err = %v", err)
	}
}

func TestAcquireLock_SecondAcquireFailsImmediately(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "work.json")

	release, err := AcquireLock(target)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if _, err := AcquireLock(target); err == nil {
		t.Fatal("expected second AcquireLock to fail while first is held")
	}
}

func TestAcquireLock_ReleaseAllowsReacquire(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "work.json")

	release, err := AcquireLock(target)
	if err != nil {
		t.Fatal(err)
	}
	release()
	// Double release must be a no-op, not an unlock of a recycled fd.
	release()

	second, err := AcquireLock(target)
	if err != nil {
		t.Fatalf("expected reacquire after release to succeed: %v", err)
	}
	second()

	// The lock file itself is intentionally left on disk: flock guards a
	// file description, and unlinking would reintroduce a two-winner race.
	if _, err := os.Stat(target + ".lock"); err != nil {
		t.Fatalf("lock file should persist after release: %v", err)
	}
}

// TestAcquireLock_ConcurrentGoroutinesExactlyOneWinner is the real race
// test: N goroutines contend for the same lock at once and exactly one may
// win. Note that flock is per open file description, not per process, so
// in-process goroutines contend for real here.
func TestAcquireLock_ConcurrentGoroutinesExactlyOneWinner(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "work.json")

	const n = 16
	type attempt struct {
		release func()
		err     error
	}
	results := make([]attempt, n)

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			release, err := AcquireLock(target)
			// Deliberately do NOT release here — releasing inside the
			// goroutine would let a later goroutine legitimately win too,
			// making "exactly one" nondeterministic.
			results[i] = attempt{release: release, err: err}
		}(i)
	}
	start.Done()
	done.Wait()

	winners := 0
	var winner func()
	for i, r := range results {
		if r.err == nil {
			winners++
			winner = r.release
			continue
		}
		if !strings.Contains(r.err.Error(), "is held") {
			t.Fatalf("attempt %d: loser error should report the lock is held, got %v", i, r.err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly 1 winner out of %d concurrent acquirers, got %d", n, winners)
	}

	winner()
	after, err := AcquireLock(target)
	if err != nil {
		t.Fatalf("expected acquire to succeed after the winner released: %v", err)
	}
	after()
}

// TestAcquireLock_SurvivesKilledHolder proves the crash-safety property
// that replaced staleness-based stealing: a process SIGKILLed while
// holding the lock has it released by the kernel, so the resource is not
// stuck forever.
func TestAcquireLock_SurvivesKilledHolder(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "work.json")

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperHoldsLock", "-test.v")
	cmd.Env = append(os.Environ(), "AIDW_LOCK_HELPER_TARGET="+target)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	// Wait for a readiness line rather than sleeping a fixed interval.
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "LOCK-HELD") {
				ready <- nil
				return
			}
		}
		ready <- fmt.Errorf("helper exited without acquiring the lock: %v", scanner.Err())
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for helper to acquire the lock")
	}

	// While the helper holds it, we must not be able to acquire it.
	if _, err := AcquireLock(target); err == nil {
		t.Fatal("expected acquire to fail while a live subprocess holds the lock")
	}

	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()

	// The kernel drops the flock when the holder dies. Poll briefly: the
	// fd teardown is not necessarily complete the instant wait() returns.
	deadline := time.Now().Add(10 * time.Second)
	for {
		release, err := AcquireLock(target)
		if err == nil {
			release()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock still held after the holding process was SIGKILLed: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestHelperHoldsLock is not a real test: it is the subprocess body for
// TestAcquireLock_SurvivesKilledHolder, guarded by an env var so it is a
// no-op during a normal test run.
func TestHelperHoldsLock(t *testing.T) {
	target := os.Getenv("AIDW_LOCK_HELPER_TARGET")
	if target == "" {
		t.Skip("helper process only")
	}
	if _, err := AcquireLock(target); err != nil {
		t.Fatalf("helper failed to acquire lock: %v", err)
	}
	fmt.Println("LOCK-HELD")
	// Hold it (without ever releasing) until the parent kills us.
	time.Sleep(2 * time.Minute)
}

func TestRegisterRepo_ConcurrentUnrelatedReposAllSucceed(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", stateDir)

	const n = 8
	repos := make([]string, n)
	for i := 0; i < n; i++ {
		repos[i] = initGitRepo(t)
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = RegisterRepo(stateDir, repos[i])
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("RegisterRepo for repo %d failed under concurrency: %v", i, err)
		}
	}

	// Every registration must be durably present — the retry must not have
	// let a write be lost.
	rf, err := LoadRepos(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if _, ok := rf.Repos[id]; !ok {
			t.Fatalf("repo %d (%s) missing from repos.json after concurrent registration", i, id)
		}
	}
}

func TestRegisterRepo_UsesGivenStateDirNotGlobal(t *testing.T) {
	globalDir := t.TempDir()
	otherDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", globalDir)

	repo := initGitRepo(t)
	if _, err := RegisterRepo(otherDir, repo); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(otherDir, "repos.json")); err != nil {
		t.Fatalf("RegisterRepo should write repos.json under the stateDir it was given: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "repos.json")); !os.IsNotExist(err) {
		t.Fatalf("RegisterRepo must not touch the global state dir, stat err = %v", err)
	}
}

func TestRepoIdentityIn_HonorsExplicitStateDirAlias(t *testing.T) {
	globalDir := t.TempDir()
	aliasDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", globalDir)

	repo := initGitRepo(t)
	canonical, err := canonicalCommonDir(repo)
	if err != nil {
		t.Fatal(err)
	}

	rf := &ReposFile{Repos: map[string]RepoEntry{
		"pinned-id": {Aliases: []string{canonical}},
	}}
	if err := util.WriteJSON(filepath.Join(aliasDir, "repos.json"), rf); err != nil {
		t.Fatal(err)
	}

	got, err := repoIdentityIn(aliasDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != "pinned-id" {
		t.Fatalf("repoIdentityIn(aliasDir) = %q, want the alias-mapped id from that stateDir", got)
	}

	// The global state dir has no such alias, so the derived id differs —
	// proving the explicit stateDir was actually consulted.
	globalID, err := RepoIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	if globalID == "pinned-id" {
		t.Fatal("RepoIdentity should not see the alias registered only in aliasDir")
	}
}

func TestRepoIdentityIn_DuplicateClaimIsAnError(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", stateDir)

	repo := initGitRepo(t)
	canonical, err := canonicalCommonDir(repo)
	if err != nil {
		t.Fatal(err)
	}

	rf := &ReposFile{Repos: map[string]RepoEntry{
		"id-a": {Aliases: []string{canonical}},
		"id-b": {LastKnownPaths: []string{canonical}},
	}}
	if err := util.WriteJSON(filepath.Join(stateDir, "repos.json"), rf); err != nil {
		t.Fatal(err)
	}

	// Repeat: map iteration order is randomized, so a nondeterministic
	// implementation would pass this only sometimes.
	for i := 0; i < 20; i++ {
		if _, err := repoIdentityIn(stateDir, repo); err == nil {
			t.Fatal("expected a duplicate-registration error, got nil")
		}
	}
}

func TestStateDir_MakesRelativeOverrideAbsolute(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", "relative-state")
	got := StateDir()
	if !filepath.IsAbs(got) {
		t.Fatalf("StateDir() = %q, want an absolute path", got)
	}
}

func TestStateDir_AbsoluteWithoutHome(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	got := StateDir()
	if !filepath.IsAbs(got) {
		t.Fatalf("StateDir() with no HOME = %q, want an absolute path", got)
	}
}
