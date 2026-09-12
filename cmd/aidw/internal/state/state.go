// Package state owns lower-level, work-model-agnostic infrastructure:
// where durable aidw state lives on disk (ExecutionRoot, StateDir), how
// concurrent writers to a single file are serialized (AcquireLock), and how
// a filesystem path is mapped to a stable repo identity (RepoIdentity,
// RegisterRepo, repos.json). state has zero knowledge of work.Record or the
// wip package; work imports state, never the reverse.
package state

import (
	"os"
	"path/filepath"

	"aidw/cmd/aidw/internal/git"
)

// ExecutionRoot is a thin pass-through to git.Toplevel. It is worktree-
// local and intentionally identical in behavior to every existing
// git.Toplevel call site (wip.go, review.go, bootstrap.go, document.go) —
// those call sites are NOT changed to use this function; this exists so
// new work-model code has one documented name for "where diffs/paths are
// relative to" instead of importing git directly and re-deriving intent.
func ExecutionRoot(path string) (string, error) {
	return git.Toplevel(path)
}

// StateDir returns the root directory for all aidw durable state:
// $AIDW_STATE_DIR if set (and non-empty), else
// ${XDG_STATE_HOME:-$HOME/.local/state}/aidw. No macOS-specific Application
// Support branch (design doc §3).
//
// The result is always absolute. A relative $AIDW_STATE_DIR or
// $XDG_STATE_HOME, or an unset $HOME, would otherwise produce a path
// interpreted against the process's current directory — which for a tool
// invoked from arbitrary repos means state silently landing in a different
// place per invocation.
func StateDir() string {
	if v := os.Getenv("AIDW_STATE_DIR"); v != "" {
		return absOrSelf(v)
	}
	xdg := os.Getenv("XDG_STATE_HOME")
	if xdg == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			// No HOME at all: fall back to the OS temp dir rather than a
			// path relative to the current working directory.
			home = filepath.Join(os.TempDir(), "aidw-home")
		}
		xdg = filepath.Join(home, ".local", "state")
	}
	return absOrSelf(filepath.Join(xdg, "aidw"))
}

// absOrSelf makes p absolute against the current directory, returning p
// unchanged only if that resolution fails (an unreadable cwd), since a
// relative path is still more useful than an empty one.
func absOrSelf(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
