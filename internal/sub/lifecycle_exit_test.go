package sub

import (
	"context"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/agentjoey/pactify/internal/procx"
)

// A harness that exits before it is registered must not fail the job with
// "register harness process"; its own exit is reported by the run.
func TestStartProcessToleratesHarnessThatAlreadyExited(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	if _, err := store.Create("j1", Request{Harness: "agy", Cwd: t.TempDir(), Access: "write"}); err != nil {
		t.Fatal(err)
	}
	old := inspectHarness
	inspectHarness = func(int) (procx.Identity, error) { return procx.Identity{}, syscall.ESRCH }
	t.Cleanup(func() { inspectHarness = old })

	cmd := exec.Command("true")
	if err := store.StartProcess(context.Background(), "j1", cmd); err != nil {
		t.Fatalf("StartProcess = %v, want nil for an already-exited harness", err)
	}
	_ = cmd.Wait()
	st, err := store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Processes) != 0 {
		t.Fatalf("registered %v for an exited harness", st.Processes)
	}
}
