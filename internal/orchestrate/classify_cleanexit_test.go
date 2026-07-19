package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// silentWorkerRunner models the opencode delivery class: the worker exits
// cleanly WITHOUT checkpointing (its delivery never reached the driver's
// ledger). With deliver=true it leaves real work in the tree — the shape the
// rescue exists for; deliver=false models a worker that produced NOTHING
// (killed during setup, or a no-op run), which must never be rescued. The
// reviewer still accepts normally so a rescued run can ship.
type silentWorkerRunner struct {
	t           *testing.T
	dir         string
	deliver     bool
	workerCalls int
}

func (f *silentWorkerRunner) Run(ctx context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	if task == "" {
		f.t.Fatalf("could not parse task id from brief:\n%s", lc.Briefing)
	}
	if isWorker(lc.Briefing) {
		f.workerCalls++
		if f.deliver {
			if err := os.WriteFile(filepath.Join(f.dir, "impl.txt"), []byte("work"), 0o644); err != nil {
				f.t.Fatal(err)
			}
		}
		return nil
	}
	return pact.At(f.dir).As(lc.Seat).Accept(task)
}

// A worker that exits cleanly without checkpointing — but whose WORK is in the
// tree and whose verify command passes — gets the same rescue as the crash
// path: the driver checkpoints on its behalf and the run proceeds to
// review/ship instead of burning failures toward escalation.
func TestCleanExitNoCheckpointRescuedByVerify(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat/x", spec)

	runner := &silentWorkerRunner{t: t, dir: dir, deliver: true}
	notify := &recNotify{}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, notify)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
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
	spec := writeSpec(t, dir, "t1", "false")
	assign(t, dir, "t1", "f", "feat/x", spec)

	notify := &recNotify{}
	if err := Run(context.Background(), baseOpts(dir, &silentWorkerRunner{t: t, dir: dir}, &failExec{}, notify)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	joined := strings.Join(notify.msgs, "\n")
	if !strings.Contains(joined, "recorded no checkpoint") {
		t.Fatalf("escalation should name the no-checkpoint cause; notify=%v", notify.msgs)
	}
	if got := featureStatus(t, dir, "f"); got == "shipped" {
		t.Fatal("feature shipped despite failing verify; want escalation")
	}
}

// 2026-07-19 Phase C rerun F2-b: a worker killed mid-`sleep 120` produced
// NOTHING, yet the rescue ran `verify: true` (trivially green), checkpointed a
// phantom delivery AND cleared the fail budget — with a weak gate the timeout
// loop could burn forever without ever tripping escalation. A rescue must
// require actual owner delivery (dirty tree or commits ahead of base); with
// zero delivery the failure is counted and the run escalates naming the cause.
func TestCleanExitZeroDeliveryNotRescuedEvenWithTrivialVerify(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true") // trivially-green gate
	assign(t, dir, "t1", "f", "feat/x", spec)

	runner := &silentWorkerRunner{t: t, dir: dir, deliver: false}
	notify := &recNotify{}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, notify)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got == "shipped" {
		t.Fatal("zero-delivery worker must not be rescued into shipped by a trivially-green verify")
	}
	joined := strings.Join(notify.msgs, "\n")
	if !strings.Contains(joined, "orchestrate paused") {
		t.Fatalf("zero-delivery loop must escalate, notify=%v", notify.msgs)
	}
	if runner.workerCalls < 2 {
		t.Fatalf("failures must be counted toward the limit (retries), got %d worker calls", runner.workerCalls)
	}
}
