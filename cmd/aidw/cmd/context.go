package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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
	Short: "Print context-summary.md to stdout, or its staleness as JSON with --json",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		asJSON, _ := c.Flags().GetBool("json")

		if asJSON {
			staleness, err := wip.CheckSummaryStaleness(args[0])
			if err != nil {
				fmt.Fprintln(os.Stderr, "[aidw]", err)
				os.Exit(1)
			}
			PrintJSON(staleness)
			return
		}

		state, err := wip.FindBranchState(args[0], "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		summaryPath := filepath.Join(state.WipDir, "context-summary.md")
		data, err := os.ReadFile(summaryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw] No context-summary.md found. Run: aidw summarize-context <path>")
			os.Exit(1)
		}
		fmt.Print(string(data))
	},
}

func init() {
	contextSummaryCmd.Flags().Bool("json", false, "Print {stale, generated_at, summary_path} instead of the raw summary")
	Root.AddCommand(summarizeContextCmd)
	Root.AddCommand(contextSummaryCmd)
}
