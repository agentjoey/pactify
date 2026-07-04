package orchestrate

import (
	"context"
	"os/exec"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// quorumRunner drives the pact engine for the quorum driver test: a worker brief
// checkpoints; a reviewer brief accepts AS the launched seat and records the seat
// in call order, so the test can assert the reviewers ran serially and that a met
// quorum stopped the sweep early (the remaining reviewer never launched).
type quorumRunner struct {
	dir         string
	workerCalls int
	reviewOrder []string // reviewer seats launched, in order
}

func (r *quorumRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	if isWorker(lc.Briefing) {
		r.workerCalls++
		return pact.At(r.dir).As(lc.Seat).Checkpoint(task, "evidence: tests pass")
	}
	r.reviewOrder = append(r.reviewOrder, lc.Seat)
	return pact.At(r.dir).As(lc.Seat).Accept(task)
}

// A quorum task runs its reviewers serially and stops as soon as the quorum is met:
// reviewers a then b accept (2 of 3) and c is never launched. The feature ships.
func TestQuorumDriverSerialEarlyStop(t *testing.T) {
	dir := newProject(t)
	// Add the three reviewer seats to the roster.
	for _, spec := range []string{"a:reviewer:A.md", "b:reviewer:B.md", "c:reviewer:C.md"} {
		if err := pact.At(dir).As("orch").AddSeat(spec); err != nil {
			t.Fatalf("add seat %s: %v", spec, err)
		}
	}
	spec := writeSpec(t, dir, "t1", "go test ./...")
	if err := pact.At(dir).As("orch").AssignQuorum("t1", "f", "feat/x", "w", []string{"a", "b", "c"}, 2, spec, nil); err != nil {
		t.Fatalf("assign quorum: %v", err)
	}
	// Check out the feature branch so worker checkpoints land where merge integrates.
	c := exec.Command("git", "checkout", "-q", "-B", "feat/x")
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("checkout: %v %s", err, out)
	}

	runner := &quorumRunner{dir: dir}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	// Reviewers ran serially in reviewer-set order, stopping at quorum: a, b only.
	want := []string{"a", "b"}
	if len(runner.reviewOrder) != len(want) {
		t.Fatalf("reviewer launches = %v, want %v (quorum met at 2 → c never runs)", runner.reviewOrder, want)
	}
	for i, s := range want {
		if runner.reviewOrder[i] != s {
			t.Fatalf("reviewer launch order = %v, want %v", runner.reviewOrder, want)
		}
	}
}
