package cockpit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
)

// A turn owns its cancellation, including descendants that outlive the CLI.
// Explicit Interrupt, the context watcher and Close share the same cleanup.
type agyProcess struct {
	observe       func(string, []byte)
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	lines         chan json.RawMessage
	ctx           context.Context
	err           error
	diagnostics   *agyDiagnostics
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
		_, file, line, _ := runtime.Caller(1)
		fmt.Fprintf(p.diagnostics, "pactify interrupt requested at %s:%d\n", file, line)
		p.cancelling.Store(true)
		p.stop()
		// Snapshot before EOF or SIGINT: agy may exit immediately and reparent its tools.
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
		_ = p.stdin.Close()
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

func (p *agyProcess) send(text string) error {
	var frame bytes.Buffer
	err := json.NewEncoder(&frame).Encode(struct {
		Event   string `json:"event"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}{Event: "user", Message: struct {
		Content string `json:"content"`
	}{text}})
	if err != nil {
		return err
	}
	if p.observe != nil {
		p.observe("stdin", frame.Bytes())
	}
	_, err = p.stdin.Write(frame.Bytes())
	return err
}
func (p *agyProcess) read() {
	defer close(p.lines)
	defer close(p.done)
	scanner := bufio.NewScanner(p.stdout)
	scanner.Buffer(make([]byte, 65536), 16*1024*1024)
	for scanner.Scan() {
		line := append(json.RawMessage(nil), scanner.Bytes()...)
		if p.observe != nil {
			p.observe("stdout", line)
		}
		select {
		case p.lines <- line:
		case <-p.ctx.Done():
		}
	}
	scanErr := scanner.Err()
	p.err = errors.Join(scanErr, p.cmd.Wait())
	_ = p.stdout.Close()
	_ = p.stdin.Close()
}
