package acp

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
)

const (
	acpCancelGrace     = 3 * time.Second
	acpDescendantGrace = 250 * time.Millisecond
)

// spawnedProcess owns teardown independently of the JSON-RPC reader. Cancel
// snapshots before notifying the peer, which may immediately exit and reparent
// tools in separate groups/sessions. Close snapshots again before killing it.
type spawnedProcess struct {
	cmd         *exec.Cmd
	stdin       io.Closer
	stdout      io.Closer
	reaped      chan struct{}
	identity    procx.Identity
	identityErr error

	mu             sync.Mutex
	descendants    map[procx.Identity]struct{}
	snapshotErr    error
	cancelDeadline time.Time
	killed         bool
	killOnce       sync.Once
	closeOnce      sync.Once
	closeErr       error
}

func (p *spawnedProcess) snapshotLocked() {
	if p.identityErr != nil {
		if !errors.Is(p.identityErr, syscall.ESRCH) {
			p.snapshotErr = errors.Join(p.snapshotErr, p.identityErr)
		}
		return
	}
	descendants, err := procx.Descendants(p.identity)
	if err != nil {
		p.snapshotErr = errors.Join(p.snapshotErr, fmt.Errorf("snapshot ACP descendants: %w", err))
	}
	for _, identity := range descendants {
		p.descendants[identity] = struct{}{}
	}
}
func (p *spawnedProcess) beforeCancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.killed {
		return
	}
	p.snapshotLocked()
	if p.cancelDeadline.IsZero() {
		p.cancelDeadline = time.Now().Add(acpCancelGrace)
	}
}
func (p *spawnedProcess) snapshot() []procx.Identity {
	descendants := make([]procx.Identity, 0, len(p.descendants))
	for identity := range p.descendants {
		descendants = append(descendants, identity)
	}
	return descendants
}
func (p *spawnedProcess) waitForCancelCleanup() {
	p.mu.Lock()
	grace := time.Until(p.cancelDeadline)
	descendants := p.snapshot()
	killed := p.killed
	p.mu.Unlock()
	if !killed && grace > 0 && len(descendants) > 0 {
		// Keep the transport writable: session/cancel may still be queued, and a
		// canceled local Prompt context is not a remote cancellation acknowledgment.
		_, _ = procx.WaitDescendants("ACP", descendants, grace)
	}
}
func (p *spawnedProcess) kill() {
	p.killOnce.Do(func() {
		p.mu.Lock()
		p.snapshotLocked()
		p.killed = true
		p.mu.Unlock()
		_ = p.stdin.Close()
		_ = p.identity.Signal(syscall.SIGKILL, true)
		_ = p.cmd.Process.Kill()
		// Detached tools may retain output pipes. Release the reader immediately;
		// Cmd.WaitDelay separately bounds stderr drain before descendant cleanup.
		_ = p.stdout.Close()
	})
}
func (p *spawnedProcess) close() error {
	p.closeOnce.Do(func() {
		p.kill()
		<-p.reaped
		p.mu.Lock()
		descendants, snapshotErr := p.snapshot(), p.snapshotErr
		p.mu.Unlock()
		p.closeErr = errors.Join(snapshotErr, procx.TerminateDescendants("ACP", descendants, acpDescendantGrace))
	})
	return p.closeErr
}
