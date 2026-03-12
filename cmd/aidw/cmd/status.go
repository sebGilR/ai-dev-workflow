package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var statusCmd = &cobra.Command{
	Use:   "status <path>",
	Short: "Show current branch workflow status",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		out, err := wip.SummarizeStatus(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		fmt.Print(out)
	},
}

func init() {
	Root.AddCommand(statusCmd)
}
