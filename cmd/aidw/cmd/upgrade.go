package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/install"
	"aidw/cmd/aidw/internal/wip"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade [path]",
	Short: "Re-apply global configs, refresh skills/agents, and migrate legacy .wip dirs",
	Long: `upgrade re-applies all global configuration merges (CLAUDE.md managed block,
settings.json permissions, mcp.json MCP servers, global gitignore), refreshes the
embedded skills and agents under ~/.claude/, and optionally migrates legacy un-timestamped
.wip/<slug> directories to the YYYYMMDD-<slug> format.

Pass a repo path to also run ensure-repo and migrate-wip for that directory.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(c *cobra.Command, args []string) {
		repoPath := ""
		if len(args) > 0 {
			repoPath = args[0]
		}

		result, err := install.Bootstrap(repoPath, os.Stderr)
		if err != nil {
			Die("upgrade: %v", err)
		}

		// WIP migration is specific to the upgrade command
		if repoPath != "" {
			migrateResult, err := wip.MigrateWip(repoPath)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("migrate-wip: %v", err))
			} else {
				result.Warnings = append(result.Warnings, migrateResult.Warnings...)
			}
		}

		PrintJSON(result)
	},
}
func init() {
	Root.AddCommand(upgradeCmd)
}

