package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var bootstrapWorkspaceCmd = &cobra.Command{
	Use:   "bootstrap-workspace <path>",
	Short: "Bootstrap all repos in a workspace",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		result, err := wip.BootstrapWorkspace(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

var ensureRepoCmd = &cobra.Command{
	Use:   "ensure-repo <path>",
	Short: "Bootstrap a single repo",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		result, err := wip.EnsureRepo(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

func init() {
	Root.AddCommand(bootstrapWorkspaceCmd)
	Root.AddCommand(ensureRepoCmd)
}
