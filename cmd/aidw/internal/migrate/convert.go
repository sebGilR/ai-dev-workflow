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
