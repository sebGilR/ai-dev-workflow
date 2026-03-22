package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/install"
	"aidw/cmd/aidw/internal/wip"
)

// UpgradeResult summarises what the upgrade command applied.
type UpgradeResult struct {
	InstallRoot string        `json:"install_root"`
	ClaudeMD    string        `json:"claude_md"`
	Settings    string        `json:"settings"`
	MCPJSON     string        `json:"mcp_json"`
	Gitignore   string        `json:"gitignore"`
	Skills      []string      `json:"skills"`
	Agents      []string      `json:"agents"`
	RepoPath    string        `json:"repo_path,omitempty"`
	WipMigrated []wip.MigratedDir `json:"wip_migrated,omitempty"`
	Warnings    []string      `json:"warnings,omitempty"`
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade [path]",
	Short: "Re-apply global configs, refresh skill/agent symlinks, and migrate legacy .wip dirs",
	Long: `upgrade re-applies all global configuration merges (CLAUDE.md managed block,
settings.json permissions, mcp.json MCP servers, global gitignore), refreshes the
skill and agent symlinks under ~/.claude/, and optionally migrates legacy un-timestamped
.wip/<slug> directories to the YYYYMMDD-<slug> format.

Pass a repo path to also run ensure-repo and migrate-wip for that directory.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(c *cobra.Command, args []string) {
		result := &UpgradeResult{}

		homeDir, err := os.UserHomeDir()
		if err != nil {
			Die("get home dir: %v", err)
		}
		claudeHome := filepath.Join(homeDir, ".claude")

		// Resolve the install root via the stable symlink.
		installLink := filepath.Join(claudeHome, "ai-dev-workflow")
		installRoot, err := filepath.EvalSymlinks(installLink)
		if err != nil {
			Die("cannot resolve %s: %v\nRe-run install.sh to repair the installation.", installLink, err)
		}
		result.InstallRoot = installRoot

		// 1. Merge CLAUDE.md managed block.
		claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
		snippet := filepath.Join(installRoot, "templates", "global", "claude_managed_block.md")
		if err := install.MergeCLAUDEMd(claudeMD, snippet); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("merge-claude-md: %v", err))
			result.ClaudeMD = "skipped (error)"
		} else {
			result.ClaudeMD = claudeMD
		}

		// 2. Merge settings.json.
		settings := filepath.Join(claudeHome, "settings.json")
		tmpl := filepath.Join(installRoot, "templates", "global", "settings.template.json")
		if err := install.MergeSettings(settings, tmpl); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("merge-settings: %v", err))
			result.Settings = "skipped (error)"
		} else {
			result.Settings = settings
		}

		// 3. Merge mcp.json.
		if err := install.MergeMCPJSON(os.Stderr); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("merge-mcp-json: %v", err))
			result.MCPJSON = "skipped (error)"
		} else {
			result.MCPJSON = filepath.Join(claudeHome, "mcp.json")
		}

		// 4. Update global gitignore.
		if err := install.UpdateGlobalGitignore(); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("update-global-gitignore: %v", err))
			result.Gitignore = "skipped (error)"
		} else {
			result.Gitignore = "updated"
		}

		// 5. Refresh skill symlinks.
		skillsSrc := filepath.Join(installRoot, "claude", "skills")
		skillsDest := filepath.Join(claudeHome, "skills")
		result.Skills = relinkDir(skillsSrc, skillsDest, &result.Warnings)

		// 6. Refresh agent symlinks.
		agentsSrc := filepath.Join(installRoot, "claude", "agents")
		agentsDest := filepath.Join(claudeHome, "agents")
		result.Agents = relinkDir(agentsSrc, agentsDest, &result.Warnings)

		// 7. Optional repo operations.
		if len(args) > 0 {
			repoPath := args[0]

			if _, err := wip.EnsureRepo(repoPath); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("ensure-repo: %v", err))
			} else {
				result.RepoPath = repoPath
			}

			migrateResult, err := wip.MigrateWip(repoPath)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("migrate-wip: %v", err))
			} else {
				result.WipMigrated = migrateResult.Migrated
				result.Warnings = append(result.Warnings, migrateResult.Warnings...)
			}
		}

		PrintJSON(result)
	},
}

// relinkDir creates or refreshes symlinks in destDir pointing at each entry in
// srcDir. Returns a list of linked names. Errors are appended to warnings.
func relinkDir(srcDir, destDir string, warnings *[]string) []string {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		*warnings = append(*warnings, fmt.Sprintf("mkdir %s: %v", destDir, err))
		return nil
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("read %s: %v", srcDir, err))
		return nil
	}

	var linked []string
	for _, e := range entries {
		src := filepath.Join(srcDir, e.Name())
		dest := filepath.Join(destDir, e.Name())
		if err := safeSymlink(src, dest); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("link %s: %v", e.Name(), err))
			continue
		}
		linked = append(linked, e.Name())
	}
	return linked
}

// safeSymlink creates or updates a symlink at dest pointing to src.
// If dest already points to src it is a no-op. If dest is a stale symlink it
// is updated. If dest is a regular file or directory (not a symlink) an error
// is returned to avoid overwriting user-managed files.
func safeSymlink(src, dest string) error {
	info, err := os.Lstat(dest)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			current, err := os.Readlink(dest)
			if err != nil {
				return fmt.Errorf("readlink %s: %w", dest, err)
			}
			if current == src {
				return nil // already correct
			}
			// Stale symlink — update it.
			if err := os.Remove(dest); err != nil {
				return fmt.Errorf("remove stale symlink: %w", err)
			}
		} else {
			return fmt.Errorf("%s exists and is not a symlink managed by ai-dev-workflow", dest)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("lstat: %w", err)
	}
	return os.Symlink(src, dest)
}

func init() {
	Root.AddCommand(upgradeCmd)
}
