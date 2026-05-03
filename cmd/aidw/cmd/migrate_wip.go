package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var migrateWipCmd = &cobra.Command{
	Use:   "migrate-wip <path>",
	Short: "Rename legacy un-timestamped .wip/<slug> dirs to YYYYMMDDHHMMSS-<slug> format",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		result, err := wip.MigrateWip(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

func init() {
	Root.AddCommand(migrateWipCmd)
}
