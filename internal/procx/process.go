// Package procx identifies local processes without relying on reusable PIDs alone.
package procx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Identity includes the kernel's process birth time (and boot identity on Linux).
// An absent identity is never authority to signal a process.
type Identity struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

func (p Identity) Alive() (bool, error) {
	if p.PID <= 1 || p.Start == "" {
		return false, nil
	}
	current, err := Inspect(p.PID)
	if errors.Is(err, syscall.ESRCH) || os.IsNotExist(err) {
		return false, nil
	}
	return current == p && err == nil, err
}

// Signal rechecks identity immediately before each signal. Group signaling also
// requires the identified process to still lead that group.
func (p Identity) Signal(sig syscall.Signal, group bool) error {
	alive, err := p.Alive()
	if err != nil || !alive {
		return err
	}
	target := p.PID
	if group {
		pgid, e := syscall.Getpgid(p.PID)
		if errors.Is(e, syscall.ESRCH) {
			return nil
		}
		if e != nil {
			return e
		}
		if pgid != p.PID {
			return errors.New("job process is not its process group leader")
		}
		target = -target
	}
	alive, err = p.Alive()
	if err != nil || !alive {
		return err
	}
	err = syscall.Kill(target, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

type launcherKey struct{}

// WithLauncher scopes process registration to a delegated job. Other cockpit
// consumers retain exec.Cmd.Start's behavior.
func WithLauncher(ctx context.Context, launch func(*exec.Cmd) error) context.Context {
	return context.WithValue(ctx, launcherKey{}, launch)
}
func Start(ctx context.Context, cmd *exec.Cmd) error {
	if launch, ok := ctx.Value(launcherKey{}).(func(*exec.Cmd) error); ok {
		return launch(cmd)
	}
	return cmd.Start()
}
