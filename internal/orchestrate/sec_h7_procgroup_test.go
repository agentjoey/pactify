package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Security regression — review finding H7 (high).
//
// The default cmd transport (osExecIdle) starts the child in its own process
// group (Setpgid) but never sets cmd.Cancel. On ctx cancellation — which is what
// RunTimeout expiry and a Ctrl-C do — exec.CommandContext's default cancel sends
// SIGKILL to the single group LEADER only; grandchildren (MCP servers, npx node
// children) orphan and keep running, holding a vendor session and burning tokens.
// The idle watchdog reaps the group (killGroup) but the far more common cancel
// path does not. The ACP transport already fixed this with cmd.Cancel = killGroup
// (acp.go); the cmd transport must too.
//
// RED until osExecIdle sets cmd.Cancel = killGroup.
func TestSEC_H7_CmdTransportReapsProcessGroupOnCancel(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "grandchild-was-alive")

	ctx, cancel := context.WithCancel(context.Background())
	// Long idle so the idle watchdog never fires — we test the ctx-cancel path.
	fn := osExecIdle(30 * time.Second)

	// The child backgrounds a grandchild that touches the marker after 0.5s, then
	// the child itself blocks. A leader-only kill leaves the reparented grandchild
	// alive → it touches the marker. A process-group kill reaps both → no marker.
	// The leader `exec sleep`s (so no lingering shell); the backgrounded grandchild
	// touches the marker after 0.3s. On the buggy leader-only kill the grandchild
	// briefly outlives the leader and touches the marker (then exits, closing the
	// pipe so cmd.Wait returns promptly). A group kill reaps it first.
	script := "( sleep 0.3; touch '" + marker + "' ) & exec sleep 3"
	done := make(chan error, 1)
	go func() { done <- fn(ctx, "sh", []string{"-c", script}, t.TempDir(), nil, nil) }()

	time.Sleep(150 * time.Millisecond) // let the child + grandchild start
	cancel()                           // simulate RunTimeout / Ctrl-C
	<-done                             // osExecIdle returns once the child is killed

	// Wait past the grandchild's 0.3s timer. Reaped with the group → no marker;
	// orphaned → marker present.
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("H7: grandchild survived ctx-cancel — process group not reaped (orphaned agent leak)")
	}
}
