package pact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
)

// join --task (2026-07-19 cross-vendor dogfood follow-up): a briefing names a
// specific task, but seat-scoped join lifts "the first workable owned task" —
// which may be a different one. A targeted join removes that mismatch: it
// lifts exactly the named task, refuses with an error naming THAT task when
// its deps are unmet, and checks out that task's feature branch.

func TestJoinTaskLiftsExactlyTargetTask(t *testing.T) {
	repo, orch := depsRepo(t)
	// Two independently runnable tasks owned by w; untargeted join would lift t1.
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("t2", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	w := pact.At(repo).As("w")
	if err := w.JoinTask("w", "worker", "t2"); err != nil {
		t.Fatalf("targeted join must succeed, got %v", err)
	}
	st, err := os.ReadFile(filepath.Join(repo, ".pact/STATE.yml"))
	if err != nil {
		t.Fatal(err)
	}
	state := string(st)
	if !strings.Contains(state, "id: t2\n        owner: w\n        status: in_progress") {
		t.Fatalf("t2 must be in_progress after targeted join:\n%s", state)
	}
	if !strings.Contains(state, "id: t1\n        owner: w\n        status: assigned") {
		t.Fatalf("t1 must stay assigned (targeted join must not lift it):\n%s", state)
	}
}

func TestJoinTaskBlockedDepErrorNamesTarget(t *testing.T) {
	repo, orch := depsRepo(t)
	if err := orch.Assign("t1", "f", "feat/x", "w2", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("t2", "f", "feat/x", "w", "rev", "", []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	w := pact.At(repo).As("w")
	err := w.JoinTask("w", "worker", "t2")
	if err == nil || !strings.Contains(err.Error(), "t2") || !strings.Contains(err.Error(), "t1") {
		t.Fatalf("targeted join of a dep-blocked task must fail naming the TARGET and its dep, got %v", err)
	}
}

func TestJoinTaskNotOwnedRefused(t *testing.T) {
	repo, orch := depsRepo(t)
	if err := orch.Assign("t1", "f", "feat/x", "w2", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	w := pact.At(repo).As("w")
	err := w.JoinTask("w", "worker", "t1")
	if err == nil || !strings.Contains(err.Error(), "owned by") {
		t.Fatalf("targeted join of a task owned by another seat must be refused, got %v", err)
	}
}

func TestJoinTaskUnknownRefused(t *testing.T) {
	repo, _ := depsRepo(t)
	w := pact.At(repo).As("w")
	err := w.JoinTask("w", "worker", "nope")
	if err == nil || !strings.Contains(err.Error(), "unknown task") {
		t.Fatalf("targeted join of an unknown task must be refused, got %v", err)
	}
}

func TestJoinTaskChecksOutTargetFeatureBranch(t *testing.T) {
	repo, orch := depsRepo(t)
	// w owns tasks in two features; untargeted join would check out the first.
	if err := orch.Assign("t1", "fa", "feat/a", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("t2", "fb", "feat/b", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	w := pact.At(repo).As("w")
	if err := w.JoinTask("w", "worker", "t2"); err != nil {
		t.Fatal(err)
	}
	if cur, _ := gitx.CurrentBranch(repo); cur != "feat/b" {
		t.Fatalf("targeted join must check out the TARGET task's feature branch, on %q", cur)
	}
}

// A targeted join may resume a task that is already in_progress (cold-start
// after a crash): no state change, no error.
func TestJoinTaskResumeInProgress(t *testing.T) {
	repo, orch := depsRepo(t)
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	w := pact.At(repo).As("w")
	if err := w.JoinTask("w", "worker", "t1"); err != nil {
		t.Fatal(err)
	}
	if err := w.JoinTask("w", "worker", "t1"); err != nil {
		t.Fatalf("targeted re-join of an in_progress task must succeed (resume), got %v", err)
	}
}
