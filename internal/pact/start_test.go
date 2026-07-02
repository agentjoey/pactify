package pact

import (
	"strings"
	"testing"
)

// Start records the driver's task-scoped "owner launched" fact: the task flips
// assigned → in_progress, and only an assigned task may start (a task that has
// progressed past assigned keeps its stronger status).
func TestStartFlipsAssignedTaskToInProgress(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)

	p := At(repo)
	if err := p.Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Assign("t1", "f", "feat/x", "w", "orch", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}

	if err := p.Start("t1"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st, err := p.StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Features[0].Tasks[0].Status; got != "in_progress" {
		t.Fatalf("task = %q, want in_progress", got)
	}

	// A second start is refused (task is no longer assigned) — the loop's
	// retry path must not stack duplicate starts.
	if err := p.Start("t1"); err == nil || !strings.Contains(err.Error(), "not assigned") {
		t.Fatalf("re-start should be refused with a not-assigned error, got: %v", err)
	}
	// And an unknown task is an explicit error.
	if err := p.Start("nope"); err == nil || !strings.Contains(err.Error(), "no such task") {
		t.Fatalf("unknown task should error, got: %v", err)
	}
}
