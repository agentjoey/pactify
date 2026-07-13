package planner_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/planner"
)

// Security regression — review finding H5 (high, data-integrity).
//
// ApplyTx captures origSize = stat(log).Size() once, then on a later assign
// failure "rolls back" with os.Truncate(log, origSize) — OUTSIDE any ledger lock,
// to a byte offset captured before the lock ever existed. A concurrent writer that
// appends between the capture and the truncate has its committed event silently
// deleted (the "pact state behind git" corruption). The fix holds withLedgerLock
// across the whole apply and rolls back by event_id, so a concurrent write is
// serialized after the rollback and never lost.
//
// This drives a real race: the plan's first task assigns successfully (holding the
// ledger lock, so the concurrent writer queues behind it and appends before the
// truncate), and its second task collides with a pre-existing id (forcing the
// failure → truncate). The concurrently-appended event MUST survive.
//
// RED until ApplyTx serializes under withLedgerLock and rolls back by event_id.
func TestSEC_H5_ApplyTxRollbackPreservesConcurrentWrite(t *testing.T) {
	const foreignTask = "foreign-survivor"

	for iter := 0; iter < 12; iter++ {
		dir := newGitRepo(t)
		t.Setenv("PACT_DIR", "")
		t.Setenv("PACT_AGENT_ID", "claude")
		if err := pact.At(dir).As("claude").Init("p", []string{
			"claude:orchestrator,reviewer:CLAUDE.md",
			"alice:worker:A.md",
			"bob:worker:B.md",
		}); err != nil {
			t.Fatal(err)
		}
		tasksDir := filepath.Join(dir, ".pact", "tasks")
		os.MkdirAll(tasksDir, 0o755)
		for _, id := range []string{"tgood", "collide"} {
			os.WriteFile(filepath.Join(tasksDir, id+".md"), []byte("# "+id), 0o644)
		}
		// Pre-assign "collide" so ApplyTx's collide assign fails after tgood succeeds.
		if err := pact.At(dir).As("claude").Assign("collide", "f0", "feat-f0", "alice", "bob", ".pact/tasks/collide.md", nil); err != nil {
			t.Fatal(err)
		}

		plan := planner.Plan{Feature: "f1", Branch: "feat-f1", Tasks: []planner.PlanTask{
			{ID: "tgood", Owner: "alice", Reviewer: "bob", Spec: ".pact/tasks/tgood.md", Verify: "go test"},
			{ID: "collide", Owner: "alice", Reviewer: "bob", Spec: ".pact/tasks/collide.md", Verify: "go test"},
		}}
		roster := []string{"alice", "bob", "claude"}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _ = planner.ApplyTx(dir, plan, roster, "claude") // fails on collide → rollback
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = pact.At(dir).As("claude").Assign(foreignTask, "f2", "feat-f2", "alice", "bob", "", nil)
		}()
		close(start)
		wg.Wait()

		logBytes, _ := os.ReadFile(filepath.Join(dir, ".pact", "log.jsonl"))
		if !strings.Contains(string(logBytes), foreignTask) {
			t.Fatalf("iter %d: H5 — ApplyTx rollback truncated a concurrent writer's committed event (%q lost)", iter, foreignTask)
		}
	}
}
