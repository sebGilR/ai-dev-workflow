package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/migrate"
	"aidw/cmd/aidw/internal/state"
)

// migrateStateCmd is H1: copy legacy .wip branch state into the work-model
// store. It never deletes a migration source (that is Batch 2 Lane D's
// `--cleanup-sources`, out of scope here — see spec §1) and never touches
// .wip's own reads (D3, "read-through").
var migrateStateCmd = &cobra.Command{
	Use:   "migrate-state <path>",
	Short: "Migrate legacy .wip branch state into the work-model store",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		extraPaths, _ := c.Flags().GetStringArray("path")
		allRegistered, _ := c.Flags().GetBool("all-registered")

		roots, err := resolveMigrateRoots(args[0], extraPaths, allRegistered)
		if err != nil {
			Die("migrate-state: %v", err)
		}

		summary, err := migrate.Run(state.StateDir(), roots)
		if err != nil {
			Die("migrate-state: %v", err)
		}
		PrintJSON(summary)
		if len(summary.Divergent) > 0 || len(summary.Skipped) > 0 {
			os.Exit(1)
		}
	},
}

// resolveMigrateRoots implements D2's bounded root resolution: the default
// root set is git.WorktreeList against the invoked repo; --path (repeatable)
// adds explicit roots; --all-registered adds every repos.json
// last_known_paths entry. A root that does not resolve on disk (os.Stat
// fails) is reported to stderr and skipped, never fatal — this applies
// uniformly to --path and --all-registered roots, not only
// --all-registered, since an explicit --path typo is exactly as
// non-fatal-worthy as a stale registered path.
func resolveMigrateRoots(path string, extraPaths []string, allRegistered bool) ([]string, error) {
	top, err := state.ExecutionRoot(path)
	if err != nil {
		return nil, fmt.Errorf("not a git repo: %w", err)
	}

	worktrees, err := git.WorktreeList(top)
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	seen := map[string]bool{}
	var roots []string
	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[aidw] migrate-state: skipping unresolvable root %s: %v\n", p, err)
			return
		}
		if seen[abs] {
			return
		}
		if _, err := os.Stat(abs); err != nil {
			fmt.Fprintf(os.Stderr, "[aidw] migrate-state: skipping unresolvable root %s: %v\n", p, err)
			return
		}
		seen[abs] = true
		roots = append(roots, abs)
	}

	for _, w := range worktrees {
		add(w)
	}
	for _, p := range extraPaths {
		add(p)
	}

	if allRegistered {
		rf, err := state.LoadRepos(state.StateDir())
		if err != nil {
			return nil, fmt.Errorf("load repos.json: %w", err)
		}
		for _, entry := range rf.Repos {
			for _, p := range entry.LastKnownPaths {
				add(p)
			}
		}
	}

	return roots, nil
}

func init() {
	migrateStateCmd.Flags().StringArray("path", nil, "Additional worktree root to scan (repeatable)")
	migrateStateCmd.Flags().Bool("all-registered", false, "Also scan every repos.json last_known_paths entry")
	Root.AddCommand(migrateStateCmd)
}
