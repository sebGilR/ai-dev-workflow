package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/policy"
)

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage local agentic command policies",
}

var policyInitCmd = &cobra.Command{
	Use:   "init <path>",
	Short: "Initialize a default policy in the repository",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		if err := policy.Init(args[0]); err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Initialized default policy in .aidw/policy.json")
	},
}

var policyCheckCmd = &cobra.Command{
	Use:   "check <path> <command>",
	Short: "Evaluate a command against the local policy",
	Args:  cobra.ExactArgs(2),
	Run: func(c *cobra.Command, args []string) {
		repoPath := args[0]
		cmdStr := args[1]

		cfg, err := policy.Load(repoPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}

		verdict := cfg.Evaluate(cmdStr)
		PrintJSON(verdict)
	},
}

func init() {
	policyCmd.AddCommand(policyInitCmd)
	policyCmd.AddCommand(policyCheckCmd)
	Root.AddCommand(policyCmd)
}
