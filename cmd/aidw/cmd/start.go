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
		type StartResult struct {
			Repo   string     `json:"repo"`
			Branch string     `json:"branch"`
			WipDir string     `json:"wip_dir"`
			Status wip.Status `json:"status"`
		}
		PrintJSON(StartResult{
			Repo:   state.Repo,
			Branch: state.Branch,
			WipDir: state.WipDir,
			Status: state.Status,
		})
	},
}

func init() {
	startCmd.Flags().String("branch", "", "Branch name override")
	Root.AddCommand(startCmd)
}
