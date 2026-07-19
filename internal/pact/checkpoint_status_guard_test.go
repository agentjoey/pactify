package pact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// 2026-07-19 orchestrate e2e F3 root cause: checkCheckpoint validated owner and
// deps but never STATUS, so an owner could checkpoint its own ACCEPTED task and
// — because the projection unconditionally folds checkpoint → awaiting_review —
// unilaterally rewind a reviewer's verdict (observed as a double accept in the
// ledger). A reviewer decision must only be reopened by the reviewer (changes)
// or the orchestrator (cancel/new task), never by the owner.
func TestCheckpointRefusedOnAcceptedTask(t *testing.T) {
	repo, orch := acceptReadyRepo(t) // t1 driven to awaiting_review by owner w
	if err := orch.As("rev").Accept("t1"); err != nil {
		t.Fatal(err)
	}
	w := pact.At(repo).As("w")
	os.WriteFile(filepath.Join(repo, "more.txt"), []byte("y"), 0o644)
	err := w.Checkpoint("t1", "verify passed on recovery")
	if err == nil || !strings.Contains(err.Error(), "accepted") {
		t.Fatalf("checkpoint of an accepted task must be refused naming the status, got %v", err)
	}
	// The refusal must leave the verdict intact: still exactly one accept event.
	log, rerr := os.ReadFile(filepath.Join(repo, ".pact/log.jsonl"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if n := strings.Count(string(log), `"accept"`); n != 1 {
		t.Fatalf("ledger must keep exactly one accept, got %d", n)
	}
}

// Guard the guard: a SECOND checkpoint while awaiting_review is the
// fix-until-green loop's bread and butter (checkpoint → verify red → fix →
// checkpoint again, before any review). The new status guard must not break it.
func TestCheckpointAllowedWhileAwaitingReview(t *testing.T) {
	repo, _ := acceptReadyRepo(t)
	w := pact.At(repo).As("w")
	os.WriteFile(filepath.Join(repo, "fix.txt"), []byte("z"), 0o644)
	if err := w.Checkpoint("t1", "fixed the red gate"); err != nil {
		t.Fatalf("re-checkpoint while awaiting_review must stay allowed (fix-until-green), got %v", err)
	}
}
