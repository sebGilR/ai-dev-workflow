package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/verify"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify installation and configuration",
	Run: func(c *cobra.Command, args []string) {
		workspace, _ := c.Flags().GetString("workspace")
		results := verify.Run(workspace)

		icons := map[string]string{"pass": "+", "FAIL": "!", "warn": "~"}
		for _, ch := range results.Checks {
			icon := icons[ch.Status]
			detail := ""
			if ch.Detail != "" {
				detail = fmt.Sprintf("  (%s)", ch.Detail)
			}
			fmt.Printf("[%s] %s%s\n", icon, ch.Name, detail)
		}

		if !results.OK {
			os.Exit(1)
		}
	},
}

func init() {
	verifyCmd.Flags().String("workspace", "", "Optional workspace path to also check repos")
	Root.AddCommand(verifyCmd)
}
