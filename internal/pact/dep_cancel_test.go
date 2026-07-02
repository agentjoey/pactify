package pact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// A cancelled dep can never reach `accepted`; the gates must treat it as
// vacuously satisfied instead of blocking the dependent forever. An existing
// (uncancelled) unaccepted dep must keep blocking.
func TestCancelledDepUnblocksJoinAndCheckpoint(t *testing.T) {
	repo, orch := depsRepo(t)
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("t2", "f", "feat/x", "w2", "rev", "", []string{"t1"}); err != nil {
		t.Fatal(err)
	}

	// The existing unaccepted dep still blocks the dependent's join.
	w2 := pact.At(repo).As("w2")
	err := w2.Join("w2", "worker")
	if err == nil || !strings.Contains(err.Error(), "blocked by") {
		t.Fatalf("want join blocked by unaccepted t1, got %v", err)
	}

	if err := orch.Cancel("t1"); err != nil {
		t.Fatal(err)
	}

	// t1 gone from the projection -> t2 is runnable: join and checkpoint pass.
	if err := w2.Join("w2", "worker"); err != nil {
		t.Fatalf("join after dep cancel must succeed, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w2.Checkpoint("t2", "done"); err != nil {
		t.Fatalf("checkpoint after dep cancel must succeed, got %v", err)
	}
}

// The checkpoint dep gate exists for the join-before-assign ordering hole; it
// too must block on a live unaccepted dep and open once that dep is cancelled.
func TestCheckpointDepGateBlocksThenOpensOnCancel(t *testing.T) {
	repo, orch := depsRepo(t)
	// w2 joins BEFORE any dep'd assign (the gate-ordering hole).
	w2 := pact.At(repo).As("w2")
	if err := w2.Join("w2", "worker"); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("t2", "f", "feat/x", "w2", "rev", "", []string{"t1"}); err != nil {
		t.Fatal(err)
	}

	err := w2.Checkpoint("t2", "too early")
	if err == nil || !strings.Contains(err.Error(), "blocked by unaccepted dep t1") {
		t.Fatalf("want checkpoint blocked by t1, got %v", err)
	}

	if err := orch.Cancel("t1"); err != nil {
		t.Fatal(err)
	}
	// Re-join to land on the feature branch (the seat joined before the assign,
	// so it is still on base and the base-write guard would refuse).
	if err := w2.Join("w2", "worker"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w2.Checkpoint("t2", "done"); err != nil {
		t.Fatalf("checkpoint after dep cancel must succeed, got %v", err)
	}
}
