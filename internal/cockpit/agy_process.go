package cockpit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
)

// A turn owns its cancellation, including descendants that outlive the CLI.
// Explicit Interrupt, the context watcher and Close share the same cleanup.
type agyProcess struct {
	cmd           *exec.Cmd
	stdout        io.ReadCloser
	done          chan struct{}
	stop          context.CancelFunc
	identity      procx.Identity
	identityErr   error
	interruptOnce sync.Once
	interruptDone chan struct{}
	cancelling    atomic.Bool
	interruptErr  error
}

func (p *agyProcess) interrupt(ctx context.Context, grace time.Duration) error {
	p.interruptOnce.Do(func() {
		defer close(p.interruptDone)
		select {
		case <-p.done:
			return
		default:
		}
		p.cancelling.Store(true)
		p.stop()
		// Snapshot before SIGINT: agy may exit immediately and reparent its tools.
		var descendants []procx.Identity
		var snapshotErr error
		if p.identityErr != nil {
			snapshotErr = p.identityErr
		} else {
			descendants, snapshotErr = procx.Descendants(p.identity)
		}
		if snapshotErr != nil {
			snapshotErr = fmt.Errorf("snapshot agy descendants before cancellation: %w", snapshotErr)
		}
		_ = p.cmd.Process.Signal(os.Interrupt)
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-p.done:
		case <-timer.C:
			p.killParent()
		case <-ctx.Done():
			p.killParent()
		}
		<-p.done
		// Even a prompt SIGINT exit may leave children in their own groups/sessions.
		p.interruptErr = errors.Join(snapshotErr, terminateAgyDescendants(descendants, grace))
	})
	return p.interruptErr
}
func (p *agyProcess) killParent() {
	_ = p.identity.Signal(syscall.SIGKILL, false)
	_ = p.cmd.Process.Kill()
	// A surviving child can retain a pipe after the CLI exits. Release Scan;
	// Cmd.WaitDelay bounds stderr drain while we collect the known descendants.
	_ = p.stdout.Close()
}
func terminateAgyDescendants(descendants []procx.Identity, grace time.Duration) error {
	if len(descendants) == 0 {
		return nil
	}
	var errs []error
	for _, p := range descendants {
		if err := p.Signal(syscall.SIGTERM, false); err != nil {
			errs = append(errs, fmt.Errorf("terminate agy descendant %d: %w", p.PID, err))
		}
	}
	remaining, err := waitAgyDescendants(descendants, grace)
	errs = append(errs, err)
	for _, p := range remaining {
		if err := p.Signal(syscall.SIGKILL, false); err != nil {
			errs = append(errs, fmt.Errorf("kill agy descendant %d: %w", p.PID, err))
		}
	}
	remaining, err = waitAgyDescendants(remaining, grace)
	errs = append(errs, err)
	for _, p := range remaining {
		errs = append(errs, fmt.Errorf("agy descendant %d is still alive after SIGKILL", p.PID))
	}
	return errors.Join(errs...)
}
func waitAgyDescendants(processes []procx.Identity, grace time.Duration) ([]procx.Identity, error) {
	deadline := time.Now().Add(grace)
	var errs []error
	for {
		remaining := make([]procx.Identity, 0, len(processes))
		for _, p := range processes {
			alive, err := p.Alive()
			if err != nil {
				errs = append(errs, fmt.Errorf("inspect agy descendant %d: %w", p.PID, err))
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
