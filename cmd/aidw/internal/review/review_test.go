package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aidw/cmd/aidw/internal/util"
	"aidw/cmd/aidw/internal/wip"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("git setup %v: %v\n%s", args, err, out)
		}
	}
}

// --- unit tests (no FS/git) ---

func TestParseChangedFiles(t *testing.T) {
	files := parseChangedFiles(" M foo/bar.go\n?? baz.txt\n")
	if len(files) != 2 || files[0] != "foo/bar.go" || files[1] != "baz.txt" {
		t.Errorf("unexpected files: %v", files)
	}

	empty := parseChangedFiles("")
	if len(empty) != 0 {
		t.Errorf("expected empty, got %v", empty)
	}

	short := parseChangedFiles("X\n")
	if len(short) != 0 {
		t.Errorf("expected empty for short lines, got %v", short)
	}
}

func TestRepoName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/home/user/my-project", "my-project"},
		{"/home/user/my-project/", "my-project"},
		{"", ""},
	}
	for _, c := range cases {
		got := repoName(c.in)
		if got != c.want {
			t.Errorf("repoName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractSection(t *testing.T) {
	text := "## Claude Review\n\nsome content\n\n## Adversarial Review\n\nadv\n"

	claude := extractSection(text, reClaude)
	if claude != "some content" {
		t.Errorf("claude section = %q, want %q", claude, "some content")
	}

	adversarial := extractSection(text, reAdversarial)
	if adversarial != "adv" {
		t.Errorf("adversarial section = %q, want %q", adversarial, "adv")
	}

	// No next heading — content at end
	textEnd := "## Claude Review\n\ncontent at end"
	end := extractSection(textEnd, reClaude)
	if end != "content at end" {
		t.Errorf("end section = %q, want %q", end, "content at end")
	}

	// No match
	noMatch := extractSection("no headings here", reClaude)
	if noMatch != "" {
		t.Errorf("expected empty for no match, got %q", noMatch)
	}
}

func TestExtractExistingSections(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.md")

	// Missing file
	c, a := extractExistingSections(reviewPath)
	if c != "" || a != "" {
		t.Errorf("missing file: expected empty, got claude=%q adversarial=%q", c, a)
	}

	// Placeholder treated as empty
	placeholder := "## Claude Review\n\n" + claudeReviewPlaceholder
	if err := os.WriteFile(reviewPath, []byte(placeholder), 0o644); err != nil {
		t.Fatal(err)
	}
	c, a = extractExistingSections(reviewPath)
	if c != "" {
		t.Errorf("placeholder should yield empty claude, got %q", c)
	}
	_ = a

	// Real content preserved
	real := "## Claude Review\n\nReal finding here.\n\n## Adversarial Review\n\nadv content"
	if err := os.WriteFile(reviewPath, []byte(real), 0o644); err != nil {
		t.Fatal(err)
	}
	c, a = extractExistingSections(reviewPath)
	if c != "Real finding here." {
		t.Errorf("claude = %q, want %q", c, "Real finding here.")
	}
	if a != "adv content" {
		t.Errorf("adversarial = %q, want %q", a, "adv content")
	}
}

// --- integration tests (temp git repo) ---

func TestSynthesizeReview_NoBundleOrAdversarial(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	result, err := SynthesizeReview(dir)
	if err != nil {
		t.Fatalf("SynthesizeReview: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}

	data, err := os.ReadFile(result.ReviewPath)
	if err != nil {
		t.Fatalf("read review.md: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "# Review") {
		t.Error("missing '# Review' heading")
	}
	if !strings.Contains(content, "## Claude Review") {
		t.Error("missing '## Claude Review' heading")
	}
	if !strings.Contains(content, claudeReviewPlaceholder) {
		t.Error("missing placeholder")
	}
	if strings.Contains(content, "## Adversarial Review") {
		t.Error("should not contain '## Adversarial Review' when no adversarial file")
	}
}

func TestSynthesizeReview_PreservesClaudeContent(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// First call seeds review.md
	result, err := SynthesizeReview(dir)
	if err != nil {
		t.Fatalf("SynthesizeReview (seed): %v", err)
	}

	// Overwrite with real Claude content
	real := "# Review\n\n## Claude Review\n\nThis is my analysis.\n"
	if err := os.WriteFile(result.ReviewPath, []byte(real), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second call should preserve the content
	result2, err := SynthesizeReview(dir)
	if err != nil {
		t.Fatalf("SynthesizeReview (preserve): %v", err)
	}

	data, err := os.ReadFile(result2.ReviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "This is my analysis.") {
		t.Errorf("claude content not preserved; got:\n%s", string(data))
	}
}

func TestSynthesizeReview_IncludesAdversarialFile(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Seed the wip dir via first call
	result, err := SynthesizeReview(dir)
	if err != nil {
		t.Fatalf("SynthesizeReview (seed): %v", err)
	}
	wipDir := filepath.Dir(result.ReviewPath)

	// Write adversarial file
	advPath := filepath.Join(wipDir, "adversarial-review.md")
	if err := os.WriteFile(advPath, []byte("Critical bug found."), 0o644); err != nil {
		t.Fatal(err)
	}

	result2, err := SynthesizeReview(dir)
	if err != nil {
		t.Fatalf("SynthesizeReview (with adversarial): %v", err)
	}

	data, _ := os.ReadFile(result2.ReviewPath)
	content := string(data)
	if !strings.Contains(content, "## Adversarial Review") {
		t.Error("missing '## Adversarial Review' heading")
	}
	if !strings.Contains(content, "Critical bug found.") {
		t.Error("missing adversarial content")
	}
}

func TestSynthesizeReview_IncludesChangedFiles(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Seed the wip dir via first call
	result, err := SynthesizeReview(dir)
	if err != nil {
		t.Fatalf("SynthesizeReview (seed): %v", err)
	}
	wipDir := filepath.Dir(result.ReviewPath)

	// Write review-bundle.json with changed files
	bundle := &BundleResult{
		Repo:         "test-repo",
		Branch:       "test-branch",
		GeneratedAt:  "2026-01-01T00:00:00Z",
		ChangedFiles: []string{"foo.go", "bar.go"},
	}
	bundlePath := filepath.Join(wipDir, "review-bundle.json")
	if err := util.WriteJSON(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	result2, err := SynthesizeReview(dir)
	if err != nil {
		t.Fatalf("SynthesizeReview (with bundle): %v", err)
	}

	data, _ := os.ReadFile(result2.ReviewPath)
	content := string(data)
	if !strings.Contains(content, "## Changed Files") {
		t.Error("missing '## Changed Files' heading")
	}
	if !strings.Contains(content, "- foo.go") {
		t.Error("missing foo.go")
	}
	if !strings.Contains(content, "- bar.go") {
		t.Error("missing bar.go")
	}
}

func TestReviewBundle_BasicStructure(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	bundle, err := ReviewBundle(dir)
	if err != nil {
		t.Fatalf("ReviewBundle: %v", err)
	}
	if bundle.Repo == "" {
		t.Error("Repo is empty")
	}
	if bundle.Branch == "" {
		t.Error("Branch is empty")
	}
	if bundle.GeneratedAt == "" {
		t.Error("GeneratedAt is empty")
	}

	// review-bundle.json should exist in wip dir
	state, err := wip.EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}
	bundlePath := filepath.Join(state.WipDir, "review-bundle.json")
	if _, err := os.Stat(bundlePath); err != nil {
		t.Errorf("review-bundle.json not written: %v", err)
	}
}

func TestGeminiReview_NotInstalled(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Ensure branch state and write a fake bundle with non-empty diff
	state, err := wip.EnsureBranchState(dir, "test-branch")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}
	bundle := &BundleResult{
		Repo:        "test-repo",
		Branch:      "test-branch",
		GeneratedAt: "2026-01-01T00:00:00Z",
		BranchDiff:  "diff --git a/foo.go b/foo.go\n+added line",
	}
	bundlePath := filepath.Join(state.WipDir, "review-bundle.json")
	if err := util.WriteJSON(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	// Set PATH to only include git's directory, excluding gemini
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found, skipping")
	}
	if _, err2 := exec.LookPath("gemini"); err2 == nil {
		t.Skip("gemini is installed; not_installed path cannot be tested")
	}
	t.Setenv("PATH", filepath.Dir(gitBin))

	result, err := GeminiReview(dir, "", 0)
	if err != nil {
		t.Fatalf("GeminiReview returned error: %v", err)
	}
	if result.Status != "not_installed" {
		t.Errorf("expected status=not_installed, got %q", result.Status)
	}
}
