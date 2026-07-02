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
	err := orch.Assign("t2", "f", "feat/x", "w", "rev", "", []string{"t0"})
	if err == nil || !strings.Contains(err.Error(), "unknown dep") {
		t.Fatalf("want 'unknown dep' error, got %v", err)
	}
}

func TestAssignDepCrossFeature(t *testing.T) {
	_, orch := depsRepo(t)
	if err := orch.Assign("t1", "fa", "feat/a", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	err := orch.Assign("t2", "fb", "feat/b", "w2", "rev", "", []string{"t1"})
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
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	// t2 deps[t1] — valid DAG edge.
	if err := orch.Assign("t2", "f", "feat/x", "w2", "rev", "", []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	// t3 deps[t2, t3]: the self edge closes a cycle t3 -> t3.
	err := orch.Assign("t3", "f", "feat/x", "w", "rev", "", []string{"t2", "t3"})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want 'cycle' error, got %v", err)
	}
}

// TestAssignDepDAGAccepted guards against false positives: a deep DAG chain
// (A <- B <- C, plus a diamond) must assign cleanly.
func TestAssignDepDAGAccepted(t *testing.T) {
	_, orch := depsRepo(t)
	if err := orch.Assign("a", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("b", "f", "feat/x", "w2", "rev", "", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("c", "f", "feat/x", "w", "rev", "", []string{"a", "b"}); err != nil {
		t.Fatalf("diamond DAG must be accepted, got %v", err)
	}
}

func TestJoinGateBlockedByUnacceptedDep(t *testing.T) {
	repo, orch := depsRepo(t)
	// t1: owner w, reviewer rev, no deps.
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	// t2: owner w2, deps[t1].
	if err := orch.Assign("t2", "f", "feat/x", "w2", "rev", "", []string{"t1"}); err != nil {
		t.Fatal(err)
	}

	// w2 join while t1 not accepted -> blocked.
	w2 := pact.At(repo).As("w2")
	err := w2.Join("w2", "worker")
	if err == nil || !strings.Contains(err.Error(), "blocked by") || !strings.Contains(err.Error(), "t1") {
		t.Fatalf("want join blocked-by-t1 error, got %v", err)
	}

	// Drive t1 to accepted: w joins, checkpoints; rev accepts.
	w := pact.At(repo).As("w")
	if err := w.Join("w", "worker"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x"), 0o644)
	if err := w.Checkpoint("t1", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := orch.As("rev").Accept("t1"); err != nil {
		t.Fatal(err)
	}

	// Now w2 join succeeds.
	if err := w2.Join("w2", "worker"); err != nil {
		t.Fatalf("w2 join after t1 accepted must succeed, got %v", err)
	}
}

// Regression: one seat owning a runnable task AND a future dep-blocked task must
// still be able to join (the old gate failed the whole join on the blocked task,
// so the feature branch was never created).
func TestJoinGateRunnableTaskNotStrandedByFutureDep(t *testing.T) {
	repo, orch := depsRepo(t)
	// w owns BOTH t1 (no deps, runnable) and t2 (deps[t1], blocked).
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("t2", "f", "feat/x", "w", "rev", "", []string{"t1"}); err != nil {
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
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("t2", "f", "feat/x", "w2", "rev", "", []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.ReadFile(filepath.Join(repo, ".pact/STATE.yml"))
	if err != nil {
		t.Fatal(err)
	}
	state := string(st)
	if !strings.Contains(state, "deps: [t1]") {
		t.Fatalf("STATE must contain 'deps: [t1]' under t2:\n%s", state)
	}
	// t1 has no deps -> exactly one deps line in the whole STATE.
	if n := strings.Count(state, "deps:"); n != 1 {
		t.Fatalf("expected exactly one deps line (t2 only), got %d:\n%s", n, state)
	}
}
