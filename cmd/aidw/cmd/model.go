package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/config"
)

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Manage model routing and tiering",
}

var modelRouteCmd = &cobra.Command{
	Use:   "route <tier>",
	Short: "Get the routed model name for a specific tier (frontier|efficient)",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		tier := args[0]
		cfg := config.Load()

		switch tier {
		case "frontier":
			printRoutedModel(cfg.FrontierModel, "AIDW_FRONTIER_MODEL")
		case "efficient":
			printRoutedModel(cfg.EfficientModel, "AIDW_EFFICIENT_MODEL")
		default:
			fmt.Fprintf(os.Stderr, "unknown tier %q — valid values: frontier, efficient\n", tier)
			os.Exit(1)
		}
	},
}

// printRoutedModel writes the resolved model name to stdout. Tiers are
// unset by default (the right model depends on the host), so when nothing
// is configured stdout stays empty — keeping `$(aidw model route <tier>)`
// captures honest — and the explanation goes to stderr. Exit status stays
// 0: "not configured" is a valid state, not a failure.
func printRoutedModel(model, envVar string) {
	if model == "" {
		fmt.Fprintf(os.Stderr, "(not configured — set %s)\n", envVar)
		return
	}
	fmt.Println(model)
}

func init() {
	modelCmd.AddCommand(modelRouteCmd)
	Root.AddCommand(modelCmd)
}
