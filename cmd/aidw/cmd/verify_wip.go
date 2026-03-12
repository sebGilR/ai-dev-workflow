package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

func makeVerifyCmd(use, short, filename string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <path>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		Run: func(c *cobra.Command, args []string) {
			state, err := wip.EnsureBranchState(args[0], "")
			if err != nil {
				fmt.Fprintln(os.Stderr, "[aidw]", err)
				os.Exit(1)
			}
			ok, msg := wip.VerifyWipFile(state.WipDir, filename)
			result := map[string]any{
				"file":     filename,
				"wip_dir":  state.WipDir,
				"verified": ok,
			}
			if !ok {
				result["error"] = msg
			}
			PrintJSON(result)
			if !ok {
				os.Exit(1)
			}
		},
	}
}

var verifyPlanCmd = makeVerifyCmd("verify-plan", "Verify plan.md exists and has content", "plan.md")
var verifyResearchCmd = makeVerifyCmd("verify-research", "Verify research.md exists and has content", "research.md")
var verifyReviewCmd = makeVerifyCmd("verify-review", "Verify review.md exists and has content", "review.md")

func init() {
	Root.AddCommand(verifyPlanCmd)
	Root.AddCommand(verifyResearchCmd)
	Root.AddCommand(verifyReviewCmd)
}
