package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/util"
)

// RepoEntry records the known identities/paths of one registered repo.
type RepoEntry struct {
	Aliases        []string `json:"aliases"`
	LastKnownPaths []string `json:"last_known_paths"`
}

// ReposFile is the on-disk shape of $AIDW_STATE_DIR/repos.json.
type ReposFile struct {
	Repos map[string]RepoEntry `json:"repos"`
}

// LoadRepos reads repos.json under stateDir. A missing file is treated as
// an empty ReposFile, not an error.
func LoadRepos(stateDir string) (*ReposFile, error) {
	path := reposPath(stateDir)
	var rf ReposFile
	if err := util.ReadJSON(path, &rf); err != nil {
		if os.IsNotExist(err) {
			return &ReposFile{Repos: map[string]RepoEntry{}}, nil
		}
		return nil, fmt.Errorf("load repos.json: %w", err)
	}
	if rf.Repos == nil {
		rf.Repos = map[string]RepoEntry{}
	}
	return &rf, nil
}

func reposPath(stateDir string) string {
	return filepath.Join(stateDir, "repos.json")
}

// canonicalCommonDir returns the EvalSymlinks-normalized common git
// directory for path.
func canonicalCommonDir(path string) (string, error) {
	common, err := git.CommonDir(path)
	if err != nil {
		return "", fmt.Errorf("git common dir: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(common)
	if err != nil {
		return "", fmt.Errorf("eval symlinks: %w", err)
	}
	return resolved, nil
}

// RepoIdentity returns the repo_id for path: the EvalSymlinks-normalized
// canonical path of `git rev-parse --path-format=absolute --git-common-dir`
// for path (via git.CommonDir), so all worktrees of one clone resolve to
// the same repo_id (never keyed by basename or remote URL — two
// independent clones of the same remote are different repo_ids unless
// explicitly linked). Checks repos.json for an existing entry whose
// aliases or last_known_paths contain the canonical path first (future
// moved-repo support, design doc §10); if none matches, falls back to a
// deterministic derived ID: first 16 hex chars of sha256(canonical path).
// Pure lookup — never writes repos.json. Safe to call from lookup-only
// commands (work status, work list).
func RepoIdentity(path string) (string, error) {
	canonical, err := canonicalCommonDir(path)
	if err != nil {
		return "", err
	}

	stateDir := StateDir()
	rf, err := LoadRepos(stateDir)
	if err != nil {
		return "", err
	}
	for id, entry := range rf.Repos {
		if containsString(entry.Aliases, canonical) || containsString(entry.LastKnownPaths, canonical) {
			return id, nil
		}
	}

	return derivedRepoID(canonical), nil
}

func derivedRepoID(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])[:16]
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// RegisterRepo resolves path's repo_id by calling RepoIdentity itself (it
// must NOT re-implement the derivation, so the cmd layer never has to
// canonicalize a path twice and risk the two derivations diverging). Then
// ensures repos.json has an entry for that repo_id, appending the
// canonical path to LastKnownPaths if not already present. Locked
// (AcquireLock) + atomic (util.WriteJSON). Called only from work-model
// mutate paths (work start, work attach, checkpoint --from-hook) — never
// from status/list. Takes the same input as RepoIdentity (a path, not a
// pre-canonicalized string).
func RegisterRepo(stateDir, path string) (string, error) {
	repoID, err := RepoIdentity(path)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalCommonDir(path)
	if err != nil {
		return "", err
	}

	target := reposPath(stateDir)
	release, err := AcquireLock(target)
	if err != nil {
		return "", fmt.Errorf("register repo: %w", err)
	}
	defer release()

	rf, err := LoadRepos(stateDir)
	if err != nil {
		return "", err
	}
	entry := rf.Repos[repoID]
	if !containsString(entry.LastKnownPaths, canonical) {
		entry.LastKnownPaths = append(entry.LastKnownPaths, canonical)
	}
	rf.Repos[repoID] = entry

	if err := util.WriteJSON(target, rf); err != nil {
		return "", fmt.Errorf("register repo: write repos.json: %w", err)
	}
	return repoID, nil
}
