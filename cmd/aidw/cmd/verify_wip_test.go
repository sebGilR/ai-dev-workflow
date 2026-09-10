package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The verify-* family is read-only. Invoking a verifier used to seed a full
// nine-file .wip directory via EnsureBranchState and then "verify" the empty
// file it had just created. These commands call os.Exit, so they are exercised
// through the built binary.

// TestVerifyCommands_NoActiveWorkCreatesNothing covers AC-B2: on a repo with
// no .wip, every verify-* command fails with the /wip-start message and
// creates nothing.
func TestVerifyCommands_NoActiveWorkCreatesNothing(t *testing.T) {
	for _, args := range [][]string{
		{"verify-plan", "."},
		{"verify-spec", "."},
		{"verify-task-context", "."},
		{"verify-research", "."},
		{"verify-review", "."},
		{"verify-wip-file", ".", "spec.md"},
	} {
		t.Run(args[0], func(t *testing.T) {
			dir := initTestGitRepo(t)

			_, stderr, code := runAidw(t, dir, args...)

			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, "/wip-start") {
				t.Errorf("stderr must point at /wip-start, got %q", stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, ".wip")); !os.IsNotExist(err) {
				t.Errorf("%s must not create a .wip directory", args[0])
			}
		})
	}
}

// TestVerifyCommands_UseExistingState confirms the switch to the lookup-only
// resolver did not break the normal path: with state present, verification
// still reports on the real file.
func TestVerifyCommands_UseExistingState(t *testing.T) {
	dir := initTestGitRepo(t)
	if _, _, code := runAidw(t, dir, "start", "."); code != 0 {
		t.Fatalf("aidw start exited %d", code)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".wip", "*", "spec.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one seeded spec.md, got %v (%v)", matches, err)
	}
	if err := os.WriteFile(matches[0], []byte("# Spec\n\nreal content here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runAidw(t, dir, "verify-spec", ".")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, `"verified": true`) && !strings.Contains(stdout, `"verified":true`) {
		t.Errorf("expected verified:true in output, got %q", stdout)
	}
}
