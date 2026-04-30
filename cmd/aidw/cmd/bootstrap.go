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

		result, err := install.Bootstrap(repoPath, os.Stderr)
		if err != nil {
			Die("bootstrap: %v", err)
		}
		PrintJSON(result)
	},
}

func init() {
	Root.AddCommand(bootstrapCmd)
}
