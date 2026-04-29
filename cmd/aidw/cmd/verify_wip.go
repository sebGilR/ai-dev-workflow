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
			type VerifyResult struct {
				File     string `json:"file"`
				WipDir   string `json:"wip_dir"`
				Verified bool   `json:"verified"`
				Error    string `json:"error,omitempty"`
			}
			result := VerifyResult{
				File:     filename,
				WipDir:   state.WipDir,
				Verified: ok,
			}
			if !ok {
				result.Error = msg
			}
			PrintJSON(result)
			if !ok {
				os.Exit(1)
			}
		},
	}
}

var verifyPlanCmd = makeVerifyCmd("verify-plan", "Verify plan.md exists and has content", "plan.md")
var verifySpecCmd = makeVerifyCmd("verify-spec", "Verify spec.md exists and has content", "spec.md")
var verifyTaskContextCmd = makeVerifyCmd("verify-task-context", "Verify task-context.md exists and has content", "task-context.md")
var verifyResearchCmd = makeVerifyCmd("verify-research", "Verify research.md exists and has content", "research.md")
var verifyReviewCmd = makeVerifyCmd("verify-review", "Verify review.md exists and has content", "review.md")

var verifyWipFileCmd = &cobra.Command{
	Use:   "verify-wip-file <path> <filename>",
	Short: "Verify a specific file in the WIP directory exists and has content",
	Args:  cobra.ExactArgs(2),
	Run: func(c *cobra.Command, args []string) {
		state, err := wip.EnsureBranchState(args[0], "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		filename := args[1]
		ok, msg := wip.VerifyWipFile(state.WipDir, filename)
		type VerifyResult struct {
			File     string `json:"file"`
			WipDir   string `json:"wip_dir"`
			Verified bool   `json:"verified"`
			Error    string `json:"error,omitempty"`
		}
		result := VerifyResult{
			File:     filename,
			WipDir:   state.WipDir,
			Verified: ok,
		}
		if !ok {
			result.Error = msg
		}
		PrintJSON(result)
		if !ok {
			os.Exit(1)
		}
	},
}

func init() {
	Root.AddCommand(verifyPlanCmd)
	Root.AddCommand(verifySpecCmd)
	Root.AddCommand(verifyTaskContextCmd)
	Root.AddCommand(verifyResearchCmd)
	Root.AddCommand(verifyReviewCmd)
	Root.AddCommand(verifyWipFileCmd)
}
