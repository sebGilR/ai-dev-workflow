package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LockStaleThreshold is how old a .lock file must be before a new acquirer
// treats it as abandoned (e.g. the process holding it crashed) and steals
// it rather than waiting forever.
const LockStaleThreshold = 30 * time.Second

// AcquireLock acquires an exclusive lock on path (a real target file, e.g.
// .../work.json) by creating path+".lock" with O_CREATE|O_EXCL, writing a
// unique ownership token into the lock file's content at creation time. The
// token — not the bare existence or path of the lock file — is what
// authorizes removal, closing a TOCTOU race in the steal path below: if
// this were "check mtime, then blind-remove-by-path", two concurrent
// stealers could each see the same stale lock, both remove it, and both
// believe they hold it (and a later release() on the original, stolen-from
// holder would then delete whichever of the two is the current legitimate
// holder). With a token: before any removal (steal or release), AcquireLock
// / release re-reads the lock file's current content and only removes it if
// that content still matches the token this call itself is trying to
// steal/release — never removes by path alone.
func AcquireLock(path string) (release func(), err error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("acquire lock: mkdir: %w", err)
	}

	token, err := createLock(lockPath)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire lock: %w", err)
		}
		// Lock file already exists — check staleness.
		info, statErr := os.Stat(lockPath)
		if statErr != nil {
			return nil, fmt.Errorf("acquire lock: stat existing: %w", statErr)
		}
		if time.Since(info.ModTime()) <= LockStaleThreshold {
			return nil, fmt.Errorf("acquire lock: %s is held", lockPath)
		}

		existingToken, readErr := os.ReadFile(lockPath)
		if readErr != nil && !os.IsNotExist(readErr) {
			return nil, fmt.Errorf("acquire lock: read stale lock: %w", readErr)
		}
		fmt.Fprintf(os.Stderr, "[aidw] stealing stale lock: %s\n", lockPath)
		if err := removeIfMatches(lockPath, string(existingToken)); err != nil {
			return nil, fmt.Errorf("acquire lock: remove stale lock: %w", err)
		}

		token, err = createLock(lockPath)
		if err != nil {
			return nil, fmt.Errorf("acquire lock: retry after steal: %w", err)
		}
	}

	release = func() {
		_ = removeIfMatches(lockPath, token)
	}
	return release, nil
}

// createLock creates lockPath with O_CREATE|O_EXCL, writes a fresh
// ownership token into it, and returns that token.
func createLock(lockPath string) (string, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	token := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	_, writeErr := f.WriteString(token)
	closeErr := f.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return token, nil
}

// removeIfMatches removes lockPath only if its current content still
// equals token, tolerating the file already being gone (ENOENT). It never
// removes by path alone.
func removeIfMatches(lockPath, token string) error {
	current, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if string(current) != token {
		// Someone else's lock now — not ours to remove.
		return nil
	}
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
