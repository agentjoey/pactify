package procx

import (
	"errors"
	"fmt"
	"syscall"
	"time"
)

// killWait bounds the wait after SIGKILL. The signal cannot be caught, so this
// only runs out when the target is not scheduled in time, as on a loaded machine.
const killWait = 2 * time.Second

// TerminateDescendants sends TERM then KILL to a saved snapshot, checking birth
// identity before every signal. The wait after TERM is bounded by grace; the wait
// after KILL by the longer of grace and killWait.
func TerminateDescendants(name string, descendants []Identity, grace time.Duration) error {
	if len(descendants) == 0 {
		return nil
	}
	var errs []error
	for _, p := range descendants {
		if err := p.Signal(syscall.SIGTERM, false); err != nil {
			errs = append(errs, fmt.Errorf("terminate %s descendant %d: %w", name, p.PID, err))
		}
	}
	remaining, err := WaitDescendants(name, descendants, grace)
	errs = append(errs, err)
	for _, p := range remaining {
		if err := p.Signal(syscall.SIGKILL, false); err != nil {
			errs = append(errs, fmt.Errorf("kill %s descendant %d: %w", name, p.PID, err))
		}
	}
	remaining, err = WaitDescendants(name, remaining, max(grace, killWait))
	errs = append(errs, err)
	for _, p := range remaining {
		errs = append(errs, fmt.Errorf("%s descendant %d is still alive after SIGKILL", name, p.PID))
	}
	return errors.Join(errs...)
}

// WaitDescendants waits for saved identities to exit without sending a signal.
// It also returns inspection errors, so unknown processes are never signaled.
func WaitDescendants(name string, processes []Identity, grace time.Duration) ([]Identity, error) {
	deadline := time.Now().Add(grace)
	var errs []error
	for {
		remaining := make([]Identity, 0, len(processes))
		for _, p := range processes {
			alive, err := p.Alive()
			if err != nil {
				errs = append(errs, fmt.Errorf("inspect %s descendant %d: %w", name, p.PID, err))
			} else if alive {
				remaining = append(remaining, p)
			}
		}
		if len(remaining) == 0 || !time.Now().Before(deadline) {
			return remaining, errors.Join(errs...)
		}
		processes = remaining
		time.Sleep(10 * time.Millisecond)
	}
}
