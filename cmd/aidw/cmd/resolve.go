package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

// resolveWipCmd is a lookup-only resolver: it never creates a .wip
// directory, a repo bootstrap, or any seeded files. It exists so shell
// scripts (notably save-wip-snapshot.sh) and other callers can find the
// current branch's WIP directory — if one already exists — without the
// side effects of EnsureBranchState. Output is line-oriented (not JSON) so
// it can be parsed with `. <(aidw resolve-wip .)`-style sourcing or simple
// `key=value` grep/cut in bash, with no dependency on jq being installed.
var resolveWipCmd = &cobra.Command{
	Use:   "resolve-wip <path>",
	Short: "Print the current branch's WIP directory, if any, without creating one",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		state, err := wip.FindBranchState(args[0], "")
		if err != nil {
			if errors.Is(err, wip.ErrNoActiveWork) {
				fmt.Println("exists=false")
				fmt.Println("wip_dir=")
				os.Exit(0)
			}
			// A real error (e.g. not a git repo) — exit 1 so callers can
			// distinguish "no active work" from "couldn't even check".
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		fmt.Println("exists=true")
		fmt.Printf("wip_dir=%s\n", state.WipDir)
	},
}

func init() {
	Root.AddCommand(resolveWipCmd)
}
