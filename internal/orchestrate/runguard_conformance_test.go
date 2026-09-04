package orchestrate

import (
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/runguard"
)

// The checkpoint guard reads the runtime status files this package writes, and
// the two compute their paths independently (both hardcode `.pact/orchestrate`
// rather than going through paths.DirIn — the runtime dir deliberately does not
// follow a PACT_DIR rename). Nothing but this test holds them together: move
// the writer and the guard silently stops guarding, because "no status file"
// reads as "no run in flight".
//
// So drive the REAL writers and assert the REAL reader sees them.
func TestSerialStatusIsVisibleToRunGuard(t *testing.T) {
	dir := t.TempDir()

	if err := writeStatus(dir, Status{
		Feature:   "m4",
		Task:      "m4-s11",
		Seat:      "kimi-worker",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}

	run, ok := runguard.Serial(dir)
	if !ok {
		t.Fatal("runguard cannot see the status.json this package just wrote")
	}
	if run.Task != "m4-s11" || run.Seat != "kimi-worker" {
		t.Errorf("run = %+v, want task m4-s11 / seat kimi-worker", run)
	}
	if blocking := runguard.BlocksCheckpoint(dir, "other-task"); len(blocking) != 1 {
		t.Errorf("a live serial run must block a manual checkpoint of another task, got %+v", blocking)
	}
}

func TestParallelFeatureStatusIsVisibleToRunGuard(t *testing.T) {
	dir := t.TempDir()

	if err := writeFeatureStatus(dir, "m4", Status{
		Feature:   "m4",
		Task:      "m4-s11",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("writeFeatureStatus: %v", err)
	}

	runs := runguard.Live(dir)
	if len(runs) != 1 || runs[0].Task != "m4-s11" {
		t.Fatalf("runguard cannot see the parallel status this package just wrote: %+v", runs)
	}
}

// The status the driver stamps must stay inside the guard's freshness window,
// or a live run reads as dead. Heartbeat interval < StaleAfter is the contract.
func TestHeartbeatOutpacesTheGuardStaleWindow(t *testing.T) {
	if statusHeartbeatInterval >= runguard.StaleAfter {
		t.Fatalf("heartbeat %v must stay well under the guard's stale window %v, or a live run reads as dead",
			statusHeartbeatInterval, runguard.StaleAfter)
	}
}
