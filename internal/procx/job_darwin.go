package procx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func userProcesses() ([]Identity, error) {
	// proc_listpids supports UID-scoped enumeration even in seatbelt profiles
	// which deny the broader kern.proc.uid sysctl.
	const procUIDOnly = 4
	pids, err := listPIDs(procUIDOnly, os.Getuid())
	if err != nil {
		return nil, err
	}
	var processes []Identity
	for _, pid := range pids {
		entries, err := unix.SysctlKinfoProcSlice("kern.proc.pid", int(pid))
		if err != nil || len(entries) == 0 {
			continue
		}
		p := entries[0]
		if p.Proc.P_pid <= 1 || p.Proc.P_stat == 5 || p.Eproc.Ucred.Uid != uint32(os.Getuid()) {
			continue
		}
		processes = append(processes, Identity{PID: int(p.Proc.P_pid), Start: fmt.Sprintf("%d:%d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec)})
	}
	return processes, nil
}

func processEnvironment(pid int) ([]string, error) {
	data, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	return parseProcessArgs(data)
}

// KERN_PROCARGS2 contains argc, the executable path, NUL padding, argc argument
// strings, then NUL-separated environment entries. Arguments are never markers.
func parseProcessArgs(data []byte) ([]string, error) {
	invalid := fmt.Errorf("invalid KERN_PROCARGS2 data")
	if len(data) < 4 {
		return nil, invalid
	}
	argc := binary.NativeEndian.Uint32(data[:4])
	data = data[4:]
	end := bytes.IndexByte(data, 0)
	if argc == 0 || end < 0 {
		return nil, invalid
	}
	data = bytes.TrimLeft(data[end+1:], "\x00")
	for i := uint32(0); i < argc; i++ {
		end = bytes.IndexByte(data, 0)
		if end < 0 {
			return nil, invalid
		}
		data = data[end+1:]
	}
	var env []string
	for len(data) > 0 && data[0] != 0 {
		end = bytes.IndexByte(data, 0)
		if end < 0 {
			return nil, invalid
		}
		env = append(env, string(data[:end]))
		data = data[end+1:]
	}
	return env, nil
}
