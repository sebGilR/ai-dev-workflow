package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// noJqPath builds a scratch directory containing symlinks to every tool
// session-start-context.sh / save-wip-snapshot.sh need (git, sed, head,
// cat, date, find, sort, grep, bash) EXCEPT jq, so tests can exercise the
// no-jq fallback parsing branches even on a machine (like this one) where
// jq is actually installed at /usr/bin/jq. Returns a PATH value containing
// only that directory.
func noJqPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	tools := []string{"git", "sed", "head", "cat", "date", "find", "sort", "grep", "bash"}
	for _, name := range tools {
		src, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("could not locate %s on PATH: %v", name, err)
		}
		if err := os.Symlink(src, filepath.Join(dir, name)); err != nil {
			t.Fatalf("symlink %s: %v", name, err)
		}
	}
	if _, err := exec.LookPath("jq"); err == nil {
		// Sanity: confirm jq genuinely isn't reachable via our scratch PATH.
		if _, statErr := os.Stat(filepath.Join(dir, "jq")); statErr == nil {
			t.Fatal("scratch PATH unexpectedly contains jq")
		}
	}
	return dir
}

func runSessionStartScript(t *testing.T, home, cwd, path, stdin string) (stdout string, exitCode int) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "templates", "global", "scripts", "session-start-context.sh")
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
		t.Fatalf("run session-start-context.sh: %v", err)
	}
	return stdout, exitCode
}

func TestSessionStartHook_NonGit(t *testing.T) {
	home := hookTestEnv(t)
	nonGit := t.TempDir()

	for _, path := range []string{"/usr/bin:/bin", noJqPath(t)} {
		out, code := runSessionStartScript(t, home, nonGit, path, "")
		if code != 0 {
			t.Errorf("path=%q: expected exit 0, got %d", path, code)
		}
		if out != "" {
			t.Errorf("path=%q: expected no output outside a git repo, got %q", path, out)
		}
	}
}

func TestSessionStartHook_NoActiveWork(t *testing.T) {
	home := hookTestEnv(t)
	repo := initHookGitRepo(t, "")

	for _, path := range []string{"/usr/bin:/bin", noJqPath(t)} {
		out, code := runSessionStartScript(t, home, repo, path, "")
		if code != 0 {
			t.Errorf("path=%q: expected exit 0, got %d", path, code)
		}
		if out != "" {
			t.Errorf("path=%q: expected no output with no active work, got %q", path, out)
		}
	}
}

func TestSessionStartHook_FreshSummary_ReportsCurrent(t *testing.T) {
	home := hookTestEnv(t)
	repo := initHookGitRepo(t, "")
	bin := filepath.Join(home, ".claude", "ai-dev-workflow", "bin", "aidw")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = repo
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("aidw %v: %v\n%s", args, err, out)
		}
	}
	run("start", ".")
	run("summarize-context", ".")

	for _, path := range []string{"/usr/bin:/bin", noJqPath(t)} {
		out, code := runSessionStartScript(t, home, repo, path, "")
		if code != 0 {
			t.Errorf("path=%q: expected exit 0, got %d", path, code)
		}
		if !strings.Contains(out, "active work in") {
			t.Errorf("path=%q: expected an active-work line, got %q", path, out)
		}
		if !strings.Contains(out, "is current") {
			t.Errorf("path=%q: expected a fresh summary to report \"current\" (not stale), got %q", path, out)
		}
		if strings.Contains(out, "STALE") {
			t.Errorf("path=%q: fresh summary must not be reported as STALE, got %q", path, out)
		}
	}
}

func TestSessionStartHook_StaleSummary_ReportsStale(t *testing.T) {
	home := hookTestEnv(t)
	repo := initHookGitRepo(t, "")
	bin := filepath.Join(home, ".claude", "ai-dev-workflow", "bin", "aidw")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = repo
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("aidw %v: %v\n%s", args, err, out)
		}
	}
	run("start", ".")
	run("summarize-context", ".")

	// Edit a source file after the summary was generated so its provenance
	// hash no longer matches — this is what "stale" means.
	entries, err := os.ReadDir(filepath.Join(repo, ".wip"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one wip dir, got %v (err=%v)", entries, err)
	}
	wipDir := filepath.Join(repo, ".wip", entries[0].Name())
	if err := os.WriteFile(filepath.Join(wipDir, "research.md"), []byte("new findings"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/usr/bin:/bin", noJqPath(t)} {
		out, code := runSessionStartScript(t, home, repo, path, "")
		if code != 0 {
			t.Errorf("path=%q: expected exit 0, got %d", path, code)
		}
		if !strings.Contains(out, "STALE") {
			t.Errorf("path=%q: expected a stale summary to be reported as STALE, got %q", path, out)
		}
	}
}

func TestSessionStartHook_ActiveWorkNoSummaryYet(t *testing.T) {
	home := hookTestEnv(t)
	repo := initHookGitRepo(t, "")
	bin := filepath.Join(home, ".claude", "ai-dev-workflow", "bin", "aidw")

	startCmd := exec.Command(bin, "start", ".")
	startCmd.Dir = repo
	startCmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	if out, err := startCmd.CombinedOutput(); err != nil {
		t.Fatalf("aidw start: %v\n%s", err, out)
	}

	for _, path := range []string{"/usr/bin:/bin", noJqPath(t)} {
		out, code := runSessionStartScript(t, home, repo, path, "")
		if code != 0 {
			t.Errorf("path=%q: expected exit 0, got %d", path, code)
		}
		if !strings.Contains(out, "active work in") {
			t.Errorf("path=%q: expected an active-work line even with no summary yet, got %q", path, out)
		}
		if strings.Contains(out, "current") || strings.Contains(out, "STALE") {
			t.Errorf("path=%q: expected no staleness line before any summary exists, got %q", path, out)
		}
	}
}
