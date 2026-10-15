package procx

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

func Inspect(pid int) (Identity, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return Identity{}, err
	}
	// comm is parenthesized and may contain spaces and closing parentheses.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return Identity{}, fmt.Errorf("invalid process stat for %d", pid)
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return Identity{}, fmt.Errorf("short process stat for %d", pid)
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return Identity{}, syscall.ESRCH
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return Identity{}, err
	}
	return Identity{PID: pid, Start: strings.TrimSpace(string(boot)) + ":" + fields[19]}, nil
}
