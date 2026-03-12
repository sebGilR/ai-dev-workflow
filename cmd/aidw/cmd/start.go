package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var startCmd = &cobra.Command{
	Use:   "start <path>",
	Short: "Initialize branch state in .wip/<branch>/",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		branch, _ := c.Flags().GetString("branch")
		state, err := wip.EnsureBranchState(args[0], branch)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(map[string]any{
			"repo":    state.Repo,
			"branch":  state.Branch,
			"wip_dir": state.WipDir,
			"status":  state.Status,
		})
	},
}

func init() {
	startCmd.Flags().String("branch", "", "Branch name override")
	Root.AddCommand(startCmd)
}
