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

		_, repoID, branch, normalizedTop, head, err := resolveAttachContext(args[0])
		if err != nil {
			Die("work start: %v", err)
		}
		if branchOverride != "" {
			branch = branchOverride
		}

		r := work.New(title, work.ModeDelivery)
		r.Attachments = append(r.Attachments, work.Attachment{
			RepoID:       repoID,
			WorktreePath: normalizedTop,
			Branch:       branch,
			Head:         head,
		})
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

		top, err := state.ExecutionRoot(args[0])
		if err != nil {
			Die("work list: not a git repo: %v", err)
		}
		// Pure lookup — never calls RegisterRepo.
		repoID, err := state.RepoIdentity(top)
		if err != nil {
			Die("work list: %v", err)
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
			if !hasAttachmentForRepo(r, repoID) {
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

		top, err := state.ExecutionRoot(args[0])
		if err != nil {
			Die("work status: not a git repo: %v", err)
		}
		branch, err := git.CurrentBranch(top)
		if err != nil {
			Die("work status: %v", err)
		}
		// Pure lookup — never calls RegisterRepo.
		repoID, err := state.RepoIdentity(top)
		if err != nil {
			Die("work status: %v", err)
		}
		normalizedTop, err := filepath.EvalSymlinks(top)
		if err != nil {
			Die("work status: %v", err)
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

	workStartCmd.Flags().String("title", "", "Title for the new work record")
	_ = workStartCmd.MarkFlagRequired("title")
	workStartCmd.Flags().String("branch", "", "Branch name override")

	workListCmd.Flags().Bool("include-archived", false, "Include archived work records")

	workStatusCmd.Flags().String("work", "", "Explicit work id (skips resolution)")

	workAttachCmd.Flags().String("work", "", "Work id to attach (required)")
	_ = workAttachCmd.MarkFlagRequired("work")

	workBindSessionCmd.Flags().String("work", "", "Work id to bind (required)")
	workBindSessionCmd.Flags().String("session", "", "Session id to bind (required)")
	_ = workBindSessionCmd.MarkFlagRequired("work")
	_ = workBindSessionCmd.MarkFlagRequired("session")

	workCheckpointCmd.Flags().String("work", "", "Explicit work id (skips resolution)")
	workCheckpointCmd.Flags().Bool("from-hook", false, "Read cwd/session_id from a JSON payload on stdin")

	Root.AddCommand(workCmd)
}
