package procx

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func Inspect(pid int) (Identity, error) {
	entries, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		return Identity{}, err
	}
	if len(entries) == 0 {
		return Identity{}, syscall.ESRCH
	}
	p := entries[0]
	if p.Proc.P_pid != int32(pid) || p.Proc.P_stat == 5 {
		return Identity{}, syscall.ESRCH
	}
	return Identity{PID: pid, Start: fmt.Sprintf("%d:%d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec)}, nil
}

// proc_listchildpids uses PROC_INFO_CALL_LISTPIDS / PROC_PPID_ONLY. Query
// descendants directly rather than requiring permission to list every process.
// Apple ABI: xnu/libsyscall/wrappers/libproc/libproc.c.
func childPIDs(parent int) ([]int32, error) {
	const procInfoCallListPIDs = 1
	const procPPIDOnly = 6
	for capacity := 64; capacity <= 1<<20; capacity *= 2 {
		pids := make([]int32, capacity)
		size, _, errno := syscall.Syscall6(unix.SYS_PROC_INFO, procInfoCallListPIDs, procPPIDOnly, uintptr(parent), 0, uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)*4))
		runtime.KeepAlive(pids)
		if errno != 0 {
			return nil, errno
		}
		if size < uintptr(len(pids)*4) {
			return pids[:int(size)/4], nil
		}
	}
	return nil, fmt.Errorf("too many child processes for %d", parent)
}
func processSnapshot(root Identity) ([]processEntry, error) {
	queue := []Identity{root}
	var result []processEntry
	seen := map[int]bool{}
	for i := 0; i < len(queue); i++ {
		parent := queue[i]
		if seen[parent.PID] {
			continue
		}
		seen[parent.PID] = true
		alive, err := parent.Alive()
		if err != nil {
			return result, err
		}
		if !alive {
			continue
		}
		pids, err := childPIDs(parent.PID)
		if err != nil {
			return result, err
		}
		if alive, err = parent.Alive(); err != nil {
			return result, err
		} else if !alive {
			continue
		}
		if i == 0 {
			result = append(result, processEntry{Identity: root})
		}
		for _, pid := range pids {
			if pid <= 1 {
				continue
			}
			entries, err := unix.SysctlKinfoProcSlice("kern.proc.pid", int(pid))
			if err != nil {
				if err == syscall.ESRCH {
					continue
				}
				return result, err
			}
			if len(entries) == 0 {
				continue
			}
			p := entries[0]
			if int(p.Eproc.Ppid) != parent.PID || p.Proc.P_stat == 5 {
				continue
			}
			identity := Identity{PID: int(p.Proc.P_pid), Start: fmt.Sprintf("%d:%d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec)}
			result = append(result, processEntry{Identity: identity, parent: parent.PID})
			queue = append(queue, identity)
		}
	}
	return result, nil
}
