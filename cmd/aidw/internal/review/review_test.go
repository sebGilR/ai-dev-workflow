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

func TestReviewBundle_CacheHit(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	bundle1, err := ReviewBundle(dir)
	if err != nil {
		t.Fatalf("ReviewBundle (first): %v", err)
	}

	state, err := wip.EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}
	bundlePath := filepath.Join(state.WipDir, "review-bundle.json")
	info1, err := os.Stat(bundlePath)
	if err != nil {
		t.Fatalf("stat bundle: %v", err)
	}

	bundle2, err := ReviewBundle(dir)
	if err != nil {
		t.Fatalf("ReviewBundle (second): %v", err)
	}

	// Cache hit: same GeneratedAt, file not rewritten.
	if bundle2.GeneratedAt != bundle1.GeneratedAt {
		t.Errorf("GeneratedAt changed on cache hit: %q → %q", bundle1.GeneratedAt, bundle2.GeneratedAt)
	}
	info2, err := os.Stat(bundlePath)
	if err != nil {
		t.Fatalf("stat bundle (second): %v", err)
	}
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Error("review-bundle.json was rewritten on cache hit; expected no write")
	}
}

func TestGeminiReview_NotInstalled(t *testing.T) {
	if _, err := exec.LookPath("gemini"); err == nil {
		t.Skip("gemini is installed; not_installed path cannot be tested")
	}

	dir := t.TempDir()
	initGitRepo(t, dir)

	state, err := wip.EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}
	bundle := &BundleResult{
		Repo:        "test-repo",
		Branch:      "test-branch",
		GeneratedAt: "2026-01-01T00:00:00Z",
		BranchDiff:  "diff --git a/foo.go b/foo.go\n+added line",
	}
	if err := util.WriteJSON(filepath.Join(state.WipDir, "review-bundle.json"), bundle); err != nil {
		t.Fatal(err)
	}

	result, err := GeminiReview(dir, "", 0)
	if err != nil {
		t.Fatalf("GeminiReview returned error: %v", err)
	}
	if result.Status != "not_installed" {
		t.Errorf("expected status=not_installed, got %q", result.Status)
	}
}

// writeFakeBundle is a helper that creates a review-bundle.json with a non-empty diff.
func writeFakeBundle(t *testing.T, wipDir string) {
	t.Helper()
	bundle := &BundleResult{
		Repo:        "test-repo",
		Branch:      "test-branch",
		GeneratedAt: "2026-01-01T00:00:00Z",
		BranchDiff:  "diff --git a/foo.go b/foo.go\n+added line",
		ChangedFiles: []string{"foo.go"},
	}
	if err := util.WriteJSON(filepath.Join(wipDir, "review-bundle.json"), bundle); err != nil {
		t.Fatal(err)
	}
}

func TestAdversarialReview_NoBundleReturnsError(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	_, err := AdversarialReview(dir, "gemini", "", 0)
	if err == nil {
		t.Fatal("expected error when review-bundle.json is missing")
	}
	if !strings.Contains(err.Error(), "review-bundle.json not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAdversarialReview_EmptyDiffSkips(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	state, err := wip.EnsureBranchState(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	emptyBundle := &BundleResult{Repo: "r", Branch: "b", GeneratedAt: "2026-01-01T00:00:00Z"}
	if err := util.WriteJSON(filepath.Join(state.WipDir, "review-bundle.json"), emptyBundle); err != nil {
		t.Fatal(err)
	}

	result, err := AdversarialReview(dir, "gemini", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "skipped" {
		t.Errorf("expected skipped, got %q", result.Status)
	}
}

func TestAdversarialReview_NotInstalled(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	state, err := wip.EnsureBranchState(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	writeFakeBundle(t, state.WipDir)

	// Map each provider name to the binary AdversarialReview will look up.
	// Only test providers that are NOT currently installed to avoid accidental invocations.
	providerBinaries := map[string]string{
		"gemini":  "gemini",
		"copilot": "copilot", // github/copilot-cli: https://github.com/github/copilot-cli
		"codex":   "codex",
	}

	var tested int
	for provider, binary := range providerBinaries {
		if _, lookErr := exec.LookPath(binary); lookErr == nil {
			t.Logf("skipping provider %q: %q is installed", provider, binary)
			continue
		}
		result, runErr := AdversarialReview(dir, provider, "", 10)
		if runErr != nil {
			t.Fatalf("provider %s: unexpected error: %v", provider, runErr)
		}
		if result.Status != "not_installed" {
			t.Errorf("provider %s: expected not_installed, got %q", provider, result.Status)
		}
		if result.Provider != provider {
			t.Errorf("provider %s: result.Provider = %q, want %q", provider, result.Provider, provider)
		}
		tested++
	}

	if tested == 0 {
		t.Skip("all tested providers are installed; cannot exercise not_installed path")
	}
}

func TestAdversarialReview_GeminiReviewBackcompat(t *testing.T) {
	if _, err := exec.LookPath("gemini"); err == nil {
		t.Skip("gemini is installed; skipping not_installed path")
	}

	dir := t.TempDir()
	initGitRepo(t, dir)

	state, err := wip.EnsureBranchState(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	writeFakeBundle(t, state.WipDir)

	// GeminiReview should delegate to AdversarialReview with "gemini" and return not_installed.
	result, err := GeminiReview(dir, "", 0)
	if err != nil {
		t.Fatalf("GeminiReview error: %v", err)
	}
	if result.Status != "not_installed" {
		t.Errorf("expected not_installed, got %q", result.Status)
	}
}

func TestAdversarialReview_ReviewPassesName(t *testing.T) {
	if _, err := exec.LookPath("gemini"); err == nil {
		t.Skip("gemini installed; skipping pass-name test to avoid network call")
	}

	dir := t.TempDir()
	initGitRepo(t, dir)

	state, err := wip.EnsureBranchState(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	writeFakeBundle(t, state.WipDir)

	result, err := AdversarialReview(dir, "gemini", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "not_installed" {
		t.Skipf("expected not_installed for gemini; got %q — skipping pass-name check", result.Status)
	}

	// Verify status.json was NOT updated with a pass for not_installed.
	statusPath := filepath.Join(state.WipDir, "status.json")
	if _, serr := os.Stat(statusPath); serr == nil {
		var st wip.Status
		if rerr := util.ReadJSON(statusPath, &st); rerr == nil {
			for _, p := range st.ReviewPasses {
				if p == "gemini-adversarial" {
					t.Error("gemini-adversarial pass should not be written for not_installed result")
				}
			}
		}
	}
}
