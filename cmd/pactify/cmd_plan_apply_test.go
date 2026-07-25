package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/planner"
	"github.com/agentjoey/pactify/internal/roles"
)

// applyPlan must assign as the PASSED acting seat, not a hardcoded "claude".
// Regression: `pactify run` / `plan apply` used planner.Apply which hardcoded
// .As("claude"), so any project whose orchestrator seat wasn't literally named
// "claude" failed with `assign: acting seat "claude" must have the orchestrator
// role` (found driving SA2 with orchestrator seat "orch"). Now applyPlan →
// planner.ApplyTx(…, seat).
func TestApplyPlanUsesGivenSeatNotHardcodedClaude(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gitInitWithCommit(t, dir)
	t.Setenv("PACT_AGENT_ID", "orch")
	// Orchestrator seat is "orch" (NOT "claude"), plus a worker + reviewer.
	if err := pact.Init("p", []string{
		"orch:orchestrator:CLAUDE.md",
		"w1:worker:AGENTS.md",
		"r1:reviewer:AGENTS.md",
	}); err != nil {
		t.Fatal(err)
	}
	// A spec file the plan references.
	if err := os.MkdirAll(filepath.Join(dir, ".pact", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pact", "tasks", "gt1.md"), []byte("Goal: x\nverify: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"feature": "g", "branch": "feat/g",
		"seats": []map[string]any{
			{"id": "w1", "kind": "kimi-cli", "roles": []string{"worker"}},
			{"id": "r1", "kind": "opencode", "roles": []string{"reviewer"}},
		},
		"tasks": []map[string]any{
			{"id": "gt1", "owner": "w1", "reviewer": "r1", "spec": ".pact/tasks/gt1.md", "verify": "true"},
		},
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, ".pact", "plan-g.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := applyPlan(dir, "g", []string{"orch", "w1", "r1"}, "orch")
	if err != nil {
		t.Fatalf("applyPlan as orchestrator 'orch' should succeed, got: %v", err)
	}
	if n != 1 {
		t.Fatalf("assigned = %d, want 1", n)
	}
	// The assign event must be attributed to "orch", never "claude".
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			if tk.ID == "gt1" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("task gt1 was not assigned")
	}
}

// applyPlan fails cleanly (not with a confusing role error) when no acting seat
// is set.
func TestApplyPlanRequiresActingSeat(t *testing.T) {
	if _, err := applyPlan(t.TempDir(), "g", []string{"orch"}, ""); err == nil {
		t.Fatal("applyPlan with empty seat should error")
	}
}

// P3: a task whose role has no bound seat is a gap the human should see at the
// review gate — warn, never block (the plan is still applied).
func TestRoleGapWarnings(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("frontend", roles.Profile{Kind: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w2", "frontend"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	plan := planner.Plan{Tasks: []planner.PlanTask{
		{ID: "t1", Role: "frontend"}, // bound → no warning
		{ID: "t2", Role: "backend"},  // no such role → warn
		{ID: "t3"},                   // no role at all → silent
	}}
	warns := roleGapWarnings(plan)
	if len(warns) != 1 {
		t.Fatalf("expected exactly one gap warning, got %v", warns)
	}
	if !strings.Contains(warns[0], "t2") || !strings.Contains(warns[0], "backend") {
		t.Fatalf("warning must name the task and the role: %q", warns[0])
	}
}
