package work

import "sort"

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
//     non-empty and a binding exists.
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
//     matches because the worktree was relocated).
//     Candidate sets (both phases) are DE-DUPLICATED BY WORK_ID before
//     applying the 0/1/>=2 rule, then sorted by WorkID for deterministic
//     output.
//     Apply 0 / 1 / >=2 handling to whichever phase actually produced the
//     (de-duplicated) candidate set: zero -> ErrNoActiveWork; exactly one
//     -> return it; more than one -> return ErrAmbiguousWork and the full
//     candidate list as the second return value — never guess, never pick
//     most-recently-updated.
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
		binding, err := LoadSessionBinding(opts.StateDir, opts.SessionID)
		if err != nil {
			return nil, nil, err
		}
		if binding != nil {
			r, err := Load(opts.StateDir, binding.WorkID)
			if err != nil {
				return nil, nil, err
			}
			return r, nil, nil
		}
	}

	all, err := ListRecords(opts.StateDir)
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
		candidates = dedupeSorted(matchByRepoBranch(live, opts.RepoID, opts.Branch))
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

func matchByRepoBranch(records []*Record, repoID, branch string) []*Record {
	var out []*Record
	for _, r := range records {
		for _, a := range r.Attachments {
			if a.RepoID == repoID && a.Branch == branch {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// dedupeSorted de-duplicates records by WorkID (a single record with
// multiple matching attachments must count once) and sorts the result by
// WorkID for deterministic output.
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
