package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/memory"
	"aidw/cmd/aidw/internal/slug"
	"aidw/cmd/aidw/internal/state"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage persistent task memory and facts",
}

// factsScope is the constant scope value every facts row uses, migrated and
// newly written alike (§2c decision 1 of the Cluster H spec). The original
// design derived scope from the branch ("branch:" + slug), but that cannot
// satisfy "the same fact looked up from a second worktree of the same
// clone returns the same value" — git forbids checking out the same branch
// in two worktrees, so a branch-derived scope is worktree-variant by
// construction. All facts operations use this one constant instead.
const factsScope = "repo"

// repoAndBranch resolves the values the memory commands actually need: the
// repository root and slugified current branch (the legacy repo_path/branch
// identifying pair, still required for a DB that has not run
// `aidw memory migrate` yet), and the repo_id (the new identifying value,
// stable across worktrees of the same clone — see state.RepoIdentity). It
// deliberately does NOT go through wip.FindBranchState/EnsureBranchState —
// memory is repo-scoped knowledge and must work on a repo/branch with no
// .wip state at all (that is what /wip-document-project does on a fresh
// repo). The branch slugification must stay identical to wip's
// resolveBranchName so legacy-schema facts stored here are readable by
// callers that resolve the branch through the wip package.
func repoAndBranch(repoPath string) (repo string, branch string, repoID string, err error) {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return "", "", "", fmt.Errorf("not a git repo: %w", err)
	}
	b, err := git.CurrentBranch(top)
	if err != nil {
		return "", "", "", fmt.Errorf("get current branch: %w", err)
	}
	id, err := state.RepoIdentity(top)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve repo id: %w", err)
	}
	return top, slug.SafeSlug(b), id, nil
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

		repo, branch, repoID, err := repoAndBranch(repoPath)
		if err != nil {
			Die("resolve repo: %v", err)
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

		if err := db.StoreFact(repo, branch, repoID, factsScope, key, val, emb); err != nil {
			Die("store: %v", err)
		}

		PrintJSON(map[string]any{
			"status":   "stored",
			"key":      key,
			"branch":   branch,
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
		
		var repoPath, repoName, branch, repoID string
		if !isGlobal {
			if len(args) == 0 {
				Die("repo path is required for local listing")
			}
			repoPath = args[0]
			repo, b, id, err := repoAndBranch(repoPath)
			if err != nil {
				Die("resolve repo: %v", err)
			}
			repoName = repo
			branch = b
			repoID = id
		}

		db, err := memory.Open()
		if err != nil {
			Die("memory db: %v", err)
		}
		defer db.Close()

		facts, err := db.ListFacts(repoName, branch, repoID, factsScope)
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

		repo, _, repoID, err := repoAndBranch(repoPath)
		if err != nil {
			Die("resolve repo: %v", err)
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

			// repo is always absolute (git rev-parse --show-toplevel), while
			// path follows target — which is relative whenever the caller
			// passed a relative one, as the wip-document-project skill does
			// (`aidw memory index . .claude/repo-docs/`). filepath.Rel errors
			// on a mixed absolute/relative pair, so resolve path first;
			// otherwise every indexed row got an empty file_path.
			absPath, err := filepath.Abs(path)
			if err != nil {
				absPath = path
			}
			if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
				absPath = resolved
			}
			relPath, relErr := filepath.Rel(repo, absPath)
			if relErr != nil {
				relPath = absPath
			}
			emb, err := client.Embed(content)
			if err != nil {
				return fmt.Errorf("embed %s: %w", relPath, err)
			}

			if err := db.IndexItem(repo, repoID, relPath, content, emb); err != nil {
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
		var repoPath, repoID, query string

		if isGlobal {
			query = args[0]
		} else {
			if len(args) < 2 {
				Die("repo path and query are required for local search")
			}
			repoPath = args[0]
			query = args[1]
			repo, _, id, err := repoAndBranch(repoPath)
			if err != nil {
				Die("resolve repo: %v", err)
			}
			repoPath = repo
			repoID = id
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

		results, err := db.Search(repoPath, repoID, queryEmb, 5)
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

// memoryMigrateCmd is Task B.1b: the only thing that ever triggers the
// repo_id/scope schema rebuild (Task B.2). Idempotent — a DB already on the
// new schema reports "already migrated" and exits 0 without taking a lock.
var memoryMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Rebuild the memory database onto the repo_id/scope schema",
	Args:  cobra.NoArgs,
	Run: func(c *cobra.Command, args []string) {
		db, err := memory.Open()
		if err != nil {
			Die("memory db: %v", err)
		}
		defer db.Close()

		summary, err := db.Migrate()
		if err != nil {
			Die("memory migrate: %v", err)
		}

		PrintJSON(summary)
	},
}

func init() {
	memoryCmd.AddCommand(memoryStatusCmd)
	memoryCmd.AddCommand(memoryStoreCmd)
	memoryCmd.AddCommand(memoryListCmd)
	memoryCmd.AddCommand(memoryIndexCmd)
	memoryCmd.AddCommand(memorySearchCmd)
	memoryCmd.AddCommand(memoryMigrateCmd)

	memoryStoreCmd.Flags().Bool("semantic", false, "Index the fact for semantic search")
	memoryListCmd.Flags().Bool("global", false, "List facts from all repositories")
	memorySearchCmd.Flags().Bool("global", false, "Search across all repositories")

	Root.AddCommand(memoryCmd)
}
