package migrate

import (
	"fmt"
	"path/filepath"
	"regexp"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/state"
	"aidw/cmd/aidw/internal/util"
	"aidw/cmd/aidw/internal/work"
)

// datedBranchDirPattern extracts the branch-name portion from a dated .wip
// branch directory's own basename (YYYYMMDD or YYYYMMDDHHMMSS prefix,
// mirroring wip.go's own dated-dir pattern at wip.go:174, generalized to any
// branch name since Convert is not looking up one specific branch).
var datedBranchDirPattern = regexp.MustCompile(`^\d{8}(?:\d{6})?-(.+)$`)

// branchNameFromDirName extracts the branch-name portion of a .wip branch
// directory's own basename: the capture group after the date prefix for a
// dated dir, or the whole name for a legacy unprefixed dir. This is always
// available (it is how Discover found the directory in the first place),
// which is what makes it a safe fallback per §2b Q4.
func branchNameFromDirName(dirName string) string {
	if m := datedBranchDirPattern.FindStringSubmatch(dirName); m != nil {
		return m[1]
	}
	return dirName
}

// legacyStatus is the minimal subset of wip.Status's JSON shape Convert
// needs. Defined locally rather than importing internal/wip's Status type,
// so internal/migrate never imports internal/wip for logic reuse (see
// package doc comment) — this is plain JSON deserialization, not a call
// into wip's behavior.
type legacyStatus struct {
	Branch string `json:"branch"`
	Stage  string `json:"stage"`
}

// Convert builds one work.Record for src, reading src.SourceWipDir's
// status.json for Branch/Stage (best-effort — a missing or unparseable
// status.json still produces a record, using the directory-name-derived
// branch as a fallback, never a hard failure of the whole run — §2b Q4).
// mode is always work.ModeDelivery (this is legacy .wip data, which is
// always delivery-mode); lifecycle is always work.LifecycleActive (H1 has
// no basis to infer paused/done/archived — that classification is Cluster
// I's job, not this migration's; work.New already defaults to
// LifecycleActive). Appends exactly one Attachment per §2b Q5. Does not
// call work.Save — the caller (migrate.go) owns the save-vs-skip decision
// per the idempotency/divergence rules (R4).
func Convert(src SourceDir) (*work.Record, error) {
	var status legacyStatus
	// Best-effort: ReadJSON's error (missing file, unparseable JSON) is
	// intentionally discarded — status stays its zero value and the
	// branchNameFromDirName fallback below takes over.
	_ = util.ReadJSON(filepath.Join(src.SourceWipDir, "status.json"), &status)

	branchName := status.Branch
	if branchName == "" {
		branchName = branchNameFromDirName(filepath.Base(src.SourceWipDir))
	}

	repoID, err := state.RepoIdentity(src.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("resolve repo identity for %s: %w", src.WorktreePath, err)
	}
	normalizedWorktreePath, err := filepath.EvalSymlinks(src.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("resolve worktree path %s: %w", src.WorktreePath, err)
	}
	// Head is best-effort: a worktree with no commits yet (or any other
	// git error) still produces a migrated record, just with an empty
	// Head — per §2b Q5's explicit "" if it errors.
	head, err := git.HeadSHA(src.WorktreePath)
	if err != nil {
		head = ""
	}

	r := work.New(branchName, work.ModeDelivery)
	r.Stage = status.Stage
	r.Attachments = append(r.Attachments, work.Attachment{
		RepoID:       repoID,
		WorktreePath: normalizedWorktreePath,
		Branch:       branchName,
		Head:         head,
	})
	return r, nil
}

// ConvertArchived builds one work.Record for src (Kind ==
// SourceKindGlobalArchive: a whole condemned branch tree under
// .wip/.archive/<branch-dir-name>[-N]/), per the Cluster I spec's §2.4.
//
// Repo identity: src.WorktreePath is the SAME real, current worktree root
// Discover/DiscoverArchived already receive — only the branch the archived
// tree once belonged to may no longer be checked out or exist in git at
// all, so state.RepoIdentity(src.WorktreePath) resolves exactly as it does
// for every other source under that root.
//
// Branch/stage identity comes from the archived tree's own status.json
// (same legacyStatus shape Convert reads), falling back to
// branchNameFromDirName(filepath.Base(src.SourceWipDir)) only when
// status.json is missing/unparseable — Convert's existing fallback order,
// reused verbatim. This intentionally does NOT strip any uniqueArchivePath
// collision suffix (e.g. "foo-2" stays "foo-2", never guessed down to
// "foo"): stripping a trailing "-N" is undecidable in general, and an
// honest, slightly-ugly fallback beats a heuristic that is wrong exactly as
// often as it's right.
//
// Head is deliberately left empty, never git.HeadSHA(src.WorktreePath):
// Convert's normal path uses the current worktree HEAD as best-effort
// because a LIVE branch dir's worktree is presumed to still be on (or near)
// that branch. For an archived entry that presumption is false by
// definition — the branch that owned this tree was condemned, and the
// worktree is very likely on a different current branch by the time
// migration runs. An honestly empty Head is correct; a plausible-looking
// wrong one is not.
//
// The one field-level deviation from Convert: r.Lifecycle is set to
// work.LifecycleArchived explicitly after work.New (which always defaults
// to LifecycleActive) — Convert relies on that default, ConvertArchived
// overrides it.
func ConvertArchived(src SourceDir) (*work.Record, error) {
	var status legacyStatus
	_ = util.ReadJSON(filepath.Join(src.SourceWipDir, "status.json"), &status)

	branchName := status.Branch
	if branchName == "" {
		branchName = branchNameFromDirName(filepath.Base(src.SourceWipDir))
	}

	repoID, err := state.RepoIdentity(src.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("resolve repo identity for %s: %w", src.WorktreePath, err)
	}
	normalizedWorktreePath, err := filepath.EvalSymlinks(src.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("resolve worktree path %s: %w", src.WorktreePath, err)
	}

	r := work.New(branchName, work.ModeDelivery)
	r.Lifecycle = work.LifecycleArchived
	r.Stage = status.Stage
	r.Attachments = append(r.Attachments, work.Attachment{
		RepoID:       repoID,
		WorktreePath: normalizedWorktreePath,
		Branch:       branchName,
		Head:         "",
	})
	return r, nil
}
