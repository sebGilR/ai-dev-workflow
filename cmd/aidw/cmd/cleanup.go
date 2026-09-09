package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var cleanupBranchCmd = &cobra.Command{
	Use:   "cleanup-branch <path>",
	Short: "Archive all files in the current branch .wip dir except context.md, pr.md, spec.md, task-context.md and status.json",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		dryRun, _ := c.Flags().GetBool("dry-run")
		purge, _ := c.Flags().GetBool("purge")
		result, err := wip.CleanupBranch(args[0], dryRun, purge)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

var clearWipCmd = &cobra.Command{
	Use:   "clear-wip <path>",
	Short: "Archive all .wip branch dirs except the most recently dated one",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		dryRun, _ := c.Flags().GetBool("dry-run")
		purge, _ := c.Flags().GetBool("purge")
		result, err := wip.ClearWip(args[0], dryRun, purge)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

var clearOthersCmd = &cobra.Command{
	Use:   "clear-others <path>",
	Short: "Archive all .wip branch dirs except the current branch's dir",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		dryRun, _ := c.Flags().GetBool("dry-run")
		purge, _ := c.Flags().GetBool("purge")
		result, err := wip.ClearOtherBranches(args[0], dryRun, purge)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

func init() {
	cleanupBranchCmd.Flags().Bool("dry-run", false, "Show what would be archived/deleted without actually doing it")
	clearWipCmd.Flags().Bool("dry-run", false, "Show what would be archived/deleted without actually doing it")
	clearOthersCmd.Flags().Bool("dry-run", false, "Show what would be archived/deleted without actually doing it")

	purgeHelp := "Permanently delete current non-kept entries AND previously archived content, instead of archiving"
	cleanupBranchCmd.Flags().Bool("purge", false, purgeHelp)
	clearWipCmd.Flags().Bool("purge", false, purgeHelp)
	clearOthersCmd.Flags().Bool("purge", false, purgeHelp)

	Root.AddCommand(cleanupBranchCmd)
	Root.AddCommand(clearWipCmd)
	Root.AddCommand(clearOthersCmd)
}
