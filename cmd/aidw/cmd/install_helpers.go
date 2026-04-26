package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/install"
)

func init() {
	Root.AddCommand(mergeCLAUDEMdCmd)
	Root.AddCommand(mergeSettingsCmd)
	Root.AddCommand(mergeMCPJSONCmd)
	Root.AddCommand(updateGlobalGitignoreCmd)
	Root.AddCommand(generateGithubAgentsCmd)

	mergeCLAUDEMdCmd.Flags().String("claude-md", "", "Path to CLAUDE.md")
	mergeCLAUDEMdCmd.Flags().String("snippet", "", "Path to snippet file")
	mergeSettingsCmd.Flags().String("settings", "", "Path to settings.json")
	mergeSettingsCmd.Flags().String("template", "", "Path to template JSON")
	updateGlobalGitignoreCmd.Flags().StringArray("add", nil, "Extra entries to add to the global gitignore")
	generateGithubAgentsCmd.Flags().String("src", "", "Source directory containing agent markdown files")
	generateGithubAgentsCmd.Flags().String("dest", "", "Destination directory for generated agents")
}

var generateGithubAgentsCmd = &cobra.Command{
	Use:   "generate-github-agents",
	Short: "Generate .github/agents/ from claude/agents/ stripping MCP sections",
	RunE: func(cmd *cobra.Command, args []string) error {
		src, _ := cmd.Flags().GetString("src")
		dest, _ := cmd.Flags().GetString("dest")
		if src == "" || dest == "" {
			return fmt.Errorf("--src and --dest are required")
		}
		if err := install.GenerateGithubAgents(src, dest); err != nil {
			fmt.Fprintln(os.Stderr, "generate-github-agents:", err)
			os.Exit(1)
		}
		return nil
	},
}

var mergeCLAUDEMdCmd = &cobra.Command{
	Use:   "merge-claude-md",
	Short: "Insert or replace the managed block in CLAUDE.md",
	RunE: func(cmd *cobra.Command, args []string) error {
		claudeMD, _ := cmd.Flags().GetString("claude-md")
		snippet, _ := cmd.Flags().GetString("snippet")
		if claudeMD == "" || snippet == "" {
			return fmt.Errorf("--claude-md and --snippet are required")
		}
		if err := install.MergeCLAUDEMd(claudeMD, snippet); err != nil {
			fmt.Fprintln(os.Stderr, "merge-claude-md:", err)
			os.Exit(1)
		}
		return nil
	},
}

var mergeSettingsCmd = &cobra.Command{
	Use:   "merge-settings",
	Short: "Deep-merge a JSON template into settings.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		settings, _ := cmd.Flags().GetString("settings")
		tmpl, _ := cmd.Flags().GetString("template")
		if settings == "" || tmpl == "" {
			return fmt.Errorf("--settings and --template are required")
		}
		if err := install.MergeSettings(settings, tmpl); err != nil {
			fmt.Fprintln(os.Stderr, "merge-settings:", err)
			os.Exit(1)
		}
		return nil
	},
}

var mergeMCPJSONCmd = &cobra.Command{
	Use:   "merge-mcp-json",
	Short: "Add or update Serena and Context7 in ~/.claude/mcp.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := install.MergeMCPJSON(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "merge-mcp-json:", err)
			os.Exit(1)
		}
		return nil
	},
}

var updateGlobalGitignoreCmd = &cobra.Command{
	Use:   "update-global-gitignore",
	Short: "Add managed lines to the global gitignore",
	RunE: func(cmd *cobra.Command, args []string) error {
		extra, _ := cmd.Flags().GetStringArray("add")
		if err := install.UpdateGlobalGitignore(extra...); err != nil {
			fmt.Fprintln(os.Stderr, "update-global-gitignore:", err)
			os.Exit(1)
		}
		return nil
	},
}
