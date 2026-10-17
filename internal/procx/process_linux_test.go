package procx

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestSnapshotSkipsProcessExitingDuringRead(t *testing.T) {
	// The CI failure: read /proc/<pid>/stat: no such process.
	exiting := &os.PathError{Op: "read", Path: "/proc/14188/stat", Err: syscall.ESRCH}
	for _, err := range []error{exiting, syscall.ESRCH, &os.PathError{Op: "open", Path: "/proc/1/stat", Err: syscall.ENOENT}, &os.PathError{Op: "open", Path: "/proc/1/stat", Err: syscall.EACCES}} {
		if !skippableSnapshotError(err) {
			t.Errorf("%v was not skipped", err)
		}
	}
	if skippableSnapshotError(errors.New("invalid process stat for 1")) {
		t.Error("a malformed stat file was skipped")
	}
}
