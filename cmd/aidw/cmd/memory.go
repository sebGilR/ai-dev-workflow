package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/memory"
	"aidw/cmd/aidw/internal/wip"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage persistent task memory and facts",
}

var memoryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the health of the memory layer",
	Run: func(c *cobra.Command, args []string) {
		db, err := memory.Open()
		if err != nil {
			Die("memory db: %v", err)
		}
		defer db.Close()
		PrintJSON(db.Status())
	},
}

var memoryStoreCmd = &cobra.Command{
	Use:   "store <path> <key> <value>",
	Short: "Store a persistent fact for the current branch",
	Args:  cobra.ExactArgs(3),
	Run: func(c *cobra.Command, args []string) {
		repoPath := args[0]
		key := args[1]
		val := args[2]
		semantic, _ := c.Flags().GetBool("semantic")

		state, err := wip.EnsureBranchState(repoPath, "")
		if err != nil {
			Die("wip state: %v", err)
		}

		db, err := memory.Open()
		if err != nil {
			Die("memory db: %v", err)
		}
		defer db.Close()

		var emb []float32
		if semantic {
			client, err := memory.NewEmbeddingClient()
			if err != nil {
				Die("embedding client: %v", err)
			}
			emb, err = client.Embed(fmt.Sprintf("%s: %s", key, val))
			if err != nil {
				Die("embed: %v", err)
			}
		}

		if err := db.StoreFact(state.Repo, state.Branch, key, val, emb); err != nil {
			Die("store: %v", err)
		}

		PrintJSON(map[string]any{
			"status":   "stored",
			"key":      key,
			"branch":   state.Branch,
			"semantic": semantic,
		})
	},
}

var memoryListCmd = &cobra.Command{
	Use:   "list [path]",
	Short: "List all persistent facts. Defaults to current branch.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(c *cobra.Command, args []string) {
		isGlobal, _ := c.Flags().GetBool("global")
		
		var repoPath, repoName, branch string
		if !isGlobal {
			if len(args) == 0 {
				Die("repo path is required for local listing")
			}
			repoPath = args[0]
			state, err := wip.EnsureBranchState(repoPath, "")
			if err != nil {
				Die("wip state: %v", err)
			}
			repoName = state.Repo
			branch = state.Branch
		}

		db, err := memory.Open()
		if err != nil {
			Die("memory db: %v", err)
		}
		defer db.Close()

		facts, err := db.ListFacts(repoName, branch)
		if err != nil {
			Die("list: %v", err)
		}

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
			"global": isGlobal,
			"branch": branch,
			"facts":  out,
		})
	},
}

var memoryIndexCmd = &cobra.Command{
	Use:   "index <repo_path> [target_path]",
	Short: "Index documentation for semantic search",
	Args:  cobra.MinimumNArgs(1),
	Run: func(c *cobra.Command, args []string) {
		repoPath := args[0]
		target := repoPath
		if len(args) > 1 {
			target = args[1]
		}

		state, err := wip.EnsureBranchState(repoPath, "")
		if err != nil {
			Die("wip state: %v", err)
		}

		db, err := memory.Open()
		if err != nil {
			Die("memory db: %v", err)
		}
		defer db.Close()

		if !db.VectorEnabled() {
			Die("vector extension not loaded — semantic search is unavailable")
		}

		client, err := memory.NewEmbeddingClient()
		if err != nil {
			Die("embedding client: %v", err)
		}

		var count int
		err = filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
				return nil
			}

			data, _ := os.ReadFile(path)
			content := string(data)
			if len(content) < 10 {
				return nil
			}

			relPath, _ := filepath.Rel(state.Repo, path)
			emb, err := client.Embed(content)
			if err != nil {
				return fmt.Errorf("embed %s: %w", relPath, err)
			}

			if err := db.IndexItem(state.Repo, relPath, content, emb); err != nil {
				return fmt.Errorf("store %s: %w", relPath, err)
			}
			count++
			return nil
		})

		if err != nil {
			Die("walk: %v", err)
		}

		PrintJSON(map[string]any{
			"status":        "indexed",
			"files_indexed": count,
		})
	},
}

var memorySearchCmd = &cobra.Command{
	Use:   "search [path] <query>",
	Short: "Perform semantic search over project knowledge",
	Args:  cobra.MinimumNArgs(1),
	Run: func(c *cobra.Command, args []string) {
		isGlobal, _ := c.Flags().GetBool("global")
		var repoPath, query string
		
		if isGlobal {
			query = args[0]
		} else {
			if len(args) < 2 {
				Die("repo path and query are required for local search")
			}
			repoPath = args[0]
			query = args[1]
			state, err := wip.EnsureBranchState(repoPath, "")
			if err != nil {
				Die("wip state: %v", err)
			}
			repoPath = state.Repo
		}

		db, err := memory.Open()
		if err != nil {
			Die("memory db: %v", err)
		}
		defer db.Close()

		client, err := memory.NewEmbeddingClient()
		if err != nil {
			Die("embedding client: %v", err)
		}

		queryEmb, err := client.Embed(query)
		if err != nil {
			Die("embed query: %v", err)
		}

		results, err := db.Search(repoPath, queryEmb, 5)
		if err != nil {
			Die("search: %v", err)
		}

		PrintJSON(map[string]any{
			"query":   query,
			"global":  isGlobal,
			"results": results,
		})
	},
}

func init() {
	memoryCmd.AddCommand(memoryStatusCmd)
	memoryCmd.AddCommand(memoryStoreCmd)
	memoryCmd.AddCommand(memoryListCmd)
	memoryCmd.AddCommand(memoryIndexCmd)
	memoryCmd.AddCommand(memorySearchCmd)

	memoryStoreCmd.Flags().Bool("semantic", false, "Index the fact for semantic search")
	memoryListCmd.Flags().Bool("global", false, "List facts from all repositories")
	memorySearchCmd.Flags().Bool("global", false, "Search across all repositories")

	Root.AddCommand(memoryCmd)
}
