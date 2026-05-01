package install

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"aidw/cmd/aidw/internal/util"
	"aidw/cmd/aidw/internal/wip"
	embedfs "aidw"
)

// BootstrapResult summarises what the bootstrap/upgrade process applied.
type BootstrapResult struct {
	ClaudeMD    string   `json:"claude_md"`
	Settings    string   `json:"settings"`
	MCPJSON     string   `json:"mcp_json"`
	Gitignore   string   `json:"gitignore"`
	SqliteVec   string   `json:"sqlite_vec"`
	Skills      []string `json:"skills"`
	Agents      []string `json:"agents"`
	RepoPath    string   `json:"repo_path,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

// BootstrapOptions configures the bootstrap process.
type BootstrapOptions struct {
	RepoPath    string // Path to a specific repository to bootstrap.
	SourcePath  string // If provided, symlink skills/agents from this repo instead of copying from embedded FS.
	Interactive bool   // If true, prompt for optional features (Adversarial Review, RTK).
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

	// 4. Configure MCP
	fmt.Fprintln(w, "→ Configuring MCP servers...")
	if err := MergeMCPJSON(w); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("mcp: %v", err))
		result.MCPJSON = "failed"
	} else {
		result.MCPJSON = filepath.Join(claudeHome, "mcp.json")
	}

	// 5. Configure Settings
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

	// 6. Merge CLAUDE.md (Global)
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

	// 7. Update global gitignore
	fmt.Fprintln(w, "→ Updating global gitignore...")
	if err := UpdateGlobalGitignore(); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("gitignore: %v", err))
		result.Gitignore = "failed"
	} else {
		result.Gitignore = "updated"
	}

	// 8. Setup Shell and Environment
	if opts.SetupShell {
		fmt.Fprintln(w, "→ Setting up shell profile and environment...")
		if err := SetupShell(opts.Interactive, w); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("shell-setup: %v", err))
		}
	}

	// 9. Repo-specific bootstrap
	if opts.RepoPath != "" {
		fmt.Fprintf(w, "→ Bootstrapping repository: %s\n", opts.RepoPath)
		if _, err := wip.EnsureRepo(opts.RepoPath); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("repo bootstrap: %v", err))
		} else {
			result.RepoPath = opts.RepoPath
		}
	}

	return result, nil
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
		if e.IsDir() { skills = append(skills, e.Name()) }
	}

	// Agents
	agentsFS, _ := fs.Sub(embedfs.FS, "claude/agents")
	claudeAgents := filepath.Join(claudeHome, "agents")
	util.CopyFS(agentsFS, claudeAgents)
	
	entries, _ = fs.ReadDir(agentsFS, ".")
	for _, e := range entries {
		if !e.IsDir() { agents = append(agents, e.Name()) }
	}

	// Managed scripts
	scripts := map[string]string{
		"templates/global/scripts/statusline.sh":         "statusline.sh",
		"templates/global/scripts/save-wip-snapshot.sh":  "save-wip-snapshot.sh",
		"templates/global/scripts/get-embeddings.template.sh": "get-embeddings.sh",
		"bin/serena-query":                               "bin/serena-query",
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
