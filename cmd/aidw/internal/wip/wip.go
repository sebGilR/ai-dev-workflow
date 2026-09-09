// Package wip manages branch-scoped workflow state (.wip directories).
package wip

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// ErrNoActiveWork is returned by lookup-only resolution (FindBranchState and
// callers built on it) when no .wip branch directory exists yet. Callers
// should surface a message pointing at /wip-start rather than silently
// creating state.
var ErrNoActiveWork = errors.New("no active work for this branch — run /wip-start")

// wipFiles is the list of standard WIP workflow files.
var wipFiles = []string{"plan.md", "spec.md", "task-context.md", "review.md", "research.md", "context.md", "execution.md", "pr.md"}

// keepOnCleanup is the set of files to keep during cleanup-branch. status.json
// must never be swept — cleanup must not reset workflow stage/state.
var keepOnCleanup = map[string]bool{"context.md": true, "pr.md": true, "task-context.md": true, "spec.md": true, "status.json": true}

// branchArchiveDirName is where CleanupBranch archives (rather than deletes)
// non-kept files, under the branch's own .wip/<branch>/ directory.
const branchArchiveDirName = "archive"

// globalArchiveDirName is where ClearWip/ClearOtherBranches archive
// (rather than delete) entire condemned branch directories, under .wip/.
const globalArchiveDirName = ".archive"

// uniqueArchivePath returns a destination path for moving `name` into
// archiveRoot, appending "-2", "-3", ... on collision so repeated
// archive/clear runs never clobber each other's content.
func uniqueArchivePath(archiveRoot, name string) string {
	dest := filepath.Join(archiveRoot, name)
	for i := 2; ; i++ {
		if _, err := os.Lstat(dest); err != nil {
			return dest
		}
		dest = filepath.Join(archiveRoot, fmt.Sprintf("%s-%d", name, i))
	}
}

// stages is the set of valid workflow stages.
var stages = map[string]bool{
	"started":       true,
	"planned":       true, // Legacy/Simple
	"specified":     true, // BMAD: spec.md exists
	"spec-reviewed": true, // BMAD: skeptic pass done
	"researched":    true,
	"implementing":  true,
	"reviewed":      true,
	"review-fixed":  true,
	"pr-prep":       true,
}

// Status represents the state recorded in status.json.
type Status struct {
	Repo              string   `json:"repo"`
	RepoPath          string   `json:"repo_path"`
	Branch            string   `json:"branch"`
	Stage             string   `json:"stage"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
	LastCompletedStep *string  `json:"last_completed_step"`
	ReviewPasses      []string `json:"review_passes"`
}

// BranchState contains the resolved state for a branch's WIP directory.
type BranchState struct {
	Repo   string         `json:"repo"`
	Branch string         `json:"branch"`
	WipDir string         `json:"wip_dir"`
	Status Status `json:"status"`
}

// resolveBranchName applies the current-branch fallback + slugification shared
// by EnsureBranchState and FindBranchState.
func resolveBranchName(top, branch string) (string, error) {
	branchName := branch
	if branchName == "" {
		b, err := git.CurrentBranch(top)
		if err != nil {
			return "", fmt.Errorf("get current branch: %w", err)
		}
		branchName = b
	}
	return slug.SafeSlug(branchName), nil
}

// findExistingWipDir resolves phases 1-2 of branch-dir resolution (dated dir,
// then legacy unprefixed dir) without creating anything. Returns "" if
// neither exists. This is the lookup-only core shared by EnsureBranchState
// (which falls through to phase 3 creation) and FindBranchState (which does
// not).
func findExistingWipDir(top, branchName string) (string, error) {
	wipBase := filepath.Join(top, ".wip")

	// Phase 1: find existing dated dir (YYYYMMDD or YYYYMMDDHHMMSS prefix); pick the newest
	// Note: check-then-create is not atomic; concurrent invocations for the same branch
	// could both see no existing dir and each create one. In practice this tool runs once
	// per CLI invocation so the race is not reachable under normal use.
	datePattern := regexp.MustCompile(`^(\d{8}(?:\d{6})?)-` + regexp.QuoteMeta(branchName) + `$`)
	var candidates []string
	entries, err := os.ReadDir(wipBase)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read wip directory: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := datePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if _, err := time.Parse("20060102150405", m[1]); err != nil {
			if _, err := time.Parse("20060102", m[1]); err != nil {
				continue
			}
		}
		candidates = append(candidates, e.Name())
	}
	sort.Strings(candidates)

	if len(candidates) > 0 {
		return filepath.Join(wipBase, candidates[len(candidates)-1]), nil
	}

	// Phase 2: legacy unprefixed dir
	legacy := filepath.Join(wipBase, branchName)
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		return legacy, nil
	}

	return "", nil
}

// FindBranchState resolves the branch's WIP directory using the same
// phase 1-2 lookup as EnsureBranchState, but never creates a repo, a .wip
// directory, or seeds any files. It returns ErrNoActiveWork when no branch
// directory (or no status.json inside it) exists. Use this for read-only
// commands (status, next-action, context-summary, memory) so they never
// spontaneously create workflow state — only `aidw start` / `/wip-start`
// (EnsureBranchState) does that.
func FindBranchState(repoPath, branch string) (*BranchState, error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return nil, fmt.Errorf("not a git repo: %w", err)
	}

	branchName, err := resolveBranchName(top, branch)
	if err != nil {
		return nil, err
	}

	wipDir, err := findExistingWipDir(top, branchName)
	if err != nil {
		return nil, err
	}
	if wipDir == "" {
		return nil, ErrNoActiveWork
	}

	statusPath := filepath.Join(wipDir, "status.json")
	if _, err := os.Stat(statusPath); err != nil {
		return nil, ErrNoActiveWork
	}

	var status Status
	if err := util.ReadJSON(statusPath, &status); err != nil {
		return nil, fmt.Errorf("read status.json: %w", err)
	}

	return &BranchState{
		Repo:   top,
		Branch: branchName,
		WipDir: wipDir,
		Status: status,
	}, nil
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

	branchName, err := resolveBranchName(top, branch)
	if err != nil {
		return nil, err
	}

	wipBase := filepath.Join(top, ".wip")
	if err := os.MkdirAll(wipBase, 0o755); err != nil {
		return nil, err
	}

	wipDir, err := findExistingWipDir(top, branchName)
	if err != nil {
		return nil, err
	}

	// Phase 3: create new dated dir
	if wipDir == "" {
		datePrefix := time.Now().Format("20060102150405")
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
	var status Status
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
		if status.Repo != filepath.Base(top) {
			status.Repo = filepath.Base(top)
			changed = true
		}
		if status.RepoPath != top {
			status.RepoPath = top
			changed = true
		}
		if status.Branch != branchName {
			status.Branch = branchName
			changed = true
		}
		if changed {
			status.UpdatedAt = util.NowISO()
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
type SetStageResult *Status

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
		var requiredFiles []string
		switch stage {
		case "planned":
			requiredFiles = []string{"plan.md"}
		case "specified":
			requiredFiles = []string{"spec.md", "task-context.md"}
		case "spec-reviewed":
			requiredFiles = []string{"spec.md", "task-context.md", "review.md"}
		case "researched":
			requiredFiles = []string{"research.md", "task-context.md"}
		case "reviewed":
			requiredFiles = []string{"review.md"}
		}
		for _, requiredFile := range requiredFiles {
			ok, errMsg := VerifyWipFile(state.WipDir, requiredFile)
			if !ok {
				return nil, fmt.Errorf("cannot transition to stage '%s': %s\nHint: Ensure %s exists and has content before setting this stage.\n      Use --skip-verification to bypass this check (not recommended).", stage, errMsg, requiredFile)
			}
		}
	}

	statusPath := filepath.Join(state.WipDir, "status.json")
	var status Status
	if err := util.ReadJSON(statusPath, &status); err != nil {
		return nil, err
	}

	status.Stage = stage
	status.UpdatedAt = util.NowISO()
	status.LastCompletedStep = &stage

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

	return (*Status)(&status), nil
}

// ContextSummaryResult is returned by WriteContextSummary.
type ContextSummaryResult struct {
	SummaryPath string `json:"summary_path"`
	SizeBytes   int    `json:"size_bytes"`
	Branch      string `json:"branch"`
}

// WriteContextSummary generates context-summary.md from WIP files. It is a
// lookup-only operation: it never creates a .wip directory — call
// EnsureBranchState (via `aidw start`) first if none exists.
func WriteContextSummary(repoPath string) (*ContextSummaryResult, error) {
	state, err := FindBranchState(repoPath, "")
	if err != nil {
		return nil, err
	}

	files := collectContextFiles(state.WipDir)
	summary := generateSummaryText(files, state.Status)
	header := summaryProvenanceHeader(state.WipDir, util.NowISO())
	full := header + "\n" + summary

	summaryPath := filepath.Join(state.WipDir, "context-summary.md")
	if err := util.AtomicWrite(summaryPath, []byte(full), 0o644); err != nil {
		return nil, err
	}

	branch := state.Status.Branch
	return &ContextSummaryResult{
		SummaryPath: summaryPath,
		SizeBytes:   len(full),
		Branch:      branch,
	}, nil
}

// SummaryStaleness reports whether context-summary.md's provenance header
// still matches the current on-disk contents of its source files.
type SummaryStaleness struct {
	Stale       bool   `json:"stale"`
	GeneratedAt string `json:"generated_at"`
	SummaryPath string `json:"summary_path"`
}

// CheckSummaryStaleness is lookup-only: it never creates a .wip directory or
// a summary file. An absent context-summary.md is itself reported as an
// error (callers should tell the user to run `aidw summarize-context`); a
// present-but-unparseable header (e.g. hand-edited or pre-A3a) counts as
// stale rather than erroring, since there is nothing to verify.
func CheckSummaryStaleness(repoPath string) (*SummaryStaleness, error) {
	state, err := FindBranchState(repoPath, "")
	if err != nil {
		return nil, err
	}

	summaryPath := filepath.Join(state.WipDir, "context-summary.md")
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		return nil, fmt.Errorf("no context-summary.md found — run: aidw summarize-context <path>")
	}

	prov, ok := parseSummaryProvenance(string(data))
	if !ok {
		return &SummaryStaleness{Stale: true, SummaryPath: summaryPath}, nil
	}

	current, _ := parseSummaryProvenance(summaryProvenanceHeader(state.WipDir, prov.GeneratedAt))
	stale := false
	for name, hash := range current.Sources {
		if prov.Sources[name] != hash {
			stale = true
			break
		}
	}

	return &SummaryStaleness{Stale: stale, GeneratedAt: prov.GeneratedAt, SummaryPath: summaryPath}, nil
}
// NextAction represents the recommended next step in the workflow.
type NextAction struct {
	Stage       string `json:"current_stage"`
	Action      string `json:"recommended_action"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

// GetNextAction identifies the logical next step based on current state.
// Lookup-only: never creates a .wip directory.
func GetNextAction(repoPath string) (*NextAction, error) {
	state, err := FindBranchState(repoPath, "")
	if err != nil {
		return nil, err
	}

	stage := state.Status.Stage

	switch stage {
	case "started":
		return &NextAction{
			Stage:       stage,
			Action:      "Plan",
			Command:     "/wip-plan",
			Description: "Research context and draft the implementation specification (spec.md).",
		}, nil
	case "planned":
		return &NextAction{
			Stage:       stage,
			Action:      "Specify",
			Command:     "Draft spec.md",
			Description: "Convert your plan into a hardened specification with Task X headers.",
		}, nil
	case "specified":
		return &NextAction{
			Stage:       stage,
			Action:      "Review Spec",
			Command:     "/wip-plan (Skeptic Step)",
			Description: "Run the Skeptic subagent to find flaws in your spec.md.",
		}, nil
	case "spec-reviewed":
		return &NextAction{
			Stage:       stage,
			Action:      "Implement",
			Command:     "/wip-implement",
			Description: "Start the autonomous implementation loop based on spec.md.",
		}, nil
	case "implementing":
		task, _ := NextTask(repoPath)
		if task != nil {
			return &NextAction{
				Stage:       stage,
				Action:      "Continue Implementation",
				Command:     fmt.Sprintf("aidw task next . (Task %d)", task.ID),
				Description: fmt.Sprintf("Next task: %s", task.Description),
			}, nil
		}
		return &NextAction{
			Stage:       stage,
			Action:      "Review",
			Command:     "/wip-review",
			Description: "Implementation finished. Build a review bundle and verify changes.",
		}, nil
	case "researched":
		return &NextAction{
			Stage:       stage,
			Action:      "Specify",
			Command:     "Draft spec.md",
			Description: "Use your research to draft a hardened specification.",
		}, nil
	case "reviewed":
		return &NextAction{
			Stage:       stage,
			Action:      "Fix Review",
			Command:     "/wip-fix-review",
			Description: "Address the findings consolidated in review.md.",
		}, nil
	case "review-fixed":
		return &NextAction{
			Stage:       stage,
			Action:      "PR Prep",
			Command:     "/wip-pr",
			Description: "Draft the Pull Request content (pr.md).",
		}, nil
	}

	return &NextAction{
		Stage:       stage,
		Action:      "Continue",
		Command:     "aidw status .",
		Description: "Check current branch status for clues.",
	}, nil
}

// SummarizeStatus returns a human-readable status summary. Lookup-only:
// never creates a .wip directory.
func SummarizeStatus(repoPath string) (string, error) {
	state, err := FindBranchState(repoPath, "")
	if err != nil {
		return "", err
	}
	top := state.Repo

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

	branch := state.Status.Branch
	stage := state.Status.Stage
	updatedAt := state.Status.UpdatedAt

	next, _ := GetNextAction(top)
	nextStr := ""
	if next != nil {
		nextStr = fmt.Sprintf("\nRecommended Next Step:\n- Action: %s\n- Command: %s\n- Why: %s\n", next.Action, next.Command, next.Description)
	}

	return fmt.Sprintf(`Repo: %s
Branch: %s
Stage: %s
Updated: %s
WIP directory: %s
%s
Context preview:
`, top, branch, stage, updatedAt, state.WipDir, nextStr) + fmt.Sprintf(`
%s

Plan preview:
%s

Execution preview:
%s
`, trim(context, 600), trim(plan, 600), trim(execution, 600)), nil
}

// CleanupResult is returned by CleanupBranch.
type CleanupResult struct {
	WipDir     string   `json:"wip_dir"`
	Kept       []string `json:"kept"`
	Archived   []string `json:"archived,omitempty"`
	ArchiveDir string   `json:"archive_dir,omitempty"`
	Deleted    []string `json:"deleted,omitempty"`
	DryRun     bool     `json:"dry_run,omitempty"`
	Purge      bool     `json:"purge,omitempty"`
}

// CleanupBranch archives (moves) all files in the branch's .wip dir except
// the kept set (context.md, pr.md, spec.md, task-context.md, status.json)
// into archive/<YYYYMMDDHHMMSS>/, instead of deleting them. The archive/
// directory itself is never swept by a later cleanup run. Pass purge=true
// to permanently delete both the current non-kept entries AND everything
// previously archived — this is the only path that deletes bytes, and
// callers (the wip-cleanup skill) must preview and confirm before using it.
func CleanupBranch(repoPath string, dryRun, purge bool) (*CleanupResult, error) {
	state, err := EnsureBranchState(repoPath, "")
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(state.WipDir)
	if err != nil {
		return nil, err
	}

	kept := make([]string, 0, len(keepOnCleanup))
	for k := range keepOnCleanup {
		kept = append(kept, k)
	}
	sort.Strings(kept)

	var candidates []string
	for _, e := range entries {
		name := e.Name()
		if keepOnCleanup[name] || name == branchArchiveDirName {
			continue
		}
		candidates = append(candidates, name)
	}
	sort.Strings(candidates)

	result := &CleanupResult{WipDir: state.WipDir, Kept: kept, DryRun: dryRun, Purge: purge}

	if purge {
		var deleted []string
		archiveRoot := filepath.Join(state.WipDir, branchArchiveDirName)
		if archivedEntries, err := os.ReadDir(archiveRoot); err == nil {
			for _, ae := range archivedEntries {
				deleted = append(deleted, filepath.Join(branchArchiveDirName, ae.Name()))
			}
		}
		deleted = append(deleted, candidates...)
		sort.Strings(deleted)

		if !dryRun {
			for _, name := range candidates {
				path := filepath.Join(state.WipDir, name)
				if err := os.RemoveAll(path); err != nil {
					return nil, err
				}
			}
			if err := os.RemoveAll(archiveRoot); err != nil {
				return nil, err
			}
		}
		result.Deleted = deleted
		return result, nil
	}

	if len(candidates) > 0 {
		ts := time.Now().Format("20060102150405")
		archiveDir := filepath.Join(state.WipDir, branchArchiveDirName, ts)
		if !dryRun {
			if err := os.MkdirAll(archiveDir, 0o755); err != nil {
				return nil, err
			}
			for _, name := range candidates {
				src := filepath.Join(state.WipDir, name)
				dst := filepath.Join(archiveDir, name)
				if err := os.Rename(src, dst); err != nil {
					return nil, err
				}
			}
		}
		result.Archived = candidates
		result.ArchiveDir = archiveDir
	}

	return result, nil
}

// ClearWipResult is returned by ClearWip and ClearOtherBranches.
type ClearWipResult struct {
	Kept     *string  `json:"kept"`
	Archived []string `json:"archived,omitempty"`
	Deleted  []string `json:"deleted,omitempty"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Purge    bool     `json:"purge,omitempty"`
}

// archiveOrDeleteDirs moves each named top-level dir under wipBase into
// globalArchiveDirName (collision-safe), or — when purge is true — deletes
// it outright. It never touches globalArchiveDirName itself; the purge path
// additionally wipes globalArchiveDirName's own previously-archived content
// once, via wipeGlobalArchive. Returns the names actually processed.
func archiveOrDeleteDirs(wipBase string, names []string, dryRun, purge bool) ([]string, error) {
	var processed []string
	archiveRoot := filepath.Join(wipBase, globalArchiveDirName)
	for _, name := range names {
		path := filepath.Join(wipBase, name)
		if !dryRun {
			if purge {
				if err := os.RemoveAll(path); err != nil {
					return nil, err
				}
			} else {
				if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
					return nil, err
				}
				dest := uniqueArchivePath(archiveRoot, name)
				if err := os.Rename(path, dest); err != nil {
					return nil, err
				}
			}
		}
		processed = append(processed, name)
	}
	return processed, nil
}

// listGlobalArchiveContents returns the top-level names currently archived
// under .wip/.archive/, for purge previews.
func listGlobalArchiveContents(wipBase string) []string {
	var names []string
	entries, err := os.ReadDir(filepath.Join(wipBase, globalArchiveDirName))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		names = append(names, filepath.Join(globalArchiveDirName, e.Name()))
	}
	return names
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

	datedPattern := regexp.MustCompile(`^\d{8}(?:\d{6})?-`)
	today := time.Now().Format("20060102150405")

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

// ClearWip archives (moves into .wip/.archive/, rather than deleting) old
// .wip branch dirs, keeping only the single most recently dated one across
// all branches. If no dated dirs exist, the most recent legacy dir
// (alphabetically last) is preserved to avoid data loss. This operates
// globally across all branches in the repo — it is intended as a workspace
// cleanup tool, not a per-branch operation. Pass purge=true to permanently
// delete both the condemned dirs AND everything already under
// .wip/.archive/.
func ClearWip(repoPath string, dryRun, purge bool) (*ClearWipResult, error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return nil, err
	}

	wipBase := filepath.Join(top, ".wip")
	if info, err := os.Stat(wipBase); err != nil || !info.IsDir() {
		return &ClearWipResult{Kept: nil, DryRun: dryRun, Purge: purge}, nil
	}

	entries, err := os.ReadDir(wipBase)
	if err != nil {
		return nil, err
	}

	datedPattern := regexp.MustCompile(`^(\d{8}(?:\d{6})?)-(.+)$`)
	type datedDir struct {
		date string
		name string
	}
	var dated []datedDir
	var others []string

	for _, e := range entries {
		if !e.IsDir() || e.Name() == globalArchiveDirName {
			continue
		}
		m := datedPattern.FindStringSubmatch(e.Name())
		if m != nil {
			if _, err := time.Parse("20060102150405", m[1]); err == nil {
				dated = append(dated, datedDir{date: m[1], name: e.Name()})
				continue
			}
			if _, err := time.Parse("20060102", m[1]); err == nil {
				dated = append(dated, datedDir{date: m[1], name: e.Name()})
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

	var candidates []string
	for i := 0; i < len(dated)-1; i++ {
		candidates = append(candidates, dated[i].name)
	}
	for _, name := range others {
		if keep != nil && name == *keep {
			continue
		}
		candidates = append(candidates, name)
	}
	sort.Strings(candidates)

	if purge {
		deleted := append([]string{}, listGlobalArchiveContents(wipBase)...)
		deleted = append(deleted, candidates...)
		sort.Strings(deleted)
		if _, err := archiveOrDeleteDirs(wipBase, candidates, dryRun, true); err != nil {
			return nil, err
		}
		if !dryRun {
			if err := os.RemoveAll(filepath.Join(wipBase, globalArchiveDirName)); err != nil {
				return nil, err
			}
		}
		return &ClearWipResult{Kept: keep, Deleted: deleted, DryRun: dryRun, Purge: purge}, nil
	}

	archived, err := archiveOrDeleteDirs(wipBase, candidates, dryRun, false)
	if err != nil {
		return nil, err
	}
	return &ClearWipResult{Kept: keep, Archived: archived, DryRun: dryRun, Purge: purge}, nil
}

// ClearOtherBranches archives (moves into .wip/.archive/, rather than
// deleting) all .wip branch dirs except the current branch's dir. Unlike
// ClearWip (which keeps the most-recently-dated dir globally), this command
// identifies the "keep" dir by the current git branch. All files inside the
// kept dir are preserved. Pass purge=true to permanently delete both the
// other branch dirs AND everything already under .wip/.archive/.
//
// Lookup-only: requires an existing active-work dir for the current branch
// (ErrNoActiveWork otherwise) — it does not create or seed one.
func ClearOtherBranches(repoPath string, dryRun, purge bool) (*ClearWipResult, error) {
	state, err := FindBranchState(repoPath, "")
	if err != nil {
		return nil, err
	}

	keepDirName := filepath.Base(state.WipDir)
	wipBase := filepath.Dir(state.WipDir)

	entries, err := os.ReadDir(wipBase)
	if err != nil {
		return nil, err
	}

	var candidates []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == keepDirName || e.Name() == globalArchiveDirName {
			continue
		}
		candidates = append(candidates, e.Name())
	}
	sort.Strings(candidates)

	if purge {
		deleted := append([]string{}, listGlobalArchiveContents(wipBase)...)
		deleted = append(deleted, candidates...)
		sort.Strings(deleted)
		if _, err := archiveOrDeleteDirs(wipBase, candidates, dryRun, true); err != nil {
			return nil, err
		}
		if !dryRun {
			if err := os.RemoveAll(filepath.Join(wipBase, globalArchiveDirName)); err != nil {
				return nil, err
			}
		}
		return &ClearWipResult{Kept: &keepDirName, Deleted: deleted, DryRun: dryRun, Purge: purge}, nil
	}

	archived, err := archiveOrDeleteDirs(wipBase, candidates, dryRun, false)
	if err != nil {
		return nil, err
	}
	return &ClearWipResult{Kept: &keepDirName, Archived: archived, DryRun: dryRun, Purge: purge}, nil
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

func initialStatus(repoName, repoPath, branch string) Status {
	now := util.NowISO()
	return Status{
		Repo:         repoName,
		RepoPath:     repoPath,
		Branch:       branch,
		Stage:        "started",
		CreatedAt:    now,
		UpdatedAt:    now,
		ReviewPasses: []string{},
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

// summarySourceFiles lists the WIP files (in stable order) that feed
// context-summary.md generation. Order is also used for the provenance
// header's source hash list, so it must stay stable across releases.
var summarySourceFiles = []string{"plan.md", "research.md", "execution.md", "review.md", "pr.md", "context.md", "spec.md", "task-context.md"}

func collectContextFiles(wipDir string) map[string]string {
	result := make(map[string]string)
	for _, name := range summarySourceFiles {
		data, _ := os.ReadFile(filepath.Join(wipDir, name))
		result[name] = strings.TrimSpace(string(data))
	}
	statusPath := filepath.Join(wipDir, "status.json")
	data, _ := os.ReadFile(statusPath)
	result["status.json"] = strings.TrimSpace(string(data))
	return result
}

// summaryProvenancePrefix/Pattern define the machine-readable header
// prepended to context-summary.md so staleness can be detected later without
// re-reading every source file's mtime (which is unreliable across clones/CI
// checkouts). Format:
//
//	<!-- aidw:summary generated_at=<ISO8601> sources=<name>:<sha256-8>,... -->
var summaryProvenancePattern = regexp.MustCompile(`^<!-- aidw:summary generated_at=(\S+) sources=(.*) -->\s*$`)

// summaryProvenanceHeader builds the provenance header for the CURRENT
// on-disk contents of wipDir's summary source files. generatedAt is the
// timestamp to embed (callers pass util.NowISO() when generating; staleness
// checks only care about the sources= hashes, not the timestamp).
func summaryProvenanceHeader(wipDir, generatedAt string) string {
	parts := make([]string, 0, len(summarySourceFiles))
	for _, name := range summarySourceFiles {
		data, err := os.ReadFile(filepath.Join(wipDir, name))
		if err != nil {
			parts = append(parts, name+":-")
			continue
		}
		sum := sha256.Sum256(data)
		parts = append(parts, name+":"+hex.EncodeToString(sum[:])[:8])
	}
	return fmt.Sprintf("<!-- aidw:summary generated_at=%s sources=%s -->", generatedAt, strings.Join(parts, ","))
}

// SummaryProvenance is the parsed form of a context-summary.md provenance header.
type SummaryProvenance struct {
	GeneratedAt string
	Sources     map[string]string // filename -> sha256-8 hash, or "-" if the file was missing
}

// parseSummaryProvenance extracts the provenance header from the first line
// of content. ok is false if no (or a malformed) header is present — treated
// as stale by callers, since there is nothing to verify against.
func parseSummaryProvenance(content string) (prov SummaryProvenance, ok bool) {
	firstLine, _, _ := strings.Cut(content, "\n")
	m := summaryProvenancePattern.FindStringSubmatch(strings.TrimSpace(firstLine))
	if m == nil {
		return SummaryProvenance{}, false
	}
	prov = SummaryProvenance{GeneratedAt: m[1], Sources: map[string]string{}}
	if m[2] != "" {
		for _, pair := range strings.Split(m[2], ",") {
			name, hash, found := strings.Cut(pair, ":")
			if found {
				prov.Sources[name] = hash
			}
		}
	}
	return prov, true
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

// trim returns the HEAD of text, up to limit runes, or "_none_" if empty.
// Suited to files that are edited in place (context.md, plan.md, spec.md,
// task-context.md) where the most important content is at the top.
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

// trimTail returns the TAIL of text, up to limit runes, or "_none_" if empty.
// Suited to append-mode files (execution.md, research.md) where wip-sync
// memos and the most recent notes land at the end — a head trim would show
// stale content from the start of the file instead.
func trimTail(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "_none_"
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return "... " + strings.TrimSpace(string(runes[len(runes)-limit:]))
}

func generateSummaryText(files map[string]string, status Status) string {
	branch := status.Branch
	stage := status.Stage
	updated := status.UpdatedAt

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
		fmt.Sprintf("## Specification\n%s", trim(files["spec.md"], 400)),
		"",
		fmt.Sprintf("## Task Context\n%s", trim(files["task-context.md"], 300)),
		"",
		fmt.Sprintf("## Key Research Findings\n%s", trimTail(files["research.md"], 300)),
		"",
		fmt.Sprintf("## Implementation Progress\n%s", trimTail(files["execution.md"], 300)),
		"",
		fmt.Sprintf("## Review Findings\n%s", trim(files["review.md"], 200)),
		"",
		fmt.Sprintf("## PR Preparation\n%s", trim(files["pr.md"], 150)),
		"",
	}
	return strings.Join(lines, "\n")
}
