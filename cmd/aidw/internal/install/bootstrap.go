package install

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	embedfs "aidw"
	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/util"
	"aidw/cmd/aidw/internal/wip"
)

// BootstrapResult summarises what the bootstrap/upgrade process applied.
type BootstrapResult struct {
	ClaudeMD  string   `json:"claude_md"`
	GeminiMD  string   `json:"gemini_md"`
	Settings  string   `json:"settings"`
	MCPJSON   string   `json:"mcp_json"`
	Gitignore string   `json:"gitignore"`
	SqliteVec string   `json:"sqlite_vec"`
	Gopls     string   `json:"gopls,omitempty"`
	WipGate   string   `json:"wip_gate,omitempty"`
	Skills    []string `json:"skills"`
	Agents    []string `json:"agents"`
	RepoPath  string   `json:"repo_path,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

// BootstrapOptions configures the bootstrap process.
type BootstrapOptions struct {
	RepoPath    string // Path to a specific repository to bootstrap.
	SourcePath  string // If provided, symlink skills/agents from this repo instead of copying from embedded FS.
	Interactive bool   // If true, prompt for optional features (Adversarial Review, RTK, gopls).
	SetupShell  bool   // If true, patch shell profile and create aidw.env.sh.
}

// Bootstrap initializes the global aidw environment (~/.claude).
func Bootstrap(opts BootstrapOptions, w io.Writer) (*BootstrapResult, error) {
	result := &BootstrapResult{}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}

	claudeHome := filepath.Join(home, ".claude")
	copilotHome := filepath.Join(home, ".copilot")

	// 1. Ensure directories
	os.MkdirAll(claudeHome, 0o755)
	os.MkdirAll(copilotHome, 0o755)

	// 2. Extract Skills and Agents
	if opts.SourcePath != "" {
		src, err := filepath.Abs(opts.SourcePath)
		if err != nil {
			return nil, fmt.Errorf("source path: %w", err)
		}
		fmt.Fprintf(w, "→ Symlinking skills and agents from: %s\n", src)
		skills, agents, err := linkFromSource(src, claudeHome, copilotHome, w)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("symlink: %v", err))
		}
		result.Skills = skills
		result.Agents = agents
	} else {
		fmt.Fprintln(w, "→ Extracting embedded skills and agents...")
		skills, agents, err := extractEmbedded(claudeHome, copilotHome, w)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("extract: %v", err))
		}
		result.Skills = skills
		result.Agents = agents
	}

	// 3. Install sqlite-vec
	if err := InstallSqliteVec(w); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("sqlite-vec: %v", err))
		result.SqliteVec = "failed"
	} else {
		result.SqliteVec = "installed"
	}

	// 4. Detect gopls (Serena's Go language server dependency)
	fmt.Fprintln(w, "→ Checking for gopls...")
	goplsStatus := DetectGopls(opts.Interactive, w)
	switch {
	case goplsStatus.Installed:
		result.Gopls = "installed"
		fmt.Fprintln(w, "  gopls is on PATH.")
	case goplsStatus.GoplsPresent:
		result.Gopls = "present"
		fmt.Fprintln(w, "  gopls already installed.")
	case goplsStatus.GoPresent:
		result.Gopls = "missing"
		result.Warnings = append(result.Warnings, goplsStatus.Warning)
		fmt.Fprintf(w, "  %s\n", goplsStatus.Warning)
	}

	// 5. Configure MCP
	fmt.Fprintln(w, "→ Configuring MCP servers...")
	if err := MergeMCPJSON(w); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("mcp: %v", err))
		result.MCPJSON = "failed"
	} else {
		result.MCPJSON = filepath.Join(claudeHome, "mcp.json")
	}

	// 6. Configure Settings
	fmt.Fprintln(w, "→ Merging Claude settings...")
	settingsPath := filepath.Join(claudeHome, "settings.json")
	settingsTmpl, err := embedfs.FS.ReadFile("templates/global/settings.template.json")
	if err == nil {
		if err := MergeSettings(settingsPath, settingsTmpl); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("settings: %v", err))
			result.Settings = "failed"
		} else {
			result.Settings = settingsPath
		}
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("settings template missing: %v", err))
	}

	// 6b. Offer the opt-in workflow-gate hook (never silently enabled). A
	// second, independent MergeSettings call from step 6's — gated on an
	// interactive y/N prompt, and short-circuited if already merged — so a
	// plain `aidw upgrade` never starts denying an existing user's edits.
	gateStatus := AskEnableWipGate(opts.Interactive, w, settingsPath)
	switch {
	case gateStatus.AlreadyEnabled:
		result.WipGate = "already-enabled"
	case gateStatus.Enabled:
		result.WipGate = "enabled"
	case gateStatus.Warning != "":
		result.Warnings = append(result.Warnings, fmt.Sprintf("wip-gate: %s", gateStatus.Warning))
		result.WipGate = "failed"
	default:
		result.WipGate = "skipped"
	}

	// 7. Merge CLAUDE.md (Global)
	fmt.Fprintln(w, "→ Updating global CLAUDE.md...")
	claudeMDPath := filepath.Join(claudeHome, "CLAUDE.md")
	snippet, err := embedfs.FS.ReadFile("templates/global/claude_managed_block.md")
	if err == nil {
		if err := MergeCLAUDEMd(claudeMDPath, snippet); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("claude-md: %v", err))
			result.ClaudeMD = "failed"
		} else {
			result.ClaudeMD = claudeMDPath
		}
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("claude.md snippet missing: %v", err))
	}

	// 8. Merge GEMINI.md (Global)
	fmt.Fprintln(w, "→ Updating global GEMINI.md...")
	geminiHome := filepath.Join(home, ".gemini")
	os.MkdirAll(geminiHome, 0o755)
	geminiMDPath := filepath.Join(geminiHome, "GEMINI.md")
	gSnippet, err := embedfs.FS.ReadFile("templates/global/gemini_managed_block.md")
	if err == nil {
		if err := MergeGEMINIMd(geminiMDPath, gSnippet); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("gemini-md: %v", err))
			result.GeminiMD = "failed"
		} else {
			result.GeminiMD = geminiMDPath
		}
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("gemini.md snippet missing: %v", err))
	}

	// 9. Update global gitignore
	fmt.Fprintln(w, "→ Updating global gitignore...")
	if err := UpdateGlobalGitignore(); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("gitignore: %v", err))
		result.Gitignore = "failed"
	} else {
		result.Gitignore = "updated"
	}

	// 10. Setup Shell and Environment
	if opts.SetupShell {
		fmt.Fprintln(w, "→ Setting up shell profile and environment...")
		if err := SetupShell(opts.Interactive, w); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("shell-setup: %v", err))
		}
	}

	// 11. Repo-specific bootstrap
	if opts.RepoPath != "" {
		fmt.Fprintf(w, "→ Bootstrapping repository: %s\n", opts.RepoPath)
		if _, err := wip.EnsureRepo(opts.RepoPath); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("repo bootstrap: %v", err))
		} else {
			result.RepoPath = opts.RepoPath
			if err := SeedRepo(opts.RepoPath, w); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("repo seeding: %v", err))
			}
		}
	}

	return result, nil
}

// SeedRepo copies workflow templates (Copilot instructions, Gemini.md, skills, agents)
// into the repository so they can be committed and used by non-Claude models.
func SeedRepo(repoPath string, w io.Writer) error {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return err
	}

	// 1. .github/copilot-instructions.md
	fmt.Fprintln(w, "  → Seeding .github/copilot-instructions.md...")
	copilotDest := filepath.Join(top, ".github", "copilot-instructions.md")
	os.MkdirAll(filepath.Dir(copilotDest), 0o755)
	if data, err := embedfs.FS.ReadFile("templates/github/copilot-instructions.md"); err == nil {
		if err := util.AtomicWrite(copilotDest, data, 0o644); err != nil {
			return fmt.Errorf("write copilot instructions: %w", err)
		}
	}

	// 2. GEMINI.md
	fmt.Fprintln(w, "  → Seeding GEMINI.md...")
	geminiDest := filepath.Join(top, "GEMINI.md")
	if gSnippet, err := embedfs.FS.ReadFile("templates/global/gemini_managed_block.md"); err == nil {
		if err := MergeGEMINIMd(geminiDest, gSnippet); err != nil {
			return fmt.Errorf("merge gemini.md: %w", err)
		}
	}

	// 3. .github/skills/
	fmt.Fprintln(w, "  → Seeding .github/skills/...")
	skillsFS, err := fs.Sub(embedfs.FS, "claude/skills")
	if err == nil {
		// prune=false: this seeds an arbitrary user repo, so we never
		// delete files that aren't ours (see GenerateGithubSkills's
		// prune doc comment). Routing through GenerateGithubSkills
		// rather than a raw util.CopyFS also gets the "customizations
		// will be lost" overwrite warning the agents path already has.
		if err := GenerateGithubSkills(skillsFS, filepath.Join(top, ".github", "skills"), false); err != nil {
			return fmt.Errorf("generate github skills: %w", err)
		}
	}

	// 4. .github/agents/ (stripped of MCP sections)
	fmt.Fprintln(w, "  → Seeding .github/agents/...")
	agentsFS, err := fs.Sub(embedfs.FS, "claude/agents")
	if err == nil {
		// prune=false: this seeds an arbitrary user repo, so we never
		// delete files that aren't ours just because they share the .md
		// extension (see GenerateGithubAgents's prune doc comment).
		if err := GenerateGithubAgents(agentsFS, filepath.Join(top, ".github", "agents"), false); err != nil {
			return fmt.Errorf("generate github agents: %w", err)
		}
	}

	return nil
}

func linkFromSource(src, claudeHome, copilotHome string, w io.Writer) ([]string, []string, error) {
	var skills, agents []string

	// Skills
	srcSkills := filepath.Join(src, "claude", "skills")
	destClaude := filepath.Join(claudeHome, "skills")
	destCopilot := filepath.Join(copilotHome, "skills")

	entries, err := os.ReadDir(srcSkills)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				name := e.Name()
				skills = append(skills, name)
				util.SafeLink(filepath.Join(srcSkills, name), filepath.Join(destClaude, name))
				util.SafeLink(filepath.Join(srcSkills, name), filepath.Join(destCopilot, name))
			}
		}
	}

	// Agents
	srcAgents := filepath.Join(src, "claude", "agents")
	destAgents := filepath.Join(claudeHome, "agents")

	entries, err = os.ReadDir(srcAgents)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				name := e.Name()
				agents = append(agents, name)
				util.SafeLink(filepath.Join(srcAgents, name), filepath.Join(destAgents, name))
			}
		}
	}

	// Managed link back to repo
	util.SafeLink(src, filepath.Join(claudeHome, "ai-dev-workflow"))

	return skills, agents, nil
}

func extractEmbedded(claudeHome, copilotHome string, w io.Writer) ([]string, []string, error) {
	var skills, agents []string

	// Skills
	skillsFS, _ := fs.Sub(embedfs.FS, "claude/skills")
	claudeSkills := filepath.Join(claudeHome, "skills")
	util.CopyFS(skillsFS, claudeSkills)
	copilotSkills := filepath.Join(copilotHome, "skills")
	util.CopyFS(skillsFS, copilotSkills)

	entries, _ := fs.ReadDir(skillsFS, ".")
	for _, e := range entries {
		if e.IsDir() {
			skills = append(skills, e.Name())
		}
	}

	// Agents
	agentsFS, _ := fs.Sub(embedfs.FS, "claude/agents")
	claudeAgents := filepath.Join(claudeHome, "agents")
	util.CopyFS(agentsFS, claudeAgents)

	entries, _ = fs.ReadDir(agentsFS, ".")
	for _, e := range entries {
		if !e.IsDir() {
			agents = append(agents, e.Name())
		}
	}

	// Managed scripts
	scripts := map[string]string{
		"templates/global/scripts/statusline.sh":              "statusline.sh",
		"templates/global/scripts/save-wip-snapshot.sh":       "save-wip-snapshot.sh",
		"templates/global/scripts/session-start-context.sh":   "session-start-context.sh",
		"templates/global/scripts/get-embeddings.template.sh": "get-embeddings.sh",
		"templates/global/scripts/wip-gate.sh":                "wip-gate.sh",
		"bin/serena-query": "bin/serena-query",
	}
	for src, name := range scripts {
		data, err := embedfs.FS.ReadFile(src)
		if err == nil {
			dest := filepath.Join(claudeHome, name)
			os.MkdirAll(filepath.Dir(dest), 0o755)
			util.AtomicWrite(dest, data, 0o755)
		}
	}

	return skills, agents, nil
}
