package work

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"aidw/cmd/aidw/internal/state"
)

// ResolveOptions carries the caller's known context into Resolve.
type ResolveOptions struct {
	StateDir       string
	ExplicitWorkID string // "" = not given
	SessionID      string // "" = non-hook context; skip session-binding step entirely
	RepoID         string
	Branch         string
	WorktreePath   string // EvalSymlinks-normalized before calling
}

// Resolve implements the design doc §4 precedence order:
//
//  1. ExplicitWorkID, if set — always wins, loaded via Load(). No ambiguity
//     possible.
//  2. SessionID's recorded binding (LoadSessionBinding), if SessionID is
//     non-empty and a binding exists AND the record it names still loads. A
//     binding pointing at a purged or corrupt record is treated as no
//     binding at all and falls through to step 3, never surfaced as a raw
//     filesystem error.
//  3. Unambiguous worktree association — TWO ORDERED PHASES, not a union
//     of both match kinds (a union manufactures false ambiguity):
//     Phase A: candidates = active/paused records with an attachment whose
//     WorktreePath equals opts.WorktreePath EXACTLY AND whose RepoID
//     equals opts.RepoID (both sides already EvalSymlinks-normalized by
//     the caller). The RepoID check is required, not redundant: it closes
//     the hole where an ordinary `git worktree remove <path> && git
//     worktree add <path> -b other-branch` (or any other reuse of that
//     filesystem path by an unrelated repo/branch) would otherwise
//     silently resolve onto the old, unrelated record. Branch equality is
//     deliberately NOT required in Phase A: a live worktree's branch can
//     legitimately change via a plain `git checkout` without ever calling
//     `work attach`.
//     Phase B: run ONLY if Phase A produced zero candidates — candidates =
//     active/paused records with an attachment matching RepoID+Branch (the
//     moved-worktree fallback, when the worktree_path itself no longer
//     matches because the worktree was relocated). Phase B additionally
//     SKIPS any attachment whose recorded worktree_path still exists on
//     disk and differs from opts.WorktreePath: that attachment belongs to
//     another live checkout, and a shared branch name is not licence to
//     steal its record.
//     Candidate sets (both phases) are DE-DUPLICATED BY WORK_ID before
//     applying the 0/1/>=2 rule, then sorted by WorkID for deterministic
//     output.
//     Apply 0 / 1 / >=2 handling to whichever phase actually produced the
//     (de-duplicated) candidate set: zero -> ErrNoActiveWork; exactly one
//     -> return it; more than one -> return ErrAmbiguousWork and the full
//     candidate list as the second return value — never guess, never pick
//     most-recently-updated.
//     If the underlying scan could not read one or more record directories,
//     the 0- and 1-candidate outcomes are suppressed and an
//     ErrAmbiguousWork+ErrIncompleteScan error is returned instead — a
//     skipped record may have been the second match, and the confident
//     branches are exactly the dangerous ones (the hook auto-binds on a
//     single match, making a wrong resolution permanent).
//
// Resolve is a PURE lookup: it never calls SaveSessionBinding or any other
// write. Callers that want the design's "auto-bind on unambiguous worktree
// match when a session is present" behavior (only relevant to `work
// checkpoint --from-hook`) must call SaveSessionBinding themselves after a
// successful step-3 resolution with non-empty SessionID.
func Resolve(opts ResolveOptions) (*Record, []*Record, error) {
	if opts.ExplicitWorkID != "" {
		r, err := Load(opts.StateDir, opts.ExplicitWorkID)
		if err != nil {
			return nil, nil, err
		}
		return r, nil, nil
	}

	if opts.SessionID != "" {
		binding, bindErr := LoadSessionBinding(opts.StateDir, opts.SessionID)
		// A binding we cannot read (corrupt file) or that points at a record
		// we cannot load (purged, corrupt, future schema) is treated exactly
		// like "no binding exists": fall through to step 3 and re-derive the
		// answer from worktree association. Propagating the raw load error
		// instead would make every subsequent hook fire die on
		// `open .../work.json: no such file or directory` with no way out,
		// and the design's step-3 fallback exists precisely to answer this
		// question from first principles.
		if bindErr == nil && binding != nil {
			r, loadErr := Load(opts.StateDir, binding.WorkID)
			if loadErr == nil {
				return r, nil, nil
			}
			// Lazy self-healing reap (§2.5 addendum): only when the bound
			// record is CONFIRMED gone (ErrNotFound specifically, not
			// merely unreadable — an ErrUnsupportedSchemaVersion or other
			// read failure leaves the binding alone, mirroring Cluster H's
			// R3 finding 2 distinction), delete the now-dangling binding
			// file as a side effect of this lookup, using the same
			// lock-then-reread discipline DeleteSessionBindingsFor uses.
			// This never affects Resolve's own return value/behavior —
			// it still falls through to step 3 below either way.
			if errors.Is(loadErr, ErrNotFound) {
				reapDanglingBinding(opts.StateDir, opts.SessionID, binding.WorkID)
			}
		}
	}

	all, skipped, err := ScanRecords(opts.StateDir)
	if err != nil {
		return nil, nil, err
	}
	var live []*Record
	for _, r := range all {
		if r.Lifecycle == LifecycleActive || r.Lifecycle == LifecyclePaused {
			live = append(live, r)
		}
	}

	phaseA := dedupeSorted(matchByWorktree(live, opts.RepoID, opts.WorktreePath))
	candidates := phaseA
	if len(candidates) == 0 {
		candidates = dedupeSorted(matchByRepoBranch(live, opts.RepoID, opts.Branch, opts.WorktreePath))
	}

	// An incomplete scan poisons the 0-and-1 candidate answers, which are
	// exactly the two confident ones: "no active work" (a silent exit-0
	// no-op in the hook) and "exactly this record" (which the hook then
	// makes STICKY by auto-binding the session to it). A record that failed
	// to load may have been the missing second match, so neither confident
	// answer is defensible. >=2 candidates is already the uncertain path, so
	// it needs no extra handling.
	if len(skipped) > 0 && len(candidates) < 2 {
		return nil, candidates, fmt.Errorf(
			"%w: cannot resolve work safely: %d of the scanned work records could not be read (%s); repair or remove them, or pass --work explicitly: %w",
			ErrAmbiguousWork, len(skipped), strings.Join(skipped, ", "), ErrIncompleteScan,
		)
	}

	switch len(candidates) {
	case 0:
		return nil, nil, ErrNoActiveWork
	case 1:
		return candidates[0], nil, nil
	default:
		return nil, candidates, ErrAmbiguousWork
	}
}

// reapDanglingBinding deletes sessionID's binding file, but only after
// re-confirming under the session file's own lock that it still points at
// workID — the same decide-under-the-lock discipline
// DeleteSessionBindingsFor uses, so a concurrent SaveSessionBinding rebind
// racing this lookup can never have its now-unrelated binding deleted out
// from under it. Any failure (lock busy, re-read no longer matches) is
// swallowed silently: this is opportunistic cleanup on a read path, never
// worth failing Resolve's caller over.
func reapDanglingBinding(stateDir, sessionID, workID string) {
	path := SessionPath(stateDir, sessionID)
	release, err := state.AcquireLock(path)
	if err != nil {
		return
	}
	defer release()

	binding, err := LoadSessionBinding(stateDir, sessionID)
	if err != nil || binding == nil || binding.WorkID != workID {
		return
	}
	_ = os.Remove(path)
}

func matchByWorktree(records []*Record, repoID, worktreePath string) []*Record {
	var out []*Record
	for _, r := range records {
		for _, a := range r.Attachments {
			if a.RepoID == repoID && a.WorktreePath == worktreePath {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// matchByRepoBranch is Phase B: the moved-worktree fallback. It is NOT
// path-blind. An attachment whose worktree_path still exists on disk and is
// not the querying worktree describes a DIFFERENT, still-live checkout, and
// matching it would let one live worktree steal another live worktree's work
// record purely because they share a branch name — the same false-match
// class the erratum fixed in Phase A, in the phase nobody scrutinized.
//
// Phase B therefore only falls back for attachments whose worktree is
// actually gone (or is the querying worktree itself, or was never recorded).
// "Gone" means os.IsNotExist specifically: any other stat failure
// (permissions, EIO) is treated as still-live, so a transient filesystem
// error can never widen matching.
func matchByRepoBranch(records []*Record, repoID, branch, queryWorktreePath string) []*Record {
	var out []*Record
	for _, r := range records {
		for _, a := range r.Attachments {
			if a.RepoID != repoID || a.Branch != branch {
				continue
			}
			if a.WorktreePath != "" && a.WorktreePath != queryWorktreePath && pathExists(a.WorktreePath) {
				continue
			}
			out = append(out, r)
			break
		}
	}
	return out
}

// pathExists reports whether path resolves to something on disk. Only a
// definitive "not there" answers false; every other error answers true, so
// callers fail closed.
func pathExists(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return !os.IsNotExist(err)
	}
	return true
}

// dedupeSorted sorts records by WorkID for deterministic candidate output
// and drops WorkID duplicates.
//
// Honest note on the dedup half: on the current matching path it can never
// fire. Each phase's inner loop `break`s after the first matching
// attachment, so a record is appended at most once per phase, and the two
// phases are mutually exclusive (Phase B runs only when Phase A produced
// nothing), so no work_id can arrive from both. The dedup is retained as
// defence for a future restructuring that drops the `break` or unions the
// phases, and is covered directly by TestDedupeSorted so it is exercised
// rather than merely asserted. The SORT, by contrast, is load-bearing on
// every ambiguous resolution: callers print the candidate list.
func dedupeSorted(records []*Record) []*Record {
	seen := map[string]bool{}
	var out []*Record
	for _, r := range records {
		if seen[r.WorkID] {
			continue
		}
		seen[r.WorkID] = true
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkID < out[j].WorkID })
	return out
}
