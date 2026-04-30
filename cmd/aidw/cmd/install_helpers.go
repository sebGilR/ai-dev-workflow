package cmd

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/install"
	embedfs "aidw"
)

func init() {
	Root.AddCommand(mergeCLAUDEMdCmd)
	Root.AddCommand(mergeSettingsCmd)
	Root.AddCommand(mergeMCPJSONCmd)
	Root.AddCommand(updateGlobalGitignoreCmd)
	Root.AddCommand(generateGithubAgentsCmd)

	mergeCLAUDEMdCmd.Flags().String("claude-md", "", "Path to CLAUDE.md")
	mergeCLAUDEMdCmd.Flags().String("snippet", "", "Path to snippet file (optional, defaults to embedded)")
	mergeSettingsCmd.Flags().String("settings", "", "Path to settings.json")
	mergeSettingsCmd.Flags().String("template", "", "Path to template JSON (optional, defaults to embedded)")
	updateGlobalGitignoreCmd.Flags().StringArray("add", nil, "Extra entries to add to the global gitignore")
	generateGithubAgentsCmd.Flags().String("src", "", "Source directory containing agent markdown files (optional, defaults to embedded)")
	generateGithubAgentsCmd.Flags().String("dest", "", "Destination directory for generated agents")
}

var generateGithubAgentsCmd = &cobra.Command{
	Use:   "generate-github-agents",
	Short: "Generate .github/agents/ from claude/agents/ stripping MCP sections",
	RunE: func(cmd *cobra.Command, args []string) error {
		srcPath, _ := cmd.Flags().GetString("src")
		dest, _ := cmd.Flags().GetString("dest")
		if dest == "" {
			return fmt.Errorf("--dest is required")
		}

		var srcFS fs.FS
		var err error
		if srcPath != "" {
			srcFS = os.DirFS(srcPath)
		} else {
			srcFS, err = fs.Sub(embedfs.FS, "claude/agents")
			if err != nil {
				return fmt.Errorf("embedded agents: %w", err)
			}
		}

		if err := install.GenerateGithubAgents(srcFS, dest); err != nil {
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
		snippetPath, _ := cmd.Flags().GetString("snippet")
		if claudeMD == "" {
			return fmt.Errorf("--claude-md is required")
		}

		var snippetBytes []byte
		var err error
		if snippetPath != "" {
			snippetBytes, err = os.ReadFile(snippetPath)
		} else {
			snippetBytes, err = embedfs.FS.ReadFile("templates/global/claude_managed_block.md")
		}

		if err != nil {
			return fmt.Errorf("read snippet: %w", err)
		}

		if err := install.MergeCLAUDEMd(claudeMD, snippetBytes); err != nil {
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
		tmplPath, _ := cmd.Flags().GetString("template")
		if settings == "" {
			return fmt.Errorf("--settings is required")
		}

		var tmplBytes []byte
		var err error
		if tmplPath != "" {
			tmplBytes, err = os.ReadFile(tmplPath)
		} else {
			tmplBytes, err = embedfs.FS.ReadFile("templates/global/settings.template.json")
		}

		if err != nil {
			return fmt.Errorf("read template: %w", err)
		}

		if err := install.MergeSettings(settings, tmplBytes); err != nil {
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
