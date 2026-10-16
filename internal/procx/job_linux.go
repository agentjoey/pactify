package procx

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func userProcesses() ([]Identity, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var processes []Identity
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || !entry.IsDir() {
			continue
		}
		p, err := Inspect(pid)
		if err != nil {
			continue
		}
		info, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
		if err != nil {
			continue
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid == uint32(os.Getuid()) {
			processes = append(processes, p)
		}
	}
	return processes, nil
}

func processEnvironment(pid int) ([]string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\x00"), nil
}
