package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var cleanupBranchCmd = &cobra.Command{
	Use:   "cleanup-branch <path>",
	Short: "Remove all files in the current branch .wip dir except context.md and pr.md",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		result, err := wip.CleanupBranch(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

var clearWipCmd = &cobra.Command{
	Use:   "clear-wip <path>",
	Short: "Delete all .wip branch dirs except the most recently dated one",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		result, err := wip.ClearWip(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

func init() {
	Root.AddCommand(cleanupBranchCmd)
	Root.AddCommand(clearWipCmd)
}
