package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var setStageCmd = &cobra.Command{
	Use:   "set-stage <path> <stage>",
	Short: "Update the workflow stage",
	Args:  cobra.ExactArgs(2),
	Run: func(c *cobra.Command, args []string) {
		skip, _ := c.Flags().GetBool("skip-verification")
		status, err := wip.SetStage(args[0], args[1], skip)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(status)
	},
}

func init() {
	setStageCmd.Flags().Bool("skip-verification", false, "Skip file existence verification")
	Root.AddCommand(setStageCmd)
}
