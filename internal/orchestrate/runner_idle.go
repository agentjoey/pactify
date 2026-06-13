package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// errIdle classifies an idle-timeout kill so the loop can treat it as a soft
// failure (kill + retry the worker — the correct recovery model, not
// orchestrator-takeover) distinct from a context cancellation or a real spawn
// error. The backlog "error-handling design" calls idle-timeout the precise fix
// for a hung agent that the blunt total RunTimeout over- or under-shoots.
var errIdle = errors.New("agent idle timeout")

// idleTracker records the wall-clock time of the most recent write so a watchdog
// can tell a hung agent (no output for a while) apart from a merely slow one
// (still emitting output). It is an io.Writer spliced into the child's stdio.
type idleTracker struct {
	mu   sync.Mutex
	last time.Time
}

func (t *idleTracker) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.last = time.Now()
	t.mu.Unlock()
	return len(p), nil
}

func (t *idleTracker) idleFor() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return time.Since(t.last)
}

// osExecIdle returns an execFn that kills the child if it produces no output for
// idle, returning errIdle. idle<=0 disables the watchdog and delegates to the
// plain osExec. The child's stdout/stderr are tee'd to the parent AND to an
// idleTracker; a watchdog polls the tracker and kills on a stall. A parent-ctx
// cancellation still propagates via CommandContext (and is NOT errIdle).
func osExecIdle(idle time.Duration) execFn {
	if idle <= 0 {
		return osExec
	}
	return func(ctx context.Context, name string, args []string, dir string, env []string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		cmd.Stdin = os.Stdin
		tr := &idleTracker{last: time.Now()}
		cmd.Stdout = io.MultiWriter(os.Stdout, tr)
		cmd.Stderr = io.MultiWriter(os.Stderr, tr)

		if err := cmd.Start(); err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		// Poll a few times per idle window so the kill latency is a fraction of it,
		// with a floor so a tiny idle window doesn't spin the CPU.
		interval := idle / 4
		if interval < 50*time.Millisecond {
			interval = 50 * time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case err := <-done:
				return err
			case <-ticker.C:
				if tr.idleFor() >= idle {
					_ = cmd.Process.Kill()
					<-done // reap the killed process
					return fmt.Errorf("%w: no output for %s — killed", errIdle, idle)
				}
			}
		}
	}
}
