package work

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aidw/cmd/aidw/internal/state"
	"aidw/cmd/aidw/internal/util"
)

// SessionBinding records that a session has resolved to a particular work
// item.
type SessionBinding struct {
	SessionID string `json:"session_id"`
	WorkID    string `json:"work_id"`
	BoundAt   string `json:"bound_at"`
}

// SessionPath returns the on-disk path for a session's binding file.
func SessionPath(stateDir, sessionID string) string {
	return filepath.Join(stateDir, "sessions", sessionID+".json")
}

// SaveSessionBinding acquires state.AcquireLock on the session's file,
// writes {session_id, work_id, bound_at} via util.WriteJSON. Because each
// session has its own file keyed by session_id, two different sessions
// binding to the same work_id never contend with or overwrite each other
// (bindings are additive, never evict).
func SaveSessionBinding(stateDir, sessionID, workID string) error {
	path := SessionPath(stateDir, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("save session binding: mkdir: %w", err)
	}

	release, err := state.AcquireLock(path)
	if err != nil {
		return fmt.Errorf("save session binding: %w", err)
	}
	defer release()

	binding := SessionBinding{
		SessionID: sessionID,
		WorkID:    workID,
		BoundAt:   util.NowISO(),
	}
	if err := util.WriteJSON(path, binding); err != nil {
		return fmt.Errorf("save session binding: write: %w", err)
	}
	return nil
}

// LoadSessionBinding returns (nil, nil) if no binding file exists for
// sessionID — this is a normal "not bound yet" outcome, not an error.
func LoadSessionBinding(stateDir, sessionID string) (*SessionBinding, error) {
	path := SessionPath(stateDir, sessionID)
	var b SessionBinding
	if err := util.ReadJSON(path, &b); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load session binding %s: %w", sessionID, err)
	}
	return &b, nil
}

// listSessionIDs enumerates sessions/*.json under stateDir, returning each
// file's session id (basename minus ".json"). A missing sessions/ dir
// contributes an empty list, not an error — mirrors ScanRecords'/Discover's
// "missing dir means nothing found" convention.
func listSessionIDs(stateDir string) ([]string, error) {
	dir := filepath.Join(stateDir, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list session bindings: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	return ids, nil
}

// afterListSessionIDsHook is nil in production. See DeleteSessionBindingsFor's
// doc comment.
var afterListSessionIDsHook func(sid string)

// SessionIDsBoundTo returns every session id currently bound to workID,
// without deleting anything — a read-only preview used by `work purge
// <id> --dry-run` to show what DeleteSessionBindingsFor would reap. Unlike
// DeleteSessionBindingsFor, this does not need lock-then-reread discipline:
// a preview that is stale by the time a real (non-dry-run) purge runs is
// not a correctness bug, only an ordinary preview/apply race any --dry-run
// flag already carries.
func SessionIDsBoundTo(stateDir, workID string) ([]string, error) {
	ids, err := listSessionIDs(stateDir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, sid := range ids {
		binding, loadErr := LoadSessionBinding(stateDir, sid)
		if loadErr == nil && binding != nil && binding.WorkID == workID {
			out = append(out, sid)
		}
	}
	return out, nil
}

// DeleteSessionBindingsFor removes every sessions/<sid>.json binding
// currently pointing at workID, and returns the session ids it actually
// removed. Used by `work purge <id>` (§2.5) to close the one hole that
// operation newly opens: a dangling binding whose work_id no longer exists
// on disk at all.
//
// It DECIDES under the lock, not before it: listSessionIDs' enumeration is
// unlocked (there is nothing to protect there — it is only a candidate
// list), but for each candidate session id it acquires state.AcquireLock on
// that session file's own path FIRST, then re-LoadSessionBinding INSIDE the
// held lock, then checks WorkID == workID against that freshly-read value,
// and only os.Removes if it still matches, before releasing. This mirrors
// UpdateRecord's load-mutate-save-under-one-lock-hold shape rather than a
// blind read-then-write: a session rebound (via a concurrent
// SaveSessionBinding) between the unlocked listing and this function's lock
// acquisition on that session's path must not have its now-unrelated
// binding deleted anyway. Deciding under the lock closes that window: any
// concurrent SaveSessionBinding on the same session id either completes
// entirely before this function's lock acquisition (so the fresh read sees
// the new WorkID and correctly skips it) or waits for this function's lock
// to release first (so it always writes after any stale delete decision was
// already discarded).
//
// A lock-acquire failure on one session file (another process mid-rebind)
// is NOT fatal to the overall reap — that file is simply skipped (not
// included in removed); the record itself is already gone by the time this
// runs, so there is nothing this call needs to retry or die over.
//
// Test seam (nil in production): afterListSessionIDsHook, when set, is
// called once per candidate id between this function's unlocked listing and
// its lock-acquire on that id's path — the exact window
// AC-I2-SESSIONREAP-RACE exists to close. A test can rebind the session
// inside the hook to exercise the decide-under-lock fix directly, rather
// than relying on the (behaviorally identical, but not race-discriminating)
// "reap twice" form.
func DeleteSessionBindingsFor(stateDir, workID string) (removed []string, err error) {
	ids, err := listSessionIDs(stateDir)
	if err != nil {
		return nil, err
	}

	removed = []string{}
	for _, sid := range ids {
		if afterListSessionIDsHook != nil {
			afterListSessionIDsHook(sid)
		}
		path := SessionPath(stateDir, sid)
		release, lockErr := state.AcquireLock(path)
		if lockErr != nil {
			// Another process is mid-rebind of this exact session — skip it
			// rather than fail the whole reap.
			continue
		}
		binding, loadErr := LoadSessionBinding(stateDir, sid)
		if loadErr != nil || binding == nil || binding.WorkID != workID {
			release()
			continue
		}
		if rmErr := os.Remove(path); rmErr == nil {
			removed = append(removed, sid)
		}
		release()
	}
	return removed, nil
}
