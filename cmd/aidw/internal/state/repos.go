package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

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

// reposLockAttempts / reposLockBackoff bound the retry around repos.json.
// Total worst-case wait is well under half a second — this is a fast,
// low-contention append, not something worth waiting seconds for.
const (
	reposLockAttempts = 8
	reposLockBackoff  = 15 * time.Millisecond
)

// acquireReposLock wraps AcquireLock with a small jittered retry.
//
// This retry is deliberately scoped to repos.json and NOT added to
// AcquireLock itself. work/<id>/work.json's "fail immediately, never
// block" behavior is the documented contract for that resource (design doc
// §5) and the work package depends on it. repos.json is different in kind:
// one global file with many legitimately concurrent writers from unrelated
// repos, where failing fast buys no data safety and just turns two
// simultaneous `work start`s in different repos into a spurious error.
func acquireReposLock(target string) (func(), error) {
	var lastErr error
	for attempt := 0; attempt < reposLockAttempts; attempt++ {
		release, err := AcquireLock(target)
		if err == nil {
			return release, nil
		}
		lastErr = err
		if attempt == reposLockAttempts-1 {
			break
		}
		// Jitter so a thundering herd of starts does not resynchronize on
		// every retry.
		jitter := time.Duration(rand.Int63n(int64(reposLockBackoff)))
		time.Sleep(reposLockBackoff + jitter)
	}
	return nil, fmt.Errorf("repos.json lock busy after %d attempts: %w", reposLockAttempts, lastErr)
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
// commands (work status, work list). Resolves against the global
// StateDir(); repoIdentityIn is the same logic against an explicit
// stateDir, so RegisterRepo(stateDir, ...) cannot silently consult a
// different repos.json than the one it writes.
func RepoIdentity(path string) (string, error) {
	return repoIdentityIn(StateDir(), path)
}

// repoIdentityIn is RepoIdentity against an explicit stateDir. Shared by
// RepoIdentity and RegisterRepo so the stateDir a caller passes is honored
// end to end.
func repoIdentityIn(stateDir, path string) (string, error) {
	canonical, err := canonicalCommonDir(path)
	if err != nil {
		return "", err
	}

	rf, err := LoadRepos(stateDir)
	if err != nil {
		return "", err
	}
	// Iterate in sorted key order: map iteration is randomized, so if two
	// entries ever both claim the same canonical path the winner would
	// otherwise differ run to run. Sorted order makes the resolution
	// deterministic, and the duplicate is reported so it can be fixed
	// rather than silently tolerated.
	ids := make([]string, 0, len(rf.Repos))
	for id := range rf.Repos {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	match := ""
	for _, id := range ids {
		entry := rf.Repos[id]
		if containsString(entry.Aliases, canonical) || containsString(entry.LastKnownPaths, canonical) {
			if match != "" {
				return "", fmt.Errorf("repos.json: canonical path %s is claimed by more than one repo_id (%s and %s); resolve the duplicate registration", canonical, match, id)
			}
			match = id
		}
	}
	if match != "" {
		return match, nil
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
// mutate paths (work start, work attach) — never from status/list.
// checkpoint --from-hook uses RepoIdentity directly, not RegisterRepo,
// since a hook fire must stay a pure lookup when there's no bound work.
// Takes the same input as RepoIdentity (a path, not a pre-canonicalized
// string).
func RegisterRepo(stateDir, path string) (string, error) {
	repoID, err := repoIdentityIn(stateDir, path)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalCommonDir(path)
	if err != nil {
		return "", err
	}

	target := reposPath(stateDir)
	release, err := acquireReposLock(target)
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
