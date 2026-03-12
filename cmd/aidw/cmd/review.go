package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/review"
	"aidw/cmd/aidw/internal/util"
)

var reviewBundleCmd = &cobra.Command{
	Use:   "review-bundle <path>",
	Short: "Build a review bundle from the current diff",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		bundle, err := review.ReviewBundle(args[0])
		if err != nil {
			Die("%v", err)
		}
		PrintJSON(bundle)
	},
}

var synthesizeReviewCmd = &cobra.Command{
	Use:   "synthesize-review <path>",
	Short: "Merge review sources into review.md",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		result, err := review.SynthesizeReview(args[0])
		if err != nil {
			Die("%v", err)
		}
		PrintJSON(result)
	},
}

var geminiReviewCmd = &cobra.Command{
	Use:   "gemini-review <path>",
	Short: "Run adversarial Gemini review pass",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		geminiReview := os.Getenv("AIDW_GEMINI_REVIEW")
		if geminiReview != "1" {
			fmt.Fprintln(os.Stderr, "[aidw] Gemini adversarial review disabled (AIDW_GEMINI_REVIEW != 1).")
			os.Exit(0)
		}
		model, _ := c.Flags().GetString("model")
		timeout, _ := c.Flags().GetInt("timeout")
		timeout = util.ClampInt(timeout, 10, 600)

		result, err := review.GeminiReview(args[0], model, timeout)
		if err != nil {
			Die("%v", err)
		}
		status := result.Status
		switch status {
		case "ok":
			fmt.Fprintln(os.Stderr, "Gemini adversarial review complete.")
		case "skipped", "empty":
			fmt.Fprintf(os.Stderr, "[aidw] Gemini adversarial review %s: %s\n", status, result.Reason)
		default:
			fmt.Fprintf(os.Stderr, "[aidw] Gemini adversarial review failed: %v\n", result)
			os.Exit(1)
		}
	},
}

func init() {
	geminiReviewCmd.Flags().String("model", "gemini-2.5-pro", "Gemini model to use")
	geminiReviewCmd.Flags().Int("timeout", 120, "Timeout in seconds")
	Root.AddCommand(reviewBundleCmd)
	Root.AddCommand(synthesizeReviewCmd)
	Root.AddCommand(geminiReviewCmd)
}
