package cmd

import (
	"sort"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/memory"
	"aidw/cmd/aidw/internal/wip"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage persistent task memory and facts",
}

var memoryStoreCmd = &cobra.Command{
	Use:   "store <path> <key> <value>",
	Short: "Store a persistent fact for the current branch",
	Args:  cobra.ExactArgs(3),
	Run: func(c *cobra.Command, args []string) {
		repoPath := args[0]
		key := args[1]
		val := args[2]

		state, err := wip.EnsureBranchState(repoPath, "")
		if err != nil {
			Die("wip state: %v", err)
		}

		db, err := memory.Open()
		if err != nil {
			Die("memory db: %v", err)
		}
		defer db.Close()

		if err := db.StoreFact(state.Repo, state.Branch, key, val); err != nil {
			Die("store: %v", err)
		}

		PrintJSON(map[string]any{
			"status": "stored",
			"key":    key,
			"branch": state.Branch,
		})
	},
}

var memoryListCmd = &cobra.Command{
	Use:   "list <path>",
	Short: "List all persistent facts for the current branch",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		repoPath := args[0]

		state, err := wip.EnsureBranchState(repoPath, "")
		if err != nil {
			Die("wip state: %v", err)
		}

		db, err := memory.Open()
		if err != nil {
			Die("memory db: %v", err)
		}
		defer db.Close()

		facts, err := db.ListFacts(state.Repo, state.Branch)
		if err != nil {
			Die("list: %v", err)
		}

		// Sort keys for stable output
		keys := make([]string, 0, len(facts))
		for k := range facts {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		type fact struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		out := make([]fact, 0, len(keys))
		for _, k := range keys {
			out = append(out, fact{Key: k, Value: facts[k]})
		}

		PrintJSON(map[string]any{
			"branch": state.Branch,
			"facts":  out,
		})
	},
}

func init() {
	memoryCmd.AddCommand(memoryStoreCmd)
	memoryCmd.AddCommand(memoryListCmd)
	Root.AddCommand(memoryCmd)
}
