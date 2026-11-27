package procx

import (
	"os/exec"
	"testing"
	"time"
)

// SIGKILL cannot be ignored, but a busy machine may not schedule the target
// within the caller's short grace. A short grace must not turn that delay into a
// "still alive after SIGKILL" error.
func TestTerminateDescendantsWaitsForSIGKILLBeyondShortGrace(t *testing.T) {
	for i := 0; i < 20; i++ {
		cmd := exec.Command("/bin/sh", "-c", `trap "" TERM; while :; do :; done`)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		id, err := Inspect(cmd.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		if err := TerminateDescendants("test", []Identity{id}, time.Microsecond); err != nil {
			_ = cmd.Process.Kill()
			t.Fatalf("run %d: %v", i, err)
		}
		<-done
	}
}
