package orchestrate

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
)

// countRunner records launches; a fail-fast run must never reach it.
type countRunner struct{ n int }

func (c *countRunner) Run(context.Context, LaunchContext) error {
	c.n++
	return nil
}

// 2026-07-19 orchestrate e2e F4: `--as driver` with a seat that was never
// init'd ran EVERY stint and only died at the final merge ("acting seat must
// have the orchestrator role") — burning a full run's agent time on an error
// knowable at startup. The 2026-06-13 #4 fail-fast only covered an EMPTY
// identity; an unknown or under-privileged seat must now fail before any
// launch.

func TestRunFailsFastWhenDriverSeatUnknown(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	r := &countRunner{}
	opts := baseOpts(dir, r, &okExec{}, StdoutNotifier{})
	opts.Orchestrator = "driver"
	err := Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "driver") || !strings.Contains(err.Error(), "roster") {
		t.Fatalf("want startup roster fail-fast naming the seat, got %v", err)
	}
	if r.n != 0 {
		t.Fatalf("fail-fast must precede any agent launch, got %d launches", r.n)
	}
}

func TestRunFailsFastWhenDriverSeatLacksOrchestratorRole(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	r := &countRunner{}
	opts := baseOpts(dir, r, &okExec{}, StdoutNotifier{})
	opts.Orchestrator = "w" // exists, but roles: worker only
	err := Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "orchestrator role") {
		t.Fatalf("want orchestrator-role fail-fast, got %v", err)
	}
	if r.n != 0 {
		t.Fatalf("fail-fast must precede any agent launch, got %d launches", r.n)
	}
}

// The sandbox path must refuse BEFORE touching the tree: no park branch, no
// worktree — a run that parks first would leave recovery litter for a purely
// preflight error.
func TestRunSandboxFailsFastBeforeParking(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")
	before := branchOf(t, dir)
	o := sandboxOpts(t, dir)
	o.Orchestrator = "driver"
	if err := RunSandbox(context.Background(), o); err == nil || !strings.Contains(err.Error(), "roster") {
		t.Fatalf("want roster fail-fast from RunSandbox, got %v", err)
	}
	if gitx.BranchExists(dir, parkBranch) {
		t.Fatal("preflight failure must not create the park branch")
	}
	if after := branchOf(t, dir); after != before {
		t.Fatalf("preflight failure must leave the tree on %q, got %q", before, after)
	}
}
