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
// store, and (Batch 2 Lane D) optionally clean up already-verified-copied
// sources via --cleanup-sources. It never touches .wip's own reads (D3,
// "read-through"), and --cleanup-sources only ever deletes a source that
// migrate.PlanCleanup classifies as verified-copied (hard rule 3).
var migrateStateCmd = &cobra.Command{
	Use:   "migrate-state <path>",
	Short: "Migrate legacy .wip branch state into the work-model store",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		extraPaths, _ := c.Flags().GetStringArray("path")
		allRegistered, _ := c.Flags().GetBool("all-registered")
		cleanupSources, _ := c.Flags().GetBool("cleanup-sources")
		dryRun, _ := c.Flags().GetBool("dry-run")
		includeGlobalArchive, _ := c.Flags().GetBool("include-global-archive")

		if dryRun && !cleanupSources {
			// --dry-run has no meaning outside --cleanup-sources — the
			// plain migration path has no destructive effect to preview
			// (it never deletes anything) and running it for real anyway
			// would be exactly the silent-real-migration-behind-a-preview-
			// flag surprise this refusal exists to prevent.
			Die("migrate-state: --dry-run only applies together with --cleanup-sources (the plain migration path has nothing to preview — it never deletes anything)")
		}

		roots, err := resolveMigrateRoots(args[0], extraPaths, allRegistered)
		if err != nil {
			Die("migrate-state: %v", err)
		}

		if cleanupSources {
			runCleanupSources(roots, dryRun, includeGlobalArchive)
			return
		}

		summary, err := migrate.RunWithOptions(state.StateDir(), roots, migrate.Options{IncludeGlobalArchive: includeGlobalArchive})
		if err != nil {
			Die("migrate-state: %v", err)
		}
		PrintJSON(summary)
		if len(summary.Divergent) > 0 || len(summary.Skipped) > 0 {
			os.Exit(1)
		}
	},
}

// cleanupResult is the JSON shape printed by --cleanup-sources, covering
// both the --dry-run preview and the real-deletion run (Deleted is empty on
// a dry run — nothing was actually removed).
type cleanupResult struct {
	Candidates []string          `json:"candidates"`
	Blocked    map[string]string `json:"blocked"`
	Deleted    []string          `json:"deleted"`
	DryRun     bool              `json:"dry_run"`
}

// runCleanupSources implements Lane D's CLI behavior (spec §7): --dry-run is
// a read-only preview and is NEVER refused, even when there is nothing to
// clean up yet — mirroring wip.go's --dry-run discipline. Without --dry-run,
// the command refuses (mirroring requireArchiveBeforePurge's shape,
// wip.go:68) when PlanCleanup finds zero candidates, since running the
// destructive path for no effect is more likely a misconfiguration than
// intent.
//
// includeGlobalArchive threads --include-global-archive through to
// PlanCleanupWithOptions (review findings M5/M6) — previously this flag was
// silently ignored under --cleanup-sources, and a migrated .wip/.archive/
// entry had no removal path other than the legacy, indiscriminate
// clear-wip/clear-others --purge.
func runCleanupSources(roots []string, dryRun bool, includeGlobalArchive bool) {
	candidates, blocked, err := migrate.PlanCleanupWithOptions(state.StateDir(), roots, migrate.Options{IncludeGlobalArchive: includeGlobalArchive})
	if err != nil {
		Die("migrate-state --cleanup-sources: %v", err)
	}

	if dryRun {
		PrintJSON(cleanupResult{Candidates: candidates, Blocked: blocked, Deleted: nil, DryRun: true})
		return
	}

	if len(candidates) == 0 {
		Die("migrate-state --cleanup-sources: nothing to clean up — no verified-copied sources found under the given roots (run without --cleanup-sources first, or check the %d blocked entr(y/ies) reported by --dry-run)", len(blocked))
	}

	deleted, err := migrate.DeleteSources(candidates)
	if err != nil {
		// deleted still reflects every path actually removed before the
		// failure — print it before dying so a partial failure never
		// leaves the user with no record of what was already permanently
		// deleted (review.md #5).
		PrintJSON(cleanupResult{Candidates: candidates, Blocked: blocked, Deleted: deleted, DryRun: false})
		Die("migrate-state --cleanup-sources: %v", err)
	}
	PrintJSON(cleanupResult{Candidates: candidates, Blocked: blocked, Deleted: deleted, DryRun: false})
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
		if _, err := os.Stat(abs); err != nil {
			fmt.Fprintf(os.Stderr, "[aidw] migrate-state: skipping unresolvable root %s: %v\n", p, err)
			return
		}
		// Dedup key must match migrate.go/cleanup.go's own source-path
		// normalization (filepath.EvalSymlinks), not a bare Abs — on
		// macOS /tmp is a symlink to /private/tmp, so a worktree reported
		// by `git worktree list` and the same directory reached via
		// --path /tmp/... would otherwise dedup as two distinct roots for
		// one physical directory (review.md #8). A root may legitimately
		// not exist yet by the time EvalSymlinks runs (already handled by
		// the Stat above, but keep this belt-and-suspenders): fall back
		// to abs if it errors.
		key := abs
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			key = resolved
		}
		if seen[key] {
			return
		}
		seen[key] = true
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
	migrateStateCmd.Flags().Bool("cleanup-sources", false, "Delete legacy .wip sources that have been verified-copied (never deletes anything unverified)")
	migrateStateCmd.Flags().Bool("dry-run", false, "With --cleanup-sources, preview what would be deleted without deleting anything (never refused)")
	migrateStateCmd.Flags().Bool("include-global-archive", false, "Also migrate .wip/.archive/ entries as archived work records")
	Root.AddCommand(migrateStateCmd)
}
