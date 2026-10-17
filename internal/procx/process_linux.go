package procx

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func Inspect(pid int) (Identity, error) {
	p, err := readLinuxProcess(pid)
	return p.Identity, err
}
func readLinuxProcess(pid int) (processEntry, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return processEntry{}, err
	}
	// comm is parenthesized and may contain spaces and closing parentheses.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return processEntry{}, fmt.Errorf("invalid process stat for %d", pid)
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return processEntry{}, fmt.Errorf("short process stat for %d", pid)
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return processEntry{}, syscall.ESRCH
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return processEntry{}, err
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return processEntry{}, fmt.Errorf("invalid process parent for %d: %w", pid, err)
	}
	return processEntry{Identity: Identity{PID: pid, Start: strings.TrimSpace(string(boot)) + ":" + fields[19]}, parent: parent}, nil
}

func processSnapshot(_ Identity) ([]processEntry, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var result []processEntry
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || !entry.IsDir() {
			continue
		}
		p, e := readLinuxProcess(pid)
		if e != nil {
			if skippableSnapshotError(e) {
				continue
			}
			return nil, e
		}
		result = append(result, p)
	}
	return result, nil
}

// skippableSnapshotError reports whether a process can be left out of a
// snapshot. Processes can exit during the snapshot: reading the stat file of a
// process that is exiting fails with ESRCH wrapped in a *PathError.
// Inaccessible processes cannot be signaled by this user; skip them just as
// vanished/zombie processes.
func skippableSnapshotError(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) || os.IsPermission(err)
}
