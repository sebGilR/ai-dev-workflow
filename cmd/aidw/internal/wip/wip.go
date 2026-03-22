// Package wip manages branch-scoped workflow state (.wip directories).
package wip

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/slug"
	"aidw/cmd/aidw/internal/util"
)

// wipFiles is the list of standard WIP workflow files.
var wipFiles = []string{"plan.md", "review.md", "research.md", "context.md", "execution.md", "pr.md"}

// keepOnCleanup is the set of files to keep during cleanup-branch.
var keepOnCleanup = map[string]bool{"context.md": true, "pr.md": true}

// stages is the set of valid workflow stages.
var stages = map[string]bool{
	"started":      true,
	"planned":      true,
	"researched":   true,
	"implementing": true,
	"reviewed":     true,
	"review-fixed": true,
	"pr-prep":      true,
}

// BranchState contains the resolved state for a branch's WIP directory.
type BranchState struct {
	Repo   string         `json:"repo"`
	Branch string         `json:"branch"`
	WipDir string         `json:"wip_dir"`
	Status map[string]any `json:"status"`
}

// EnsureBranchState ensures the .wip/<branch> directory exists and is properly initialized.
// If branch is empty, the current branch is used.
func EnsureBranchState(repoPath, branch string) (*BranchState, error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return nil, fmt.Errorf("not a git repo: %w", err)
	}

	// Ensure repo is bootstrapped
	if _, err := EnsureRepo(top); err != nil {
		return nil, err
	}

	branchName := branch
	if branchName == "" {
		b, err := git.CurrentBranch(top)
		if err != nil {
			return nil, fmt.Errorf("get current branch: %w", err)
		}
		branchName = b
	}
	branchName = slug.SafeSlug(branchName)

	wipBase := filepath.Join(top, ".wip")
	if err := os.MkdirAll(wipBase, 0o755); err != nil {
		return nil, err
	}

	// Phase 1: find existing dated dir (YYYYMMDD-<branch_name>); pick the newest
	// Note: check-then-create is not atomic; concurrent invocations for the same branch
	// could both see no existing dir and each create one. In practice this tool runs once
	// per CLI invocation so the race is not reachable under normal use.
	datePattern := regexp.MustCompile(`^(\d{8})-` + regexp.QuoteMeta(branchName) + `$`)
	var candidates []string
	entries, err := os.ReadDir(wipBase)
	if err != nil {
		return nil, fmt.Errorf("read wip directory: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := datePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if _, err := time.Parse("20060102", m[1]); err != nil {
			continue
		}
		candidates = append(candidates, e.Name())
	}
	sort.Strings(candidates)

	var wipDir string
	if len(candidates) > 0 {
		wipDir = filepath.Join(wipBase, candidates[len(candidates)-1])
	}

	// Phase 2: legacy unprefixed dir
	if wipDir == "" {
		legacy := filepath.Join(wipBase, branchName)
		if info, err := os.Stat(legacy); err == nil && info.IsDir() {
			wipDir = legacy
		}
	}

	// Phase 3: create new dated dir
	if wipDir == "" {
		datePrefix := time.Now().Format("20060102")
		wipDir = filepath.Join(wipBase, datePrefix+"-"+branchName)
	}

	if err := os.MkdirAll(wipDir, 0o755); err != nil {
		return nil, err
	}

	// Seed WIP files
	for _, filename := range wipFiles {
		name := strings.ReplaceAll(strings.TrimSuffix(filename, ".md"), "-", " ")
		if err := seedFileIfMissing(filepath.Join(wipDir, filename),
			fmt.Sprintf("# %s\n\n", titleCase(name))); err != nil {
			return nil, fmt.Errorf("seed %s: %w", filename, err)
		}
	}

	// Load or create status.json
	statusPath := filepath.Join(wipDir, "status.json")
	var status map[string]any
	if _, err := os.Stat(statusPath); os.IsNotExist(err) {
		status = initialStatus(filepath.Base(top), top, branchName)
		if err := util.WriteJSON(statusPath, status); err != nil {
			return nil, err
		}
	} else {
		if err := util.ReadJSON(statusPath, &status); err != nil {
			return nil, err
		}
		changed := false
		if status["repo"] != filepath.Base(top) {
			status["repo"] = filepath.Base(top)
			changed = true
		}
		if status["repo_path"] != top {
			status["repo_path"] = top
			changed = true
		}
		if status["branch"] != branchName {
			status["branch"] = branchName
			changed = true
		}
		if changed {
			status["updated_at"] = util.NowISO()
			if err := util.WriteJSON(statusPath, status); err != nil {
				return nil, err
			}
		}
	}

	// Update context.md if empty or placeholder
	contextPath := filepath.Join(wipDir, "context.md")
	data, _ := os.ReadFile(contextPath)
	existing := strings.TrimSpace(string(data))
	if existing == "# Context" || existing == "" || strings.Contains(existing, "- Repo: ``") {
		content := fmt.Sprintf(`# Context

- Repo: `+"`%s`"+`
- Repo path: `+"`%s`"+`
- Branch: `+"`%s`"+`
- Initialized at: `+"`%s`"+`
- Current status file: `+"`status.json`"+`
`, filepath.Base(top), top, branchName, util.NowISO())
		if err := util.AtomicWrite(contextPath, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("write context.md: %w", err)
		}
	}

	return &BranchState{
		Repo:   top,
		Branch: branchName,
		WipDir: wipDir,
		Status: status,
	}, nil
}

// VerifyWipFile checks that a WIP file exists and has meaningful content.
// Returns (ok, errorMessage).
func VerifyWipFile(wipDir, filename string) (bool, string) {
	path := filepath.Join(wipDir, filename)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, fmt.Sprintf("%s does not exist", filename)
	}
	if err != nil {
		return false, fmt.Sprintf("%s stat error: %v", filename, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Sprintf("%s is not a regular file", filename)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Sprintf("%s is not readable: %v", filename, err)
	}
	if len(strings.TrimSpace(string(data))) < 10 {
		return false, fmt.Sprintf("%s is too small or empty (< 10 characters of content)", filename)
	}
	return true, ""
}

// SetStageResult is returned by SetStage.
type SetStageResult map[string]any

// SetStage transitions the workflow to a new stage.
func SetStage(repoPath, stage string, skipVerification bool) (SetStageResult, error) {
	if !stages[stage] {
		return nil, fmt.Errorf("unsupported stage: %s. Allowed: %v", stage, sortedStages())
	}

	state, err := EnsureBranchState(repoPath, "")
	if err != nil {
		return nil, err
	}

	// Verify required files for specific stages
	if !skipVerification {
		var requiredFile string
		switch stage {
		case "planned":
			requiredFile = "plan.md"
		case "researched":
			requiredFile = "research.md"
		case "reviewed":
			requiredFile = "review.md"
		}
		if requiredFile != "" {
			ok, errMsg := VerifyWipFile(state.WipDir, requiredFile)
			if !ok {
				return nil, fmt.Errorf("cannot transition to stage '%s': %s\nHint: Ensure %s exists and has content before setting this stage.\n      Use --skip-verification to bypass this check (not recommended).", stage, errMsg, requiredFile)
			}
		}
	}

	statusPath := filepath.Join(state.WipDir, "status.json")
	var status map[string]any
	if err := util.ReadJSON(statusPath, &status); err != nil {
		return nil, err
	}

	status["stage"] = stage
	status["updated_at"] = util.NowISO()
	status["last_completed_step"] = stage

	if err := util.WriteJSON(statusPath, status); err != nil {
		return nil, err
	}

	// Auto-regenerate context-summary if it exists. This is best-effort: a failure
	// here does not roll back the stage transition — the summary will be stale but
	// the caller can re-run `aidw summarize-context` to regenerate it.
	summaryPath := filepath.Join(state.WipDir, "context-summary.md")
	if _, err := os.Stat(summaryPath); err == nil {
		_, _ = WriteContextSummary(repoPath)
	}

	return status, nil
}

// ContextSummaryResult is returned by WriteContextSummary.
type ContextSummaryResult struct {
	SummaryPath string `json:"summary_path"`
	SizeBytes   int    `json:"size_bytes"`
	Branch      string `json:"branch"`
}

// WriteContextSummary generates context-summary.md from WIP files.
func WriteContextSummary(repoPath string) (*ContextSummaryResult, error) {
	state, err := EnsureBranchState(repoPath, "")
	if err != nil {
		return nil, err
	}

	files := collectContextFiles(state.WipDir)
	summary := generateSummaryText(files, state.Status)

	summaryPath := filepath.Join(state.WipDir, "context-summary.md")
	if err := util.AtomicWrite(summaryPath, []byte(summary), 0o644); err != nil {
		return nil, err
	}

	branch, _ := state.Status["branch"].(string)
	return &ContextSummaryResult{
		SummaryPath: summaryPath,
		SizeBytes:   len(summary),
		Branch:      branch,
	}, nil
}

// SummarizeStatus returns a human-readable status summary.
func SummarizeStatus(repoPath string) (string, error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return "", err
	}
	state, err := EnsureBranchState(top, "")
	if err != nil {
		return "", err
	}

	firstNonEmpty := func(path string, fallback string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			return fallback
		}
		txt := strings.TrimSpace(string(data))
		if txt == "" {
			return fallback
		}
		return txt
	}

	plan := firstNonEmpty(filepath.Join(state.WipDir, "plan.md"), "")
	execution := firstNonEmpty(filepath.Join(state.WipDir, "execution.md"), "")
	context := firstNonEmpty(filepath.Join(state.WipDir, "context.md"), "")

	branch, _ := state.Status["branch"].(string)
	stage, _ := state.Status["stage"].(string)
	updatedAt, _ := state.Status["updated_at"].(string)

	return fmt.Sprintf(`Repo: %s
Branch: %s
Stage: %s
Updated: %s
WIP directory: %s

Context preview:
%s

Plan preview:
%s

Execution preview:
%s
`, top, branch, stage, updatedAt, state.WipDir, trim(context, 600), trim(plan, 600), trim(execution, 600)), nil
}

// CleanupResult is returned by CleanupBranch.
type CleanupResult struct {
	WipDir  string   `json:"wip_dir"`
	Kept    []string `json:"kept"`
	Deleted []string `json:"deleted"`
}

// CleanupBranch removes all files in the branch's .wip dir except context.md and pr.md.
func CleanupBranch(repoPath string) (*CleanupResult, error) {
	state, err := EnsureBranchState(repoPath, "")
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(state.WipDir)
	if err != nil {
		return nil, err
	}

	var deleted []string
	for _, e := range entries {
		if keepOnCleanup[e.Name()] {
			continue
		}
		path := filepath.Join(state.WipDir, e.Name())
		if e.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return nil, err
			}
		} else {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
		}
		deleted = append(deleted, e.Name())
	}

	sort.Strings(deleted)
	kept := make([]string, 0, len(keepOnCleanup))
	for k := range keepOnCleanup {
		kept = append(kept, k)
	}
	sort.Strings(kept)

	return &CleanupResult{
		WipDir:  state.WipDir,
		Kept:    kept,
		Deleted: deleted,
	}, nil
}

// ClearWipResult is returned by ClearWip.
type ClearWipResult struct {
	Kept    *string  `json:"kept"`
	Deleted []string `json:"deleted"`
}

// MigratedDir describes a single legacy-to-dated rename.
type MigratedDir struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// MigrateWipResult is returned by MigrateWip.
type MigrateWipResult struct {
	WipBase  string        `json:"wip_base"`
	Migrated []MigratedDir `json:"migrated"`
	Warnings []string      `json:"warnings,omitempty"`
}

// MigrateWip renames legacy (un-timestamped) .wip/<slug> directories to the
// YYYYMMDD-<slug> format. This standardises directory names and enables
// date-based cleanup policies. Only directories that contain at least one
// canonical WIP file (status.json, plan.md, context.md, etc.) are considered
// WIP branch dirs and eligible for migration — unrecognised directories are
// left untouched. Directories that already match the dated pattern are also
// left untouched. If the target name already exists the directory is skipped
// with a warning rather than aborting the entire migration.
func MigrateWip(repoPath string) (*MigrateWipResult, error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return nil, fmt.Errorf("not a git repo: %w", err)
	}

	wipBase := filepath.Join(top, ".wip")
	entries, err := os.ReadDir(wipBase)
	if err != nil {
		if os.IsNotExist(err) {
			return &MigrateWipResult{WipBase: wipBase}, nil
		}
		return nil, fmt.Errorf("read wip directory: %w", err)
	}

	datedPattern := regexp.MustCompile(`^\d{8}-`)
	today := time.Now().Format("20060102")

	// Canonical files that identify a WIP branch directory.
	wipMarkers := []string{"status.json", "plan.md", "context.md", "review.md", "research.md", "execution.md", "pr.md"}

	var migrated []MigratedDir
	var warnings []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if datedPattern.MatchString(name) {
			continue // already in the correct format
		}

		// Only migrate directories that look like WIP branch dirs.
		dirPath := filepath.Join(wipBase, name)
		isWip := false
		for _, marker := range wipMarkers {
			if _, err := os.Stat(filepath.Join(dirPath, marker)); err == nil {
				isWip = true
				break
			}
		}
		if !isWip {
			continue
		}

		newName := today + "-" + name
		newPath := filepath.Join(wipBase, newName)
		if _, err := os.Lstat(newPath); err == nil {
			warnings = append(warnings, fmt.Sprintf("skip %s: target %s already exists", name, newName))
			continue
		}
		if err := os.Rename(dirPath, newPath); err != nil {
			warnings = append(warnings, fmt.Sprintf("skip %s: rename failed: %v", name, err))
			continue
		}
		migrated = append(migrated, MigratedDir{Old: name, New: newName})
	}
	return &MigrateWipResult{WipBase: wipBase, Migrated: migrated, Warnings: warnings}, nil
}

// ClearWip deletes old .wip branch dirs, keeping only the single most recently
// dated one across all branches. If no dated dirs exist, the most recent legacy
// dir (alphabetically last) is preserved to avoid data loss. This operates
// globally across all branches in the repo — it is intended as a workspace
// cleanup tool, not a per-branch operation.
func ClearWip(repoPath string) (*ClearWipResult, error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return nil, err
	}

	wipBase := filepath.Join(top, ".wip")
	if info, err := os.Stat(wipBase); err != nil || !info.IsDir() {
		return &ClearWipResult{Kept: nil, Deleted: []string{}}, nil
	}

	entries, err := os.ReadDir(wipBase)
	if err != nil {
		return nil, err
	}

	datedPattern := regexp.MustCompile(`^(\d{8})-(.+)$`)
	type datedDir struct {
		date string
		path string
		name string
	}
	var dated []datedDir
	var others []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := datedPattern.FindStringSubmatch(e.Name())
		if m != nil {
			if _, err := time.Parse("20060102", m[1]); err == nil {
				dated = append(dated, datedDir{date: m[1], path: filepath.Join(wipBase, e.Name()), name: e.Name()})
				continue
			}
		}
		others = append(others, e.Name())
	}

	// Sort dated ascending
	sort.Slice(dated, func(i, j int) bool {
		if dated[i].date != dated[j].date {
			return dated[i].date < dated[j].date
		}
		return dated[i].name < dated[j].name
	})

	sort.Strings(others)

	var keep *string
	if len(dated) > 0 {
		keepName := dated[len(dated)-1].name
		keep = &keepName
	} else if len(others) > 0 {
		// No dated dirs — preserve the last legacy dir (alphabetically) to avoid
		// deleting everything when the user has never used a dated workflow.
		keepName := others[len(others)-1]
		keep = &keepName
	}

	var deleted []string
	for i := 0; i < len(dated)-1; i++ {
		if err := os.RemoveAll(dated[i].path); err != nil {
			return nil, err
		}
		deleted = append(deleted, dated[i].name)
	}
	for _, name := range others {
		if keep != nil && name == *keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(wipBase, name)); err != nil {
			return nil, err
		}
		deleted = append(deleted, name)
	}

	sort.Strings(deleted)
	return &ClearWipResult{Kept: keep, Deleted: deleted}, nil
}

// EnsureRepoResult is returned by EnsureRepo.
type EnsureRepoResult struct {
	Repo      string `json:"repo"`
	DocsDir   string `json:"docs_dir"`
	WipDir    string `json:"wip_dir"`
	GithubDir string `json:"github_dir"`
}

// EnsureRepo bootstraps a single repo with .wip and .claude/repo-docs.
func EnsureRepo(repoPath string) (*EnsureRepoResult, error) {
	if !git.IsGitRepo(repoPath) {
		return nil, fmt.Errorf("not a git repo: %s", repoPath)
	}

	top, err := git.Toplevel(repoPath)
	if err != nil {
		return nil, err
	}

	docsDir := filepath.Join(top, ".claude", "repo-docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return nil, err
	}

	wipDir := filepath.Join(top, ".wip")
	if err := os.MkdirAll(wipDir, 0o755); err != nil {
		return nil, err
	}

	githubDir := filepath.Join(top, ".github")

	return &EnsureRepoResult{
		Repo:      top,
		DocsDir:   docsDir,
		WipDir:    wipDir,
		GithubDir: githubDir,
	}, nil
}

// BootstrapWorkspaceResult is returned by BootstrapWorkspace.
type BootstrapWorkspaceResult struct {
	Workspace string   `json:"workspace"`
	Repos     []string `json:"repos"`
}

// BootstrapWorkspace bootstraps all repos in a workspace.
func BootstrapWorkspace(workspacePath string) (*BootstrapWorkspaceResult, error) {
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		return nil, err
	}

	if info, err := os.Stat(absPath); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("workspace does not exist: %s", workspacePath)
	}

	repos, err := DetectRepos(absPath)
	if err != nil {
		return nil, err
	}

	for _, repo := range repos {
		if _, err := EnsureRepo(repo); err != nil {
			return nil, err
		}
	}

	return &BootstrapWorkspaceResult{
		Workspace: absPath,
		Repos:     repos,
	}, nil
}

// DetectRepos finds all git repos in a workspace (root + immediate subdirs).
// Only scans root and immediate subdirectories — intentionally not recursive
// to avoid descending into vendor/, node_modules/, or nested git submodules.
func DetectRepos(workspacePath string) ([]string, error) {
	var repos []string
	seen := make(map[string]bool)

	check := func(path string) {
		if !git.IsGitRepo(path) {
			return
		}
		top, err := git.Toplevel(path)
		if err != nil {
			return
		}
		if seen[top] {
			return
		}
		seen[top] = true
		repos = append(repos, top)
	}

	check(workspacePath)

	entries, err := os.ReadDir(workspacePath)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			check(filepath.Join(workspacePath, e.Name()))
		}
	}

	return repos, nil
}

// --- helpers ---

func seedFileIfMissing(path, content string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return util.AtomicWrite(path, []byte(content), 0o644)
	}
	return nil
}

func initialStatus(repoName, repoPath, branch string) map[string]any {
	now := util.NowISO()
	return map[string]any{
		"repo":                repoName,
		"repo_path":           repoPath,
		"branch":              branch,
		"stage":               "started",
		"created_at":          now,
		"updated_at":          now,
		"last_completed_step": nil,
		"review_passes":       []any{},
	}
}

func sortedStages() []string {
	var ss []string
	for s := range stages {
		ss = append(ss, s)
	}
	sort.Strings(ss)
	return ss
}

func collectContextFiles(wipDir string) map[string]string {
	filenames := []string{"plan.md", "research.md", "execution.md", "review.md", "pr.md", "context.md"}
	result := make(map[string]string)
	for _, name := range filenames {
		data, _ := os.ReadFile(filepath.Join(wipDir, name))
		result[name] = strings.TrimSpace(string(data))
	}
	statusPath := filepath.Join(wipDir, "status.json")
	data, _ := os.ReadFile(statusPath)
	result["status.json"] = strings.TrimSpace(string(data))
	return result
}

// titleCase capitalizes the first letter of each space-separated word.
// Used instead of the deprecated strings.Title.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func trim(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "_none_"
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + " ..."
}

func generateSummaryText(files map[string]string, status map[string]any) string {
	branch, _ := status["branch"].(string)
	stage, _ := status["stage"].(string)
	updated, _ := status["updated_at"].(string)

	lines := []string{
		"# Workflow Summary",
		"",
		fmt.Sprintf("## Branch\n%s", branch),
		"",
		fmt.Sprintf("## Current Stage\n%s  (updated: %s)", stage, updated),
		"",
		fmt.Sprintf("## Goal\n%s", trim(files["context.md"], 300)),
		"",
		fmt.Sprintf("## Implementation Plan\n%s", trim(files["plan.md"], 400)),
		"",
		fmt.Sprintf("## Key Research Findings\n%s", trim(files["research.md"], 300)),
		"",
		fmt.Sprintf("## Implementation Progress\n%s", trim(files["execution.md"], 300)),
		"",
		fmt.Sprintf("## Review Findings\n%s", trim(files["review.md"], 200)),
		"",
		fmt.Sprintf("## PR Preparation\n%s", trim(files["pr.md"], 150)),
		"",
	}
	return strings.Join(lines, "\n")
}
