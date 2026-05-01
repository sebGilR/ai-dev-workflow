package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/install"
)

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap [path]",
	Short: "Initialize the global aidw environment and optionally bootstrap a repository",
	Args:  cobra.MaximumNArgs(1),
	Run: func(c *cobra.Command, args []string) {
		repoPath := ""
		if len(args) > 0 {
			repoPath = args[0]
		}

		sourcePath, _ := c.Flags().GetString("source-path")
		interactive, _ := c.Flags().GetBool("interactive")
		setupShell, _ := c.Flags().GetBool("setup-shell")

		opts := install.BootstrapOptions{
			RepoPath:    repoPath,
			SourcePath:  sourcePath,
			Interactive: interactive,
			SetupShell:  setupShell,
		}

		result, err := install.Bootstrap(opts, os.Stderr)
		if err != nil {
			Die("bootstrap: %v", err)
		}
		PrintJSON(result)
	},
}

func init() {
	bootstrapCmd.Flags().String("source-path", "", "If provided, symlink skills/agents from this repo")
	bootstrapCmd.Flags().Bool("interactive", false, "Prompt for optional features (Adversarial Review, RTK)")
	bootstrapCmd.Flags().Bool("setup-shell", false, "Patch shell profile and create aidw.env.sh")
	Root.AddCommand(bootstrapCmd)
}
