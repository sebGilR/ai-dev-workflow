package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var summarizeContextCmd = &cobra.Command{
	Use:   "summarize-context <path>",
	Short: "Generate context-summary.md from all WIP files",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		result, err := wip.WriteContextSummary(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		PrintJSON(result)
	},
}

var contextSummaryCmd = &cobra.Command{
	Use:   "context-summary <path>",
	Short: "Print context-summary.md to stdout",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		state, err := wip.EnsureBranchState(args[0], "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		summaryPath := state.WipDir + "/context-summary.md"
		data, err := os.ReadFile(summaryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw] No context-summary.md found. Run: aidw summarize-context <path>")
			os.Exit(1)
		}
		fmt.Print(string(data))
	},
}

func init() {
	Root.AddCommand(summarizeContextCmd)
	Root.AddCommand(contextSummaryCmd)
}
