package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// AcquireLock acquires an exclusive advisory lock guarding path (a real
// target file, e.g. .../work.json) by opening path+".lock" and taking a
// non-blocking exclusive flock(2) on the resulting file descriptor.
//
// Why a real OS lock rather than a lock file plus a staleness/steal
// heuristic: any scheme built on "check something, then remove the lock
// file by path" is a TOCTOU race, because the check and the remove are
// separate syscalls. Content-token ownership narrows the window but does
// not close it — two concurrent stealers can read the same stale token,
// both pass the check, and both believe they hold the lock. flock(2) is
// arbitrated by the kernel, so exactly one holder wins, always.
//
// Two properties this gives us for free:
//   - LOCK_NB fails immediately on contention rather than waiting, which is
//     exactly the "no blocking/waiting" contract in docs/design/work-model.md
//     §5 that callers (work.Save, work.SaveSessionBinding) rely on.
//   - The kernel releases the lock when the holding process exits for any
//     reason, including SIGKILL. That is real crash safety, and it is what
//     removes any need for staleness thresholds, ownership tokens, or
//     stealing.
//
// The lock file is intentionally left on disk after release. flock locks a
// file description, not a name or a byte of content, so the file can stay
// empty; unlinking it would reintroduce a two-winner race (a holder could
// unlink inode X while another acquirer already holds X open, and a third
// acquirer would then create and lock a fresh inode).
//
// Note that flock is per open file description, not per process: two
// AcquireLock calls in the same process open the lock file separately and
// therefore contend with each other correctly.
func AcquireLock(path string) (release func(), err error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("acquire lock: mkdir: %w", err)
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: open: %w", err)
	}

	if err := flockNB(int(f.Fd())); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			// syscall.EAGAIN == syscall.EWOULDBLOCK on both linux and
			// darwin, so this one branch covers both spellings.
			return nil, fmt.Errorf("acquire lock: %s is held", lockPath)
		}
		return nil, fmt.Errorf("acquire lock: flock: %w", err)
	}

	// sync.Once so a double release() (e.g. an explicit call plus a
	// deferred one) cannot unlock a descriptor number that has since been
	// reused by an unrelated open.
	var once sync.Once
	release = func() {
		once.Do(func() {
			// Closing the fd would release the flock on its own, but
			// unlock explicitly so the intent is not implicit in close
			// semantics.
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
		})
	}
	return release, nil
}

// flockNB takes a non-blocking exclusive flock on fd, retrying only on
// EINTR (a signal arriving mid-syscall is not a contention signal).
func flockNB(fd int) error {
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}
