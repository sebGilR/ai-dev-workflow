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
			fmt.Println(cfg.FrontierModel)
		case "efficient":
			fmt.Println(cfg.EfficientModel)
		default:
			fmt.Fprintf(os.Stderr, "unknown tier %q — valid values: frontier, efficient\n", tier)
			os.Exit(1)
		}
	},
}

func init() {
	modelCmd.AddCommand(modelRouteCmd)
	Root.AddCommand(modelCmd)
}
