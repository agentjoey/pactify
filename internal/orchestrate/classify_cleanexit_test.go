package orchestrate

import (
	"context"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// silentWorkerRunner models the opencode delivery class: the worker exits
// cleanly WITHOUT checkpointing (its delivery never reached the driver's
// ledger). The reviewer still accepts normally so a rescued run can ship.
type silentWorkerRunner struct {
	t           *testing.T
	dir         string
	workerCalls int
}

func (f *silentWorkerRunner) Run(ctx context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	if task == "" {
		f.t.Fatalf("could not parse task id from brief:\n%s", lc.Briefing)
	}
	if isWorker(lc.Briefing) {
		f.workerCalls++
		return nil
	}
	return pact.At(f.dir).As(lc.Seat).Accept(task)
}

// A worker that exits cleanly without checkpointing but whose verify command
// passes gets the same rescue as the crash path: the driver checkpoints on its
// behalf and the run proceeds to review/ship instead of burning failures
// toward escalation.
func TestCleanExitNoCheckpointRescuedByVerify(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "T1", "true")
	assign(t, dir, "T1", "F", "feat/x", spec)

	runner := &silentWorkerRunner{t: t, dir: dir}
	notify := &recNotify{}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, notify)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "F"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped (rescue should checkpoint); notify=%v", got, notify.msgs)
	}
	if runner.workerCalls != 1 {
		t.Fatalf("worker launched %d times, want 1 (rescued, not retried)", runner.workerCalls)
	}
}

// When the verify command fails too, the clean-exit-no-checkpoint path is a
// genuine failure: it keeps burning fails and escalates with the named cause.
func TestCleanExitNoCheckpointStillEscalatesWhenVerifyFails(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "T1", "false")
	assign(t, dir, "T1", "F", "feat/x", spec)

	notify := &recNotify{}
	if err := Run(context.Background(), baseOpts(dir, &silentWorkerRunner{t: t, dir: dir}, &failExec{}, notify)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	joined := strings.Join(notify.msgs, "\n")
	if !strings.Contains(joined, "recorded no checkpoint") {
		t.Fatalf("escalation should name the no-checkpoint cause; notify=%v", notify.msgs)
	}
	if got := featureStatus(t, dir, "F"); got == "shipped" {
		t.Fatal("feature shipped despite failing verify; want escalation")
	}
}
