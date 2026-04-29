package cmd

import (
	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var nextCmd = &cobra.Command{
	Use:   "next <path>",
	Short: "Show the recommended next step for the current branch",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		next, err := wip.GetNextAction(args[0])
		if err != nil {
			Die("get-next: %v", err)
		}
		PrintJSON(next)
	},
}

func init() {
	Root.AddCommand(nextCmd)
}
