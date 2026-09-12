package work

import (
	"fmt"
	"os"
	"path/filepath"

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
