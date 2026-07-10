package pact

import (
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/paths"
)

// Escalate appends an escalate event to the ledger, and validate/replay tolerate
// it without changing the task state machine.
func TestEscalateRoundtrip(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)

	p := At(repo)
	if err := p.Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.As("orch").Assign("t1", "f1", "feat/f1", "w", "orch", "tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	if err := p.As("orch").Escalate("t1", "f1", "rework limit reached", "orch"); err != nil {
		t.Fatalf("Escalate: %v", err)
	}

	evs, err := event.ReadAll(paths.LogIn(repo))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range evs {
		if e.EventType == "escalate" {
			found = true
			if e.TaskID != "t1" || e.Feature != "f1" {
				t.Fatalf("escalate scope wrong: task=%q feature=%q", e.TaskID, e.Feature)
			}
			if e.Payload["reason"] != "rework limit reached" {
				t.Fatalf("escalate reason wrong: %v", e.Payload["reason"])
			}
			if e.Payload["seat"] != "orch" {
				t.Fatalf("escalate seat wrong: %v", e.Payload["seat"])
			}
		}
	}
	if !found {
		t.Fatal("escalate event not found in log")
	}

	// Validate must not reject the new event type.
	if err := p.Validate(); err != nil {
		t.Fatalf("validate after escalate: %v", err)
	}

	// Task status must remain assigned (escalate is not a state transition).
	st, _, err := p.state()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			if tk.ID == "t1" && tk.Status != "assigned" {
				t.Fatalf("escalate changed task status to %q", tk.Status)
			}
		}
	}
}

// Only an orchestrator seat may write escalate events.
func TestEscalateRequiresOrchestratorRole(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "w")
	repo := newLockRepo(t)

	p := At(repo)
	if err := p.Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.As("orch").Assign("t1", "f1", "feat/f1", "w", "orch", "tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}

	if err := p.As("w").Escalate("t1", "f1", "x", "w"); err == nil {
		t.Fatal("worker should not be allowed to escalate")
	} else if !strings.Contains(err.Error(), "orchestrator") {
		t.Fatalf("error should mention orchestrator role, got: %v", err)
	}
}
