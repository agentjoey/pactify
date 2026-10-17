//go:build harness_e2e && darwin

package e2e

import (
	"github.com/agentjoey/pactify/internal/procx"
	"golang.org/x/sys/unix"
)

// Read process metadata only, never argv or environment (which may hold keys).
func isSleep(p procx.Identity) (bool, error) {
	alive, err := p.Alive()
	if err != nil || !alive {
		return false, err
	}
	rows, err := unix.SysctlKinfoProcSlice("kern.proc.pid", p.PID)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	var name []byte
	for _, c := range rows[0].Proc.P_comm {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	alive, err = p.Alive()
	return alive && string(name) == "sleep", err
}
