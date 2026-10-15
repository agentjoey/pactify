package procx

import (
	"fmt"
	"syscall"

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
