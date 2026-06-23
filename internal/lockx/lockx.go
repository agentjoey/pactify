// Package lockx provides a cross-process advisory file lock (flock(2)). pact uses
// it to serialize writes to a repo's base branch: every `pactify merge` acquires
// the same lock before its checkout→merge→commit critical section, so concurrent
// merges (different processes, or different git worktrees sharing one .git)
// queue instead of racing — the base-integration lane of spec coordination-authority P1.
package lockx

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Acquire takes an exclusive advisory lock on path (creating it and parents),
// blocking until the lock is free or ctx is cancelled. It returns a release func
// that unlocks and closes the handle. The lock is held by the OS against the file,
// so independent processes — and worktrees pointing at the same lock file —
// serialize on it. flock is advisory: it only excludes other flock holders, which
// is exactly the set of pactify merges.
func Acquire(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if err != syscall.EWOULDBLOCK {
			_ = f.Close()
			return nil, err
		}
		// Lock is held elsewhere — wait and retry so the caller queues rather than
		// failing, honouring ctx cancellation/timeout.
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}
}
