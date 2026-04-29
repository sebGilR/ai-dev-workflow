package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the current version of aidw",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("aidw version %s\n", Version)
	},
}

func init() {
	Root.AddCommand(versionCmd)
}
