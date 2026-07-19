package orchestrate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
)

// 2026-07-19 orchestrate e2e F1 (deterministic, reproduced 3×): a finished
// sandbox run left the pact-sandbox-park branch behind, and the NEXT run's
// CheckoutOrCreate REUSED it — rewinding the main tree (with its tracked
// .pact) to the previous run's state, so syncPact seeded a stale ledger and
// the new feature "was not found in sandbox". A second run after new work is
// committed must ship that work.
func TestRunSandbox_SecondRunNotPoisonedByStalePark(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")
	if err := RunSandbox(context.Background(), sandboxOpts(t, dir)); err != nil {
		t.Fatalf("run 1: %v", err)
	}

	// New work lands on main AFTER run 1 — exactly what a stale park erases.
	writeSpec(t, dir, "tb", "true")
	assignNoCheckout(t, dir, "tb", "fb", "feat-b", filepath.Join(".pact", "tasks", "tb.md"))
	gitCommitAll(t, dir, "assign fb")

	o := sandboxOpts(t, dir)
	o.Feature = "fb"
	if err := RunSandbox(context.Background(), o); err != nil {
		t.Fatalf("run 2 must not be poisoned by run 1's park branch: %v", err)
	}
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	shipped := false
	for _, f := range st.Features {
		if f.ID == "fb" && f.Status == "shipped" {
			shipped = true
		}
	}
	if !shipped {
		t.Fatalf("fb not shipped after second sandbox run: %+v", st.Features)
	}
}

// Park hygiene (e2e F9 same family): after a run restores the user's branch the
// park branch is pure garbage — a stale pointer that only exists to poison the
// next run. A successful run must not leave it behind.
func TestRunSandbox_RemovesParkBranchOnTeardown(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")
	if err := RunSandbox(context.Background(), sandboxOpts(t, dir)); err != nil {
		t.Fatalf("RunSandbox: %v", err)
	}
	if gitx.BranchExists(dir, parkBranch) {
		t.Fatalf("park branch %q must be deleted after a successful run", parkBranch)
	}
}
