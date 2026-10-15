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
		p.interruptErr = errors.Join(snapshotErr, procx.TerminateDescendants("agy", descendants, grace))
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
