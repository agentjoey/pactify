//go:build harness_e2e && linux

package e2e

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentjoey/pactify/internal/procx"
)

// /proc/PID/comm is process metadata, not the credential-bearing environment.
func isSleep(p procx.Identity) (bool, error) {
	alive, err := p.Alive()
	if err != nil || !alive {
		return false, err
	}
	name, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", p.PID))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	alive, err = p.Alive()
	return alive && strings.TrimSpace(string(name)) == "sleep", err
}
