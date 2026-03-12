// Package review implements review-bundle, synthesize-review, and gemini-review.
package review

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/util"
	"aidw/cmd/aidw/internal/wip"
)

const (
	maxDiffBytes             = 50_000
	claudeReviewPlaceholder  = "<!-- Claude should add its own review findings here -->"
	defaultGeminiModel       = "gemini-2.5-pro"
	defaultGeminiTimeoutSecs = 120
)

// BundleResult is the JSON-serialisable review bundle written to review-bundle.json.
type BundleResult struct {
	Repo        string      `json:"repo"`
	RepoPath    string      `json:"repo_path"`
	Branch      string      `json:"branch"`
	GeneratedAt string      `json:"generated_at"`
	DiffSources DiffSources `json:"diff_sources"`
	ChangedFiles []string   `json:"changed_files"`
	Status      string      `json:"status"`
	BranchDiff  string      `json:"branch_diff"`
	Diff        string      `json:"diff"`
	StagedDiff  string      `json:"staged_diff"`
}

// DiffSources describes each diff stream in the bundle.
type DiffSources struct {
	BranchDiff  DiffMeta `json:"branch_diff"`
	WorkingTree DiffMeta `json:"working_tree"`
	Staged      DiffMeta `json:"staged"`
}

// DiffMeta describes a single diff stream.
type DiffMeta struct {
	Base          *string `json:"base"`
	Description   string  `json:"description"`
	Truncated     bool    `json:"truncated"`
	OriginalBytes int     `json:"original_bytes"`
}

// ReviewBundle builds a diff bundle for the repo at repoPath and writes
// review-bundle.json into the WIP directory. Returns the bundle.
func ReviewBundle(repoPath string) (*BundleResult, error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return nil, fmt.Errorf("git toplevel: %w", err)
	}
	branch, err := git.CurrentBranch(top)
	if err != nil {
		return nil, fmt.Errorf("current branch: %w", err)
	}

	mergeBase := findMergeBase(top)

	// Branch diff (all committed changes since divergence)
	var branchDiff string
	var branchTruncated bool
	var branchOrigBytes int
	var branchBase *string
	if mergeBase != "" {
		raw := gitOutput(top, "diff", mergeBase, "HEAD", "--")
		branchOrigBytes = len([]byte(raw))
		branchDiff, branchTruncated = util.TruncateDiff(raw, maxDiffBytes)
		branchBase = &mergeBase
	}

	// Working-tree (unstaged) changes
	rawDiff := gitOutput(top, "diff", "--", ".")
	diff, diffTruncated := util.TruncateDiff(rawDiff, maxDiffBytes)

	// Staged changes
	rawStaged := gitOutput(top, "diff", "--cached", "--", ".")
	stagedDiff, stagedTruncated := util.TruncateDiff(rawStaged, maxDiffBytes)

	status := gitOutput(top, "status", "--short")
	changedFiles := parseChangedFiles(status)

	branchDiffDesc := "unavailable (no merge base found)"
	if mergeBase != "" {
		short := mergeBase
		if len(short) > 10 {
			short = short[:10]
		}
		branchDiffDesc = fmt.Sprintf("git diff %s..HEAD", short)
	}

	bundle := &BundleResult{
		Repo:        repoName(top),
		RepoPath:    top,
		Branch:      branch,
		GeneratedAt: util.NowISO(),
		DiffSources: DiffSources{
			BranchDiff: DiffMeta{
				Base:          branchBase,
				Description:   branchDiffDesc,
				Truncated:     branchTruncated,
				OriginalBytes: branchOrigBytes,
			},
			WorkingTree: DiffMeta{
				Description:   "git diff -- .",
				Truncated:     diffTruncated,
				OriginalBytes: len([]byte(rawDiff)),
			},
			Staged: DiffMeta{
				Description:   "git diff --cached -- .",
				Truncated:     stagedTruncated,
				OriginalBytes: len([]byte(rawStaged)),
			},
		},
		ChangedFiles: changedFiles,
		Status:       status,
		BranchDiff:   branchDiff,
		Diff:         diff,
		StagedDiff:   stagedDiff,
	}

	state, err := wip.EnsureBranchState(top, "")
	if err != nil {
		return nil, fmt.Errorf("ensure branch state: %w", err)
	}
	outPath := state.WipDir + "/review-bundle.json"
	if err := util.WriteJSON(outPath, bundle); err != nil {
		return nil, fmt.Errorf("write review-bundle.json: %w", err)
	}
	return bundle, nil
}

// SynthesizeResult is returned by SynthesizeReview.
type SynthesizeResult struct {
	ReviewPath  string `json:"review_path"`
	Synthesized bool   `json:"synthesized"`
}

// SynthesizeReview merges review sources into review.md, preserving existing
// Claude-written content across re-runs.
func SynthesizeReview(repoPath string) (*SynthesizeResult, error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return nil, fmt.Errorf("git toplevel: %w", err)
	}
	state, err := wip.EnsureBranchState(top, "")
	if err != nil {
		return nil, fmt.Errorf("ensure branch state: %w", err)
	}
	wipDir := state.WipDir

	var sections []string
	sections = append(sections, "# Review\n")

	// Append changed files list from bundle
	bundlePath := wipDir + "/review-bundle.json"
	if _, serr := os.Stat(bundlePath); serr == nil {
		var bundle BundleResult
		if rerr := util.ReadJSON(bundlePath, &bundle); rerr == nil && len(bundle.ChangedFiles) > 0 {
			sections = append(sections, "## Changed Files\n")
			for _, f := range bundle.ChangedFiles {
				sections = append(sections, "- "+f)
			}
			sections = append(sections, "")
		}
	}

	// Preserve existing Claude and Adversarial review sections
	reviewPath := wipDir + "/review.md"
	existingClaude, existingAdversarial := extractExistingSections(reviewPath)

	sections = append(sections, "## Claude Review\n")
	if existingClaude != "" {
		sections = append(sections, existingClaude+"\n")
	} else {
		sections = append(sections, claudeReviewPlaceholder+"\n")
	}

	adversarialPath := wipDir + "/adversarial-review.md"
	if data, rerr := os.ReadFile(adversarialPath); rerr == nil {
		adv := strings.TrimSpace(string(data))
		if adv != "" {
			sections = append(sections, "## Adversarial Review\n")
			sections = append(sections, adv+"\n")
		}
	} else if existingAdversarial != "" {
		sections = append(sections, "## Adversarial Review\n")
		sections = append(sections, existingAdversarial+"\n")
	}

	content := strings.Join(sections, "\n") + "\n"
	if err := util.AtomicWrite(reviewPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write review.md: %w", err)
	}

	// Auto-regenerate context-summary if it exists
	if _, serr := os.Stat(wipDir + "/context-summary.md"); serr == nil {
		_, _ = wip.WriteContextSummary(top)
	}

	return &SynthesizeResult{ReviewPath: reviewPath, Synthesized: true}, nil
}

// GeminiReviewResult is returned by GeminiReview.
type GeminiReviewResult struct {
	Status     string `json:"status"`
	Model      string `json:"model,omitempty"`
	ReturnCode int    `json:"returncode,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// GeminiReview runs an adversarial Gemini review pass, writing adversarial-review.md.
func GeminiReview(repoPath, model string, timeoutSecs int) (*GeminiReviewResult, error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return nil, fmt.Errorf("git toplevel: %w", err)
	}
	state, err := wip.EnsureBranchState(top, "")
	if err != nil {
		return nil, fmt.Errorf("ensure branch state: %w", err)
	}
	wipDir := state.WipDir

	bundlePath := wipDir + "/review-bundle.json"
	if _, serr := os.Stat(bundlePath); serr != nil {
		return nil, fmt.Errorf("review-bundle.json not found — run `aidw review-bundle .` first")
	}

	var bundle BundleResult
	if rerr := util.ReadJSON(bundlePath, &bundle); rerr != nil {
		return nil, fmt.Errorf("read review-bundle.json: %w", rerr)
	}

	diffText := bundle.BranchDiff
	if diffText == "" {
		diffText = bundle.Diff
	}
	if diffText == "" {
		diffText = bundle.StagedDiff
	}

	adversarialPath := wipDir + "/adversarial-review.md"

	if strings.TrimSpace(diffText) == "" {
		fmt.Fprintln(os.Stderr, "[aidw] No diff available for adversarial review.")
		_ = os.Remove(adversarialPath)
		return &GeminiReviewResult{Status: "skipped", Reason: "empty diff"}, nil
	}

	fileListStr := "  (unknown)"
	if len(bundle.ChangedFiles) > 0 {
		var lines []string
		for _, f := range bundle.ChangedFiles {
			lines = append(lines, "  - "+f)
		}
		fileListStr = strings.Join(lines, "\n")
	}

	prompt := "You are an adversarial code reviewer. Your job is to find bugs, security issues, " +
		"logic errors, edge cases, and design weaknesses that a friendly reviewer might miss.\n\n" +
		"Be critical and direct. Focus on HIGH and CRITICAL issues only — skip nitpicks.\n\n" +
		"Changed files:\n" + fileListStr + "\n\n" +
		"The full diff is provided via stdin. Review it thoroughly and report your findings."

	if model == "" {
		model = defaultGeminiModel
	}
	if timeoutSecs <= 0 {
		timeoutSecs = defaultGeminiTimeoutSecs
	}
	if timeoutSecs < 10 {
		timeoutSecs = 10
	}
	if timeoutSecs > 600 {
		timeoutSecs = 600
	}

	geminiPath, lookErr := exec.LookPath("gemini")
	if lookErr != nil {
		return &GeminiReviewResult{Status: "not_installed"}, nil
	}

	cmd := exec.Command(geminiPath, "--prompt", prompt, "--model", model, "--output-format", "text")
	cmd.Stdin = strings.NewReader(diffText)

	// Apply timeout via context
	output, exitCode, runErr := runWithTimeout(cmd, timeoutSecs)
	if runErr != nil {
		if strings.Contains(runErr.Error(), "timeout") {
			fmt.Fprintf(os.Stderr, "[aidw] Gemini adversarial review timed out after %ds.\n", timeoutSecs)
			return &GeminiReviewResult{Status: "timeout", Model: model}, nil
		}
		return nil, runErr
	}

	if exitCode != 0 {
		fmt.Fprintf(os.Stderr, "[aidw] Gemini adversarial review failed (exit %d):\n%s\n", exitCode, output.stderr)
		return &GeminiReviewResult{Status: "error", Model: model, ReturnCode: exitCode, Stderr: output.stderr}, nil
	}

	out := strings.TrimSpace(output.stdout)
	if out == "" {
		fmt.Fprintln(os.Stderr, "[aidw] Gemini returned empty output.")
		_ = os.Remove(adversarialPath)
		return &GeminiReviewResult{Status: "empty", Model: model}, nil
	}

	if werr := util.AtomicWrite(adversarialPath, []byte(out+"\n"), 0o644); werr != nil {
		return nil, fmt.Errorf("write adversarial-review.md: %w", werr)
	}

	// Update status.json review_passes
	statusPath := wipDir + "/status.json"
	if _, serr := os.Stat(statusPath); serr == nil {
		var st map[string]any
		if rerr := util.ReadJSON(statusPath, &st); rerr == nil {
			passes, _ := st["review_passes"].([]any)
			hasGemini := false
			for _, p := range passes {
				if p == "gemini-adversarial" {
					hasGemini = true
					break
				}
			}
			if !hasGemini {
				passes = append(passes, "gemini-adversarial")
				st["review_passes"] = passes
			}
			st["updated_at"] = util.NowISO()
			_ = util.WriteJSON(statusPath, st)
		}
	}

	return &GeminiReviewResult{Status: "ok", Model: model}, nil
}

// --- helpers ---

func findMergeBase(repoPath string) string {
	for _, base := range []string{"main", "master"} {
		out := strings.TrimSpace(gitOutput(repoPath, "merge-base", base, "HEAD"))
		if out != "" {
			return out
		}
	}
	return ""
}

func gitOutput(repoPath string, args ...string) string {
	cmdArgs := append([]string{"-C", repoPath}, args...)
	out, err := exec.Command("git", cmdArgs...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func parseChangedFiles(status string) []string {
	var files []string
	for _, line := range strings.Split(status, "\n") {
		if len(line) > 3 {
			files = append(files, line[3:])
		}
	}
	return files
}

func repoName(repoPath string) string {
	parts := strings.Split(strings.TrimRight(repoPath, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

var (
	reClaude      = regexp.MustCompile(`(?m)^## Claude Review\s*$`)
	reAdversarial = regexp.MustCompile(`(?m)^## Adversarial Review\s*$`)
	reNextHeading = regexp.MustCompile(`(?m)^## `)
)

// extractExistingSections extracts the Claude Review and Adversarial Review
// sections from an existing review.md, preserving human-written content.
func extractExistingSections(reviewPath string) (claude, adversarial string) {
	data, err := os.ReadFile(reviewPath)
	if err != nil {
		return "", ""
	}
	text := string(data)

	claude = extractSection(text, reClaude)
	if strings.TrimSpace(claude) == strings.TrimSpace(claudeReviewPlaceholder) {
		claude = ""
	}
	adversarial = extractSection(text, reAdversarial)
	return
}

func extractSection(text string, heading *regexp.Regexp) string {
	m := heading.FindStringIndex(text)
	if m == nil {
		return ""
	}
	after := strings.TrimLeft(text[m[1]:], "\n")
	if next := reNextHeading.FindStringIndex(after); next != nil {
		return strings.TrimRight(after[:next[0]], "\n")
	}
	return strings.TrimRight(after, "\n")
}

type cmdOutput struct {
	stdout string
	stderr string
}

func runWithTimeout(cmd *exec.Cmd, timeoutSecs int) (cmdOutput, int, error) {
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return cmdOutput{}, -1, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timeout := time.Duration(timeoutSecs) * time.Second

	select {
	case err := <-done:
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				return cmdOutput{}, -1, err
			}
		}
		return cmdOutput{stdout: outBuf.String(), stderr: errBuf.String()}, exitCode, nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return cmdOutput{}, -1, fmt.Errorf("timeout after %ds", timeoutSecs)
	}
}
