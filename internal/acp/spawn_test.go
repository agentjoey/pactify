package acp

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ACP-1 regression: Close must reap the WHOLE process tree, not just the direct
// child. Every npx-launched bridge forks a node grandchild; before the
// process-group kill, Close left it running as an orphaned live agent. The
// fixture mirrors that shape: sh (direct child) forks sleep (grandchild) and
// parks in wait — a plain Process.Kill on sh strands the sleep.
func TestSpawnCloseKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	c, err := Spawn(context.Background(), "/bin/sh",
		[]string{"-c", `sleep 300 & echo $! > "$PIDFILE"; wait`},
		[]string{"PIDFILE=" + pidFile}, t.TempDir())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Wait for the grandchild pid to land.
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, rerr := os.ReadFile(pidFile); rerr == nil {
			if p, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil && p > 0 {
				pid = p
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("grandchild pid never appeared")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Sanity: grandchild is alive before Close.
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("precondition: grandchild %d should be alive, got %v", pid, err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The whole group must die — poll until signal 0 reports the pid gone.
	deadline = time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			return // ESRCH: grandchild reaped — group kill worked
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL) // don't leak past the test
			t.Fatalf("grandchild %d still alive after Close — process group not killed (orphaned agent)", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
