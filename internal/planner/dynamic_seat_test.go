package planner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/planner"
)

// TestApplyAutoJoinsProposedSeat: a plan may propose a NEW seat (id/kind/roles)
// and use it as a task owner; apply auto-registers the seat with a join carrying
// its kind, so the seat lands on the roster (Agents[].Kind) drivable (spec §6 WS-K).
func TestApplyAutoJoinsProposedSeat(t *testing.T) {
	dir := newGitRepo(t)
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "claude")

	if err := pact.At(dir).As("claude").Init("testp", []string{
		"claude:orchestrator,reviewer:CLAUDE.md",
	}); err != nil {
		t.Fatal(err)
	}

	os.MkdirAll(filepath.Join(dir, ".pact", "tasks"), 0o755)
	spec := ".pact/tasks/demo-t1.md"
	os.WriteFile(filepath.Join(dir, spec), []byte("# t1"), 0o644)

	plan := planner.Plan{
		Feature: "demo",
		Branch:  "feat-demo",
		// A brand-new seat the roster does not have yet.
		Seats: []planner.PlanSeat{{ID: "gem", Kind: "gemini-cli", Roles: []string{"worker"}}},
		Tasks: []planner.PlanTask{
			{ID: "demo-t1", Owner: "gem", Reviewer: "claude", Spec: spec, Verify: "go test"},
		},
	}
	roster := []string{"claude"} // gem is NOT pre-rostered

	n, err := planner.ApplyTx(dir, plan, roster, "claude")
	if err != nil {
		t.Fatalf("ApplyTx: %v", err)
	}
	if n != 1 {
		t.Fatalf("assigned = %d, want 1", n)
	}

	// The proposed seat is now on the roster with its declared kind.
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	var kind string
	found := false
	for _, a := range st.Agents {
		if a.ID == "gem" {
			found, kind = true, a.Kind
		}
	}
	if !found {
		t.Fatal("proposed seat gem not auto-joined onto the roster")
	}
	if kind != "gemini-cli" {
		t.Fatalf("gem kind = %q, want %q", kind, "gemini-cli")
	}
}

// TestApplyAutoJoinIdempotent: re-applying a plan whose proposed seat is already on
// the roster does not fail (the seat is skipped, no duplicate join needed).
func TestApplyAutoJoinIdempotent(t *testing.T) {
	dir := newGitRepo(t)
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "claude")

	if err := pact.At(dir).As("claude").Init("testp", []string{
		"claude:orchestrator,reviewer:CLAUDE.md",
		"gem:worker:G.md",
	}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, ".pact", "tasks"), 0o755)
	spec := ".pact/tasks/demo-t1.md"
	os.WriteFile(filepath.Join(dir, spec), []byte("# t1"), 0o644)

	plan := planner.Plan{
		Feature: "demo",
		Branch:  "feat-demo",
		Seats:   []planner.PlanSeat{{ID: "gem", Kind: "gemini-cli", Roles: []string{"worker"}}},
		Tasks: []planner.PlanTask{
			{ID: "demo-t1", Owner: "gem", Reviewer: "claude", Spec: spec, Verify: "go test"},
		},
	}
	if _, err := planner.ApplyTx(dir, plan, []string{"claude", "gem"}, "claude"); err != nil {
		t.Fatalf("ApplyTx with already-rostered proposed seat: %v", err)
	}
}
