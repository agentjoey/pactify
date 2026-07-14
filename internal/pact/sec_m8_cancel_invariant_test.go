package pact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// Security regression — review finding M8.
//
// `cancel` removes a task from the projection. checkMerge only requires the tasks
// that REMAIN to be accepted, so cancelling a checkpointed-but-unaccepted task
// would let the feature merge while that task's unreviewed commits are still on
// the branch — shipping unreviewed code and bypassing invariant (2). cancel must
// refuse a task past checkpoint.

// m8Status reads a task's status through the public projection API (this is an
// external pact_test file, so the in-package statusOf helper is not visible).
func m8Status(t *testing.T, repo, task string) string {
	t.Helper()
	st, err := pact.At(repo).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			if tk.ID == task {
				return tk.Status
			}
		}
	}
	t.Fatalf("task %s not found in projection", task)
	return ""
}

// checkpointTask assigns t to owner w (reviewer rev) and drives w to checkpoint,
// leaving it awaiting_review with a commit on feat/x.
func checkpointTask(t *testing.T, repo string, orch *pact.Project, task string) {
	t.Helper()
	if err := orch.Assign(task, "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatalf("assign %s: %v", task, err)
	}
	w := pact.At(repo).As("w")
	if err := w.Join("w", "worker"); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("work"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Checkpoint(task, "evidence: done"); err != nil {
		t.Fatalf("checkpoint %s: %v", task, err)
	}
}

func TestSEC_M8_CancelRefusedForAwaitingReview(t *testing.T) {
	repo, orch := depsRepo(t)
	checkpointTask(t, repo, orch, "t1") // awaiting_review, unreviewed commit on feat/x

	err := orch.Cancel("t1")
	if err == nil {
		t.Fatal("cancel of an awaiting_review task must be refused (M8: would ship unreviewed commits)")
	}
	if !strings.Contains(err.Error(), "unreviewed") {
		t.Fatalf("cancel error should explain the unreviewed-commits hazard, got: %v", err)
	}

	// The task is untouched — not silently dropped.
	if got := m8Status(t, repo, "t1"); got != "awaiting_review" {
		t.Fatalf("t1 status = %q after refused cancel, want awaiting_review", got)
	}
	// The security property: the merge gate still holds — the unreviewed task
	// cannot be cancelled away to unblock the ship.
	if err := orch.Merge("f"); err == nil {
		t.Fatal("merge succeeded with an unaccepted task still present — invariant (2) breached")
	}
}

func TestSEC_M8_CancelRefusedForChangesRequested(t *testing.T) {
	repo, orch := depsRepo(t)
	checkpointTask(t, repo, orch, "t1")

	if err := pact.At(repo).As("rev").Changes("t1", "please fix the edge case"); err != nil {
		t.Fatalf("changes: %v", err)
	}
	if got := m8Status(t, repo, "t1"); got != "changes_requested" {
		t.Fatalf("t1 status = %q, want changes_requested", got)
	}

	// The rejected checkpoint's commit is still on the branch; cancelling would
	// merge it unreviewed. Refuse.
	err := orch.Cancel("t1")
	if err == nil {
		t.Fatal("cancel of a changes_requested task must be refused (M8)")
	}
	if !strings.Contains(err.Error(), "unreviewed") {
		t.Fatalf("cancel error should explain the unreviewed-commits hazard, got: %v", err)
	}
}

// A task with NO commits (assigned / in_progress) is still cancellable — the fix
// must not regress the normal "drop a task I no longer need" path.
func TestSEC_M8_CancelAllowedBeforeCheckpoint(t *testing.T) {
	repo, orch := depsRepo(t)
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	// Take it to in_progress (worker joined, nothing checkpointed → no commits).
	if err := pact.At(repo).As("w").Join("w", "worker"); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := orch.Cancel("t1"); err != nil {
		t.Fatalf("cancel of a pre-checkpoint task must still succeed, got: %v", err)
	}
}
