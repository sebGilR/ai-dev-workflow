package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/install"
)

var documentProjectCmd = &cobra.Command{
	Use:   "document-project <path>",
	Short: "Generate/Update project documentation in .claude/repo-docs/",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		if err := install.DocumentProject(args[0]); err != nil {
			fmt.Fprintln(os.Stderr, "document-project:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Project documentation updated in .claude/repo-docs/")
	},
}

func init() {
	Root.AddCommand(documentProjectCmd)
}
