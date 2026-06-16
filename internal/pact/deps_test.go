package pact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
)

// depsRepo bootstraps an init'd pact repo from a foreign cwd (Q1 style) and
// returns the orchestrator handle. Two workers (w, w2) plus a reviewer (rev)
// are declared so deps flows can be exercised without env mutation.
func depsRepo(t *testing.T) (repo string, orch *pact.Project) {
	t.Helper()
	repo = newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv("PACT_DIR", "")
	orch = pact.At(repo).As("orch")
	seats := []string{
		"orch:orchestrator:CLAUDE.md",
		"w:worker:A.md",
		"w2:worker:B.md",
		"rev:reviewer:R.md",
	}
	if err := orch.Init("p", seats); err != nil {
		t.Fatal(err)
	}
	return repo, orch
}

func TestAssignUnknownDep(t *testing.T) {
	_, orch := depsRepo(t)
	err := orch.Assign("T2", "F", "feat/x", "w", "rev", "", []string{"T0"})
	if err == nil || !strings.Contains(err.Error(), "unknown dep") {
		t.Fatalf("want 'unknown dep' error, got %v", err)
	}
}

func TestAssignDepCrossFeature(t *testing.T) {
	_, orch := depsRepo(t)
	if err := orch.Assign("T1", "FA", "feat/a", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	err := orch.Assign("T2", "FB", "feat/b", "w2", "rev", "", []string{"T1"})
	if err == nil || !strings.Contains(err.Error(), "same feature") {
		t.Fatalf("want 'same feature' error, got %v", err)
	}
}

// TestAssignDepCycle exercises the DFS over existing edges + the new edge.
// With unique, immutable task ids a back-edge can only be introduced as a
// self-reference or a dep that (with the new edge) makes the new node
// reachable from itself. A self-listed dep closes the smallest cycle.
func TestAssignDepCycle(t *testing.T) {
	_, orch := depsRepo(t)
	if err := orch.Assign("T1", "F", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	// T2 deps[T1] — valid DAG edge.
	if err := orch.Assign("T2", "F", "feat/x", "w2", "rev", "", []string{"T1"}); err != nil {
		t.Fatal(err)
	}
	// T3 deps[T2, T3]: the self edge closes a cycle T3 -> T3.
	err := orch.Assign("T3", "F", "feat/x", "w", "rev", "", []string{"T2", "T3"})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want 'cycle' error, got %v", err)
	}
}

// TestAssignDepDAGAccepted guards against false positives: a deep DAG chain
// (A <- B <- C, plus a diamond) must assign cleanly.
func TestAssignDepDAGAccepted(t *testing.T) {
	_, orch := depsRepo(t)
	if err := orch.Assign("A", "F", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("B", "F", "feat/x", "w2", "rev", "", []string{"A"}); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("C", "F", "feat/x", "w", "rev", "", []string{"A", "B"}); err != nil {
		t.Fatalf("diamond DAG must be accepted, got %v", err)
	}
}

func TestJoinGateBlockedByUnacceptedDep(t *testing.T) {
	repo, orch := depsRepo(t)
	// T1: owner w, reviewer rev, no deps.
	if err := orch.Assign("T1", "F", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	// T2: owner w2, deps[T1].
	if err := orch.Assign("T2", "F", "feat/x", "w2", "rev", "", []string{"T1"}); err != nil {
		t.Fatal(err)
	}

	// w2 join while T1 not accepted -> blocked.
	w2 := pact.At(repo).As("w2")
	err := w2.Join("w2", "worker")
	if err == nil || !strings.Contains(err.Error(), "blocked by") || !strings.Contains(err.Error(), "T1") {
		t.Fatalf("want join blocked-by-T1 error, got %v", err)
	}

	// Drive T1 to accepted: w joins, checkpoints; rev accepts.
	w := pact.At(repo).As("w")
	if err := w.Join("w", "worker"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x"), 0o644)
	if err := w.Checkpoint("T1", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := orch.As("rev").Accept("T1"); err != nil {
		t.Fatal(err)
	}

	// Now w2 join succeeds.
	if err := w2.Join("w2", "worker"); err != nil {
		t.Fatalf("w2 join after T1 accepted must succeed, got %v", err)
	}
}

// Regression: one seat owning a runnable task AND a future dep-blocked task must
// still be able to join (the old gate failed the whole join on the blocked task,
// so the feature branch was never created).
func TestJoinGateRunnableTaskNotStrandedByFutureDep(t *testing.T) {
	repo, orch := depsRepo(t)
	// w owns BOTH T1 (no deps, runnable) and T2 (deps[T1], blocked).
	if err := orch.Assign("T1", "F", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("T2", "F", "feat/x", "w", "rev", "", []string{"T1"}); err != nil {
		t.Fatal(err)
	}
	w := pact.At(repo).As("w")
	if err := w.Join("w", "worker"); err != nil {
		t.Fatalf("join must succeed when a runnable task exists, got %v", err)
	}
	if cur, _ := gitx.CurrentBranch(repo); cur != "feat/x" {
		t.Fatalf("feature branch should be checked out after join, on %q", cur)
	}
}

func TestProjectionDepsLine(t *testing.T) {
	repo, orch := depsRepo(t)
	if err := orch.Assign("T1", "F", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("T2", "F", "feat/x", "w2", "rev", "", []string{"T1"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.ReadFile(filepath.Join(repo, ".pact/STATE.yml"))
	if err != nil {
		t.Fatal(err)
	}
	state := string(st)
	if !strings.Contains(state, "deps: [T1]") {
		t.Fatalf("STATE must contain 'deps: [T1]' under T2:\n%s", state)
	}
	// T1 has no deps -> exactly one deps line in the whole STATE.
	if n := strings.Count(state, "deps:"); n != 1 {
		t.Fatalf("expected exactly one deps line (T2 only), got %d:\n%s", n, state)
	}
}
