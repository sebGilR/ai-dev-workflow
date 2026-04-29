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

var policyAllowCmd = &cobra.Command{
	Use:   "allow <path> <command>",
	Short: "Permanently whitelist a specific command pattern",
	Args:  cobra.ExactArgs(2),
	Run: func(c *cobra.Command, args []string) {
		repoPath := args[0]
		cmdStr := args[1]
		reason, _ := c.Flags().GetString("reason")

		if err := policy.AddRule(repoPath, cmdStr, reason); err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Command permanently whitelisted: %s\n", cmdStr)
	},
}

func init() {
	policyCmd.AddCommand(policyInitCmd)
	policyCmd.AddCommand(policyCheckCmd)
	policyCmd.AddCommand(policyAllowCmd)

	policyAllowCmd.Flags().String("reason", "User authorized always", "Reason for whitelisting")

	Root.AddCommand(policyCmd)
}
