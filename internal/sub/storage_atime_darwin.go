package sub

import (
	"os"
	"syscall"
	"time"
)

func storageAccessTime(info os.FileInfo) time.Time {
	t := info.ModTime()
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		t = maxStorageTime(t, time.Unix(st.Atimespec.Sec, st.Atimespec.Nsec))
	}
	return t
}
