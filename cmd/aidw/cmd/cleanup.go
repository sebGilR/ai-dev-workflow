package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var cleanupBranchCmd = &cobra.Command{
	Use:   "cleanup-branch <path>",
	Short: "Remove all files in the current branch .wip dir except context.md, pr.md, spec.md and task-context.md",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		dryRun, _ := c.Flags().GetBool("dry-run")
		result, err := wip.CleanupBranch(args[0], dryRun)
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
		dryRun, _ := c.Flags().GetBool("dry-run")
		result, err := wip.ClearWip(args[0], dryRun)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

var clearOthersCmd = &cobra.Command{
	Use:   "clear-others <path>",
	Short: "Delete all .wip branch dirs except the current branch's dir",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		dryRun, _ := c.Flags().GetBool("dry-run")
		result, err := wip.ClearOtherBranches(args[0], dryRun)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

func init() {
	cleanupBranchCmd.Flags().Bool("dry-run", false, "Show what would be deleted without actually deleting")
	clearWipCmd.Flags().Bool("dry-run", false, "Show what would be deleted without actually deleting")
	clearOthersCmd.Flags().Bool("dry-run", false, "Show what would be deleted without actually deleting")

	Root.AddCommand(cleanupBranchCmd)
	Root.AddCommand(clearWipCmd)
	Root.AddCommand(clearOthersCmd)
}
