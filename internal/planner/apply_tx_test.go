package planner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/planner"
)

func seedPlan(t *testing.T, n int) (dir string, plan planner.Plan, roster []string) {
	t.Helper()
	dir = newGitRepo(t)
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "claude")

	if err := pact.At(dir).As("claude").Init("testp", []string{
		"claude:orchestrator,reviewer:CLAUDE.md",
		"alice:worker:A.md",
		"bob:worker:B.md",
	}); err != nil {
		t.Fatal(err)
	}

	os.MkdirAll(filepath.Join(dir, ".pact", "tasks"), 0o755)
	tasks := make([]planner.PlanTask, n)
	for i := 0; i < n; i++ {
		id := "t" + string(rune('1'+i))
		specRel := ".pact/tasks/" + id + ".md"
		os.WriteFile(filepath.Join(dir, specRel), []byte("# "+id), 0o644)
		owner, reviewer := "alice", "bob"
		if i%2 == 1 {
			owner, reviewer = "bob", "alice"
		}
		var deps []string
		if i > 0 {
			deps = []string{"t" + string(rune('1'+i-1))}
		}
		tasks[i] = planner.PlanTask{
			ID: id, Owner: owner, Reviewer: reviewer,
			Spec: specRel, Verify: "go test", Deps: deps,
		}
	}
	plan = planner.Plan{Feature: "demo-feature", Branch: "feat-demo", Tasks: tasks}
	roster = []string{"alice", "bob", "claude"}
	return
}

func TestApplyTx_AllOrNothing_Success(t *testing.T) {
	dir, plan, roster := seedPlan(t, 3)
	n, err := planner.ApplyTx(dir, plan, roster, "claude")
	if err != nil || n != 3 {
		t.Fatalf("ApplyTx=(%d,%v), want (3,nil)", n, err)
	}
}

func TestApplyTx_RejectsAtomically(t *testing.T) {
	dir, plan, roster := seedPlan(t, 3)
	plan.Tasks[2].Deps = []string{"nonexistent"}
	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	before, _ := os.ReadFile(logPath)
	if _, err := planner.ApplyTx(dir, plan, roster, "claude"); err == nil {
		t.Fatal("invalid plan should fail")
	}
	after, _ := os.ReadFile(logPath)
	if len(after) != len(before) {
		t.Fatalf("log.jsonl should not grow: before=%d after=%d", len(before), len(after))
	}
}
