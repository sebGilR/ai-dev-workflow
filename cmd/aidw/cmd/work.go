package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/migrate"
	"aidw/cmd/aidw/internal/state"
	"aidw/cmd/aidw/internal/work"
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "Manage work records (work-model store)",
}

// resolveAttachContext computes the repo_id/branch/normalized worktree path
// and HEAD sha for path, registering the repo along the way. This is the
// mutate-path shape shared by `work start` and `work attach` — both are
// allowed to call state.RegisterRepo (lookup-only commands are not).
func resolveAttachContext(path string) (top, repoID, branch, normalizedTop, head string, err error) {
	top, err = state.ExecutionRoot(path)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("not a git repo: %w", err)
	}
	branch, err = git.CurrentBranch(top)
	if err != nil {
		return "", "", "", "", "", err
	}
	// RegisterRepo does its own git.CommonDir + filepath.EvalSymlinks
	// canonicalization internally — do not pre-canonicalize top here, that
	// duplication is exactly the divergence risk RegisterRepo's contract
	// exists to avoid.
	repoID, err = state.RegisterRepo(state.StateDir(), top)
	if err != nil {
		return "", "", "", "", "", err
	}
	head, err = git.HeadSHA(top)
	if err != nil {
		return "", "", "", "", "", err
	}
	normalizedTop, err = filepath.EvalSymlinks(top)
	if err != nil {
		return "", "", "", "", "", err
	}
	return top, repoID, branch, normalizedTop, head, nil
}

var workStartCmd = &cobra.Command{
	Use:   "start <path>",
	Short: "Create a new work record and attach it to the current repo/branch",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		title, _ := c.Flags().GetString("title")
		branchOverride, _ := c.Flags().GetString("branch")
		modeFlag, _ := c.Flags().GetString("mode")
		noAttach, _ := c.Flags().GetBool("no-attach")

		if modeFlag != "delivery" && modeFlag != "freeform" {
			Die("work start: --mode must be \"delivery\" or \"freeform\", got %q", modeFlag)
		}
		if noAttach && branchOverride != "" {
			Die("work start: --branch is not valid with --no-attach (freeform record has no attachment to override)")
		}
		if noAttach && modeFlag != "freeform" {
			Die("work start: --no-attach is only valid with --mode freeform")
		}

		var r *work.Record
		if noAttach {
			// Freeform, non-git scratch directory: skip resolveAttachContext
			// entirely — no state.RegisterRepo, no git.* call. r.Attachments
			// stays the empty slice work.New already initializes.
			r = work.New(title, work.ModeFreeform)
		} else {
			_, repoID, branch, normalizedTop, head, err := resolveAttachContext(args[0])
			if err != nil {
				Die("work start: %v", err)
			}
			if branchOverride != "" {
				branch = branchOverride
			}

			r = work.New(title, work.Mode(modeFlag))
			r.Attachments = append(r.Attachments, work.Attachment{
				RepoID:       repoID,
				WorktreePath: normalizedTop,
				Branch:       branch,
				Head:         head,
			})
		}

		// Ordering: write context.md BEFORE work.Save (see Task 3's
		// rationale) — a mid-failure here leaves an orphan context.md with
		// no work.json, which ScanRecords treats as an absent record, not
		// a live one with a missing file.
		if r.Mode == work.ModeFreeform {
			if err := work.WriteFreeformContext(state.StateDir(), r.WorkID, r.Title); err != nil {
				Die("work start: write context.md: %v", err)
			}
		}

		if err := work.Save(state.StateDir(), r); err != nil {
			Die("work start: %v", err)
		}
		PrintJSON(r)
	},
}

var workListCmd = &cobra.Command{
	Use:   "list <path>",
	Short: "List work records attached to the current repo",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		includeArchived, _ := c.Flags().GetBool("include-archived")
		global, _ := c.Flags().GetBool("global")

		var repoID string
		if !global {
			top, err := state.ExecutionRoot(args[0])
			if err != nil {
				Die("work list: not a git repo: %v", err)
			}
			// Pure lookup — never calls RegisterRepo.
			repoID, err = state.RepoIdentity(top)
			if err != nil {
				Die("work list: %v", err)
			}
		}

		all, err := work.ListRecords(state.StateDir())
		if err != nil {
			Die("work list: %v", err)
		}

		// Initialized (not a nil `var`) so an empty result serializes as
		// `[]` rather than `null` — shell callers pipe this into
		// `jq '.[]'`, which errors on null.
		filtered := make([]*work.Record, 0, len(all))
		for _, r := range all {
			if !global && !hasAttachmentForRepo(r, repoID) {
				continue
			}
			if !includeArchived && r.Lifecycle == work.LifecycleArchived {
				continue
			}
			filtered = append(filtered, r)
		}
		PrintJSON(filtered)
	},
}

func hasAttachmentForRepo(r *work.Record, repoID string) bool {
	for _, a := range r.Attachments {
		if a.RepoID == repoID {
			return true
		}
	}
	return false
}

var workStatusCmd = &cobra.Command{
	Use:   "status <path>",
	Short: "Print the work record resolved for the current repo/branch",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		flagWork, _ := c.Flags().GetString("work")

		// D2: each of these four steps additively falls through (rather
		// than Die-ing) when flagWork != "" — a freeform/no-attach record
		// is only resolvable via ExplicitWorkID or session binding, and
		// Resolve's ExplicitWorkID short-circuit never reads
		// repoID/branch/normalizedTop, so leaving them "" here is safe.
		// When flagWork == "", every Die below is byte-for-byte unchanged
		// from today. This is deliberately four separate additive
		// conditionals, not a shared helper (see D2 in the Cluster J spec).
		var repoID, branch, normalizedTop string
		top, err := state.ExecutionRoot(args[0])
		if err != nil {
			if flagWork == "" {
				Die("work status: not a git repo: %v", err)
			}
		} else {
			branch, err = git.CurrentBranch(top)
			if err != nil {
				if flagWork == "" {
					Die("work status: %v", err)
				}
			} else {
				repoID, err = state.RepoIdentity(top)
				if err != nil {
					if flagWork == "" {
						Die("work status: %v", err)
					}
				} else {
					normalizedTop, err = filepath.EvalSymlinks(top)
					if err != nil && flagWork == "" {
						Die("work status: %v", err)
					}
				}
			}
		}

		record, candidates, err := work.Resolve(work.ResolveOptions{
			StateDir:       state.StateDir(),
			ExplicitWorkID: flagWork,
			SessionID:      "", // non-hook context: skip session-binding step
			RepoID:         repoID,
			Branch:         branch,
			WorktreePath:   normalizedTop,
		})
		if err != nil {
			if errors.Is(err, work.ErrNoActiveWork) {
				fmt.Fprintln(os.Stderr, "[aidw] no active work for this context — run `aidw work start`")
				os.Exit(1)
			}
			if errors.Is(err, work.ErrIncompleteScan) {
				fmt.Fprintln(os.Stderr, "[aidw]", err)
				os.Exit(1)
			}
			if errors.Is(err, work.ErrAmbiguousWork) {
				PrintJSON(candidateSummaries(candidates, repoID))
				os.Exit(1)
			}
			Die("work status: %v", err)
		}
		PrintJSON(record)
	},
}

// candidateSummaries reduces each ambiguous-match candidate to the
// (work_id, title, branch) triple a user needs to pick one with --work.
// repoID, when non-empty, is used to prefer the attachment that actually
// matched the querying repo (there may be several attachments across
// unrelated repos) over just the last one appended.
func candidateSummaries(records []*work.Record, repoID string) []map[string]string {
	out := make([]map[string]string, 0, len(records))
	for _, r := range records {
		branch := ""
		if len(r.Attachments) > 0 {
			branch = r.Attachments[len(r.Attachments)-1].Branch
		}
		for _, a := range r.Attachments {
			if repoID != "" && a.RepoID == repoID {
				branch = a.Branch
				break
			}
		}
		out = append(out, map[string]string{
			"work_id": r.WorkID,
			"title":   r.Title,
			"branch":  branch,
		})
	}
	return out
}

var workAttachCmd = &cobra.Command{
	Use:   "attach <path>",
	Short: "Attach an existing work record to the current repo/branch",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		workID, _ := c.Flags().GetString("work")
		if workID == "" {
			Die("work attach: --work is required")
		}

		_, repoID, branch, normalizedTop, head, err := resolveAttachContext(args[0])
		if err != nil {
			Die("work attach: %v", err)
		}

		r, err := work.UpdateRecord(state.StateDir(), workID, func(r *work.Record) error {
			r.Attachments = append(r.Attachments, work.Attachment{
				RepoID:       repoID,
				WorktreePath: normalizedTop,
				Branch:       branch,
				Head:         head,
			})
			return nil
		})
		if err != nil {
			Die("work attach: %v", err)
		}
		PrintJSON(r)
	},
}

// newLifecycleCmd builds a `work <verb> <id>` command that sets
// r.Lifecycle = lifecycle via UpdateRecord's load-mutate-save-under-lock
// cycle — byte-for-byte the shape workAttachCmd/runCheckpointDirect's
// mutate closures already establish, just a one-line mutation. Stage is
// never touched (§2.1's invariant: lifecycle and stage are independent).
func newLifecycleCmd(verb string, lifecycle work.Lifecycle) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <id>",
		Short: fmt.Sprintf("Set a work record's lifecycle to %s", lifecycle),
		Args:  cobra.ExactArgs(1),
		Run: func(c *cobra.Command, args []string) {
			r, err := work.UpdateRecord(state.StateDir(), args[0], func(r *work.Record) error {
				r.Lifecycle = lifecycle
				return nil
			})
			if err != nil {
				Die("work %s: %v", verb, err)
			}
			PrintJSON(r)
		},
	}
}

var workPauseCmd = newLifecycleCmd("pause", work.LifecyclePaused)
var workDoneCmd = newLifecycleCmd("done", work.LifecycleDone)
var workArchiveCmd = newLifecycleCmd("archive", work.LifecycleArchived)

// workActivateCmd is the inverse of workArchiveCmd/workPauseCmd/workDoneCmd
// — added per Cluster I review finding H2: without an explicit "un-archive"
// verb, the re-routed skills' mis-selected `work archive <id>` (see
// workPurgeCmd's neighboring history and claude/skills/wip-cleanup's
// branch-filtered probe fix) had no clean, discoverable recovery path other
// than the undocumented fact that `work pause <id>` also re-enters
// resolve.go's Active||Paused candidacy. `work activate <id>` sets
// Lifecycle back to Active explicitly, following the identical
// UpdateRecord shape every other lifecycle verb uses.
var workActivateCmd = newLifecycleCmd("activate", work.LifecycleActive)

// purgePreview is workPurgeCmd's --dry-run output shape: everything that
// would be permanently deleted, computed without acquiring any lock or
// checking the archived-only precondition — a preview must always be
// computable (wip.go's dry-run-never-refused rule, §2.2).
type purgePreview struct {
	WorkID          string   `json:"work_id"`
	Title           string   `json:"title"`
	Lifecycle       string   `json:"lifecycle"`
	Attachments     []string `json:"attachments"` // worktree paths
	SessionBindings []string `json:"session_bindings"`
	// MappingEntries lists the wip-paths.json (normalized sourceWipDir) keys
	// that ForgetSource would remove — added per Cluster I review finding
	// H1: without this, a plain migrate-state run after a real purge would
	// see this workID's mapping entry as dangling and re-mint a fresh
	// record for the same legacy source, resurrecting content the user was
	// told was permanently deleted.
	MappingEntries []string `json:"mapping_entries"`
	DryRun         bool     `json:"dry_run"`
}

// purgeResult is workPurgeCmd's real-deletion output shape.
type purgeResult struct {
	WorkID                 string   `json:"work_id"`
	Deleted                bool     `json:"deleted"`
	SessionBindingsRemoved []string `json:"session_bindings_removed"`
	// SessionReapError is populated when DeleteSessionBindingsFor itself
	// failed after a successful record delete — reported here rather than
	// flipping the command's exit code (§2.5's "not fatal" rule: the record
	// delete already succeeded, and a reap failure must never be confused
	// with a record-delete failure).
	SessionReapError string `json:"session_reap_error,omitempty"`
	// MappingEntriesForgotten lists the wip-paths.json keys ForgetSource
	// actually removed (review finding H1). Never confused with a
	// record-delete failure, for the same "not fatal" reason as the session
	// reap: the record is already gone by the time this runs.
	MappingEntriesForgotten []string `json:"mapping_entries_forgotten"`
	MappingForgetError      string   `json:"mapping_forget_error,omitempty"`
}

// workPromoteCmd implements D3: `work promote <id> --mode delivery`
// upgrades a freeform record to delivery mode, seeding the delivery
// artifact set into work/<id>/attachments/ (files-first, then mode-flip;
// see SeedDeliveryArtifacts and this Run func's mutate closure).
var workPromoteCmd = &cobra.Command{
	Use:   "promote <id>",
	Short: "Promote a freeform work record to delivery mode",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		modeFlag, _ := c.Flags().GetString("mode")
		if modeFlag != "delivery" {
			Die("work promote: --mode must be \"delivery\"")
		}
		workID := args[0]

		// Fast-fail diagnostic pre-check (unlocked, may be stale under a
		// race — the authoritative check is inside UpdateRecord below).
		existing, err := work.Load(state.StateDir(), workID)
		if err != nil {
			Die("work promote: %v", err)
		}
		if existing.Mode != work.ModeFreeform {
			Die("work promote: %v", work.ErrAlreadyDelivery)
		}
		if existing.Lifecycle != work.LifecycleActive && existing.Lifecycle != work.LifecyclePaused {
			Die("work promote: %v", work.ErrNotPromotable)
		}

		r, err := work.UpdateRecord(state.StateDir(), workID, func(r *work.Record) error {
			// Authoritative recheck, under state.AcquireLock: re-Load'd
			// fresh by UpdateRecord itself, so this sees any concurrent
			// `work archive <id>` that landed after the diagnostic
			// pre-check above.
			if r.Mode != work.ModeFreeform {
				return work.ErrAlreadyDelivery
			}
			if r.Lifecycle != work.LifecycleActive && r.Lifecycle != work.LifecyclePaused {
				return work.ErrNotPromotable
			}
			// Seed files INSIDE the lock, after the recheck passes and
			// before the mode flip below — this is what closes the
			// lock-free-race finding: if seeding fails partway, mutate
			// returns an error, UpdateRecord does not call saveLocked, and
			// Mode is never flipped (safe to retry `work promote`; seeding
			// is idempotent per the seed-if-missing rule above).
			if err := work.SeedDeliveryArtifacts(state.StateDir(), workID); err != nil {
				return err
			}
			r.Mode = work.ModeDelivery
			return nil
		})
		if err != nil {
			Die("work promote: %v", err)
		}
		PrintJSON(r)
	},
}

var workPurgeCmd = &cobra.Command{
	Use:   "purge <id>",
	Short: "Permanently delete a work record and its attachments",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		dryRun, _ := c.Flags().GetBool("dry-run")
		force, _ := c.Flags().GetBool("force")
		workID := args[0]

		if dryRun {
			r, err := work.Load(state.StateDir(), workID)
			if err != nil {
				Die("work purge: %v", err)
			}
			paths := make([]string, 0, len(r.Attachments))
			for _, a := range r.Attachments {
				paths = append(paths, a.WorktreePath)
			}
			bindings, err := work.SessionIDsBoundTo(state.StateDir(), workID)
			if err != nil {
				Die("work purge: %v", err)
			}
			mappingEntries, err := migrate.SourcesFor(state.StateDir(), workID)
			if err != nil {
				Die("work purge: %v", err)
			}
			PrintJSON(purgePreview{
				WorkID:          r.WorkID,
				Title:           r.Title,
				Lifecycle:       string(r.Lifecycle),
				Attachments:     paths,
				SessionBindings: bindings,
				MappingEntries:  mappingEntries,
				DryRun:          true,
			})
			return
		}

		if err := work.DeleteRecord(state.StateDir(), workID, force); err != nil {
			if errors.Is(err, work.ErrNotArchived) {
				Die("work purge: %v (run `work archive %s` first, or pass --force)", err, workID)
			}
			Die("work purge: %v", err)
		}

		removed, reapErr := work.DeleteSessionBindingsFor(state.StateDir(), workID)
		result := purgeResult{
			WorkID:                 workID,
			Deleted:                true,
			SessionBindingsRemoved: removed,
		}
		if reapErr != nil {
			// The record delete already succeeded — a reap failure must
			// never be confused with a record-delete failure (§2.5's "not
			// fatal" rule). Report it in the output, exit code stays 0.
			result.SessionReapError = reapErr.Error()
			result.SessionBindingsRemoved = []string{}
		}

		// Review finding H1: forget this workID's wip-paths.json entries
		// too, for the same "not fatal, the record delete already
		// succeeded" reason as the session reap. Without this, the next
		// plain migrate-state run sees a dangling mapping entry and
		// re-mints a fresh record from the still-on-disk legacy source,
		// resurrecting content this command just told the user was
		// permanently deleted.
		forgotten, forgetErr := migrate.ForgetSource(state.StateDir(), workID)
		result.MappingEntriesForgotten = forgotten
		if forgetErr != nil {
			result.MappingForgetError = forgetErr.Error()
			result.MappingEntriesForgotten = []string{}
		}
		PrintJSON(result)
	},
}

var workBindSessionCmd = &cobra.Command{
	Use:   "bind-session --work <id> --session <id>",
	Short: "Explicitly bind a session id to a work record",
	Args:  cobra.NoArgs,
	Run: func(c *cobra.Command, args []string) {
		workID, _ := c.Flags().GetString("work")
		sessionID, _ := c.Flags().GetString("session")
		if workID == "" || sessionID == "" {
			Die("work bind-session: --work and --session are required")
		}

		if _, err := work.Load(state.StateDir(), workID); err != nil {
			Die("work bind-session: %v", err)
		}
		if err := work.SaveSessionBinding(state.StateDir(), sessionID, workID); err != nil {
			Die("work bind-session: %v", err)
		}
		PrintJSON(map[string]string{"session_id": sessionID, "work_id": workID})
	},
}

type checkpointHookPayload struct {
	Cwd       string `json:"cwd"`
	SessionID string `json:"session_id"`
}

var workCheckpointCmd = &cobra.Command{
	Use:   "checkpoint [path]",
	Short: "Touch a work record's updated_at, optionally resolving via a hook payload on stdin",
	Args:  cobra.MaximumNArgs(1),
	Run: func(c *cobra.Command, args []string) {
		fromHook, _ := c.Flags().GetBool("from-hook")
		flagWork, _ := c.Flags().GetString("work")

		if !fromHook {
			if len(args) != 1 {
				Die("work checkpoint: path is required without --from-hook")
			}
			runCheckpointDirect(args[0], flagWork)
			return
		}
		runCheckpointFromHook(args, flagWork)
	},
}

func runCheckpointDirect(path, flagWork string) {
	top, err := state.ExecutionRoot(path)
	if err != nil {
		Die("work checkpoint: not a git repo: %v", err)
	}
	branch, err := git.CurrentBranch(top)
	if err != nil {
		Die("work checkpoint: %v", err)
	}
	repoID, err := state.RepoIdentity(top)
	if err != nil {
		Die("work checkpoint: %v", err)
	}
	normalizedTop, err := filepath.EvalSymlinks(top)
	if err != nil {
		Die("work checkpoint: %v", err)
	}

	record, candidates, err := work.Resolve(work.ResolveOptions{
		StateDir:       state.StateDir(),
		ExplicitWorkID: flagWork,
		SessionID:      "", // non-hook context
		RepoID:         repoID,
		Branch:         branch,
		WorktreePath:   normalizedTop,
	})
	if err != nil {
		if errors.Is(err, work.ErrNoActiveWork) {
			fmt.Fprintln(os.Stderr, "[aidw] no active work for this context — run `aidw work start`")
			os.Exit(1)
		}
		if errors.Is(err, work.ErrIncompleteScan) {
			fmt.Fprintln(os.Stderr, "[aidw]", err)
			os.Exit(1)
		}
		if errors.Is(err, work.ErrAmbiguousWork) {
			// Direct (non-hook) invocation is user-facing, so print the
			// same candidate list `work status` prints — the spec's
			// silence requirement applies only to --from-hook. Still no
			// write of any kind.
			PrintJSON(candidateSummaries(candidates, repoID))
			os.Exit(1)
		}
		Die("work checkpoint: %v", err)
	}
	// No auto-bind here — there is no SessionID to bind in a non-hook
	// invocation. UpdateRecord re-reads under the lock rather than trusting
	// the in-memory snapshot Resolve returned, so a concurrent writer's
	// change (e.g. an attach that landed between Resolve and here) isn't
	// silently reverted; checkpoint's only effect is the updated_at bump
	// saveLocked applies.
	if _, err := work.UpdateRecord(state.StateDir(), record.WorkID, func(*work.Record) error {
		return nil
	}); err != nil {
		Die("work checkpoint: %v", err)
	}
}

func runCheckpointFromHook(args []string, flagWork string) {
	var payload checkpointHookPayload
	// A missing/empty/unparseable stdin body is tolerated — fields default
	// to "", matching the shell script's jq-or-sed best-effort tolerance.
	// Guard against a blocking read on an interactive TTY stdin.
	if info, statErr := os.Stdin.Stat(); statErr == nil && (info.Mode()&os.ModeCharDevice) == 0 {
		if body, err := io.ReadAll(os.Stdin); err == nil && len(body) > 0 {
			_ = json.Unmarshal(body, &payload)
		}
	}

	cwd := payload.Cwd
	if cwd == "" {
		if len(args) == 1 {
			cwd = args[0]
		} else if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	sessionID := payload.SessionID

	top, err := state.ExecutionRoot(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[aidw]", err)
		os.Exit(1)
	}
	branch, err := git.CurrentBranch(top)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[aidw]", err)
		os.Exit(1)
	}
	repoID, err := state.RepoIdentity(top)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[aidw]", err)
		os.Exit(1)
	}
	normalizedTop, err := filepath.EvalSymlinks(top)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[aidw]", err)
		os.Exit(1)
	}

	record, _, resolveErr := work.Resolve(work.ResolveOptions{
		StateDir:       state.StateDir(),
		ExplicitWorkID: flagWork,
		SessionID:      sessionID,
		RepoID:         repoID,
		Branch:         branch,
		WorktreePath:   normalizedTop,
	})
	if resolveErr != nil {
		if errors.Is(resolveErr, work.ErrNoActiveWork) {
			// No bound/associated active work = no-op, exit 0, no output.
			os.Exit(0)
		}
		if errors.Is(resolveErr, work.ErrIncompleteScan) {
			// Distinct from plain ambiguity: a record could not be read at
			// all, so this is worth a stderr line even though the hook
			// script wraps the whole call in `|| true` — helps a human
			// debugging a hook run manually.
			fmt.Fprintln(os.Stderr, "[aidw]", resolveErr)
			os.Exit(1)
		}
		if errors.Is(resolveErr, work.ErrAmbiguousWork) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "[aidw]", resolveErr)
		os.Exit(1)
	}

	// UpdateRecord re-reads under the lock rather than trusting the
	// in-memory snapshot Resolve returned, so a concurrent writer's change
	// isn't silently reverted; checkpoint's only effect is the updated_at
	// bump saveLocked applies.
	if _, err := work.UpdateRecord(state.StateDir(), record.WorkID, func(*work.Record) error {
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "[aidw]", err)
		os.Exit(1)
	}

	// The one caller that performs the design's step-3 auto-bind: resolved
	// via worktree association (not explicit id, not an existing session
	// binding) and a real session id is present.
	if flagWork == "" && sessionID != "" {
		existingBinding, bindErr := work.LoadSessionBinding(state.StateDir(), sessionID)
		if bindErr == nil && existingBinding == nil {
			_ = work.SaveSessionBinding(state.StateDir(), sessionID, record.WorkID)
		}
	}
	os.Exit(0)
}

func init() {
	workCmd.AddCommand(workStartCmd)
	workCmd.AddCommand(workListCmd)
	workCmd.AddCommand(workStatusCmd)
	workCmd.AddCommand(workAttachCmd)
	workCmd.AddCommand(workBindSessionCmd)
	workCmd.AddCommand(workCheckpointCmd)
	workCmd.AddCommand(workPauseCmd)
	workCmd.AddCommand(workDoneCmd)
	workCmd.AddCommand(workArchiveCmd)
	workCmd.AddCommand(workActivateCmd)
	workCmd.AddCommand(workPurgeCmd)
	workCmd.AddCommand(workPromoteCmd)

	workStartCmd.Flags().String("title", "", "Title for the new work record")
	_ = workStartCmd.MarkFlagRequired("title")
	workStartCmd.Flags().String("branch", "", "Branch name override")
	workStartCmd.Flags().String("mode", "delivery", "Work mode: delivery or freeform")
	workStartCmd.Flags().Bool("no-attach", false, "Skip git-repo attachment (freeform, non-git scratch directories only)")

	workListCmd.Flags().Bool("include-archived", false, "Include archived work records")
	workListCmd.Flags().Bool("global", false, "List work records across all repos, skipping git-repo resolution")

	workStatusCmd.Flags().String("work", "", "Explicit work id (skips resolution)")

	workPromoteCmd.Flags().String("mode", "delivery", "Target mode (only \"delivery\" is supported)")

	workAttachCmd.Flags().String("work", "", "Work id to attach (required)")
	_ = workAttachCmd.MarkFlagRequired("work")

	workBindSessionCmd.Flags().String("work", "", "Work id to bind (required)")
	workBindSessionCmd.Flags().String("session", "", "Session id to bind (required)")
	_ = workBindSessionCmd.MarkFlagRequired("work")
	_ = workBindSessionCmd.MarkFlagRequired("session")

	workCheckpointCmd.Flags().String("work", "", "Explicit work id (skips resolution)")
	workCheckpointCmd.Flags().Bool("from-hook", false, "Read cwd/session_id from a JSON payload on stdin")

	workPurgeCmd.Flags().Bool("dry-run", false, "Preview what would be permanently deleted (never refused)")
	workPurgeCmd.Flags().Bool("force", false, "Bypass the archived-only precondition")

	Root.AddCommand(workCmd)
}
