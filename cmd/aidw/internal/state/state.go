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
func StateDir() string {
	if v := os.Getenv("AIDW_STATE_DIR"); v != "" {
		return v
	}
	xdg := os.Getenv("XDG_STATE_HOME")
	if xdg == "" {
		xdg = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(xdg, "aidw")
}
