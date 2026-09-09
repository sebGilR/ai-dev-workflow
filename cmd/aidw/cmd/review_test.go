package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (no go.mod found)")
		}
		dir = parent
	}
}

// TestAdversarialDirectInvocationRuns covers AC-D2: with no enablement env var
// set (explicitly "0", the new default), an explicit `aidw adversarial-review`
// invocation still executes the provider. The gate is now the Claude Code
// permission prompt (permissions.ask), not an env var. The provider is a PATH
// stub, so no network call is made.
func TestAdversarialDirectInvocationRuns(t *testing.T) {
	fakeBin := t.TempDir()
	stub := filepath.Join(fakeBin, "gemini")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat >/dev/null\necho 'STUB REVIEW OUTPUT'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Prepend so the stub shadows any real provider but git stays reachable.
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AIDW_ADVERSARIAL_REVIEW", "0")
	t.Setenv("AIDW_GEMINI_REVIEW", "0")

	dir := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git setup %v: %v\n%s", args, err, out)
		}
	}

	// Produce a non-empty working-tree diff so the review actually has content.
	tracked := filepath.Join(dir, "foo.txt")
	if err := os.WriteFile(tracked, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "-C", dir, "add", "foo.txt"},
		{"git", "-C", dir, "commit", "-m", "add foo"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git setup %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(tracked, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build the review bundle first; without it the command would Die() and
	// os.Exit the test binary.
	reviewBundleCmd.Run(reviewBundleCmd, []string{dir})

	// NOTE: these Set calls mutate package-level cobra command state that
	// persists for the rest of the test binary. Harmless today; if more cmd
	// tests are added, reset the flags or build a fresh command instance.
	if err := adversarialReviewCmd.Flags().Set("provider", "gemini"); err != nil {
		t.Fatal(err)
	}
	if err := adversarialReviewCmd.Flags().Set("timeout", "30"); err != nil {
		t.Fatal(err)
	}
	adversarialReviewCmd.Run(adversarialReviewCmd, []string{dir})

	matches, err := filepath.Glob(filepath.Join(dir, ".wip", "*", "adversarial-review.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("adversarial-review.md was not written — the provider did not run")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "STUB REVIEW OUTPUT") {
		t.Errorf("stub provider output missing from adversarial-review.md:\n%s", data)
	}
}

// TestWipReviewSkillHasNoAdversarialOffer covers AC-D1: the wip-review skill
// never runs or offers an external adversarial review. The assertion is
// case-sensitive on the env-var-style tokens; the sanctioned lowercase
// `aidw adversarial-review` mention is allowed.
func TestWipReviewSkillHasNoAdversarialOffer(t *testing.T) {
	path := filepath.Join(repoRoot(t), "claude", "skills", "wip-review", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	content := string(data)

	for _, token := range []string{"ADVERSARIAL", "GEMINI"} {
		if strings.Contains(content, token) {
			t.Errorf("%s must not appear in wip-review/SKILL.md", token)
		}
	}
	for _, offer := range []string{
		"Run adversarial review?",
		"gemini-review",
	} {
		if strings.Contains(content, offer) {
			t.Errorf("wip-review/SKILL.md must not contain %q", offer)
		}
	}
	if !strings.Contains(content, "External adversarial review is never run or offered by this workflow.") {
		t.Error("wip-review/SKILL.md is missing the sanctioned explicit-invocation-only sentence")
	}
}
