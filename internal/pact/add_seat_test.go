package pact

import "testing"

// AddSeat appends a new seat to the roster (with kind) after init, so a roster is
// no longer frozen at init time. Acting seat must have an orchestrator role.
func TestAddSeat_AppendsToRosterWithKind(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Init("p", []string{
		"claude-opus:orchestrator,reviewer:CLAUDE.md:claude-code",
		"opencode:worker:AGENTS.md:opencode",
	}); err != nil {
		t.Fatal(err)
	}
	if err := At(".").As("claude-opus").AddSeat("kimi-worker:worker:AGENTS.md:kimi-cli"); err != nil {
		t.Fatalf("AddSeat: %v", err)
	}
	st, err := At(".").StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, a := range st.Agents {
		if a.ID == "kimi-worker" {
			found = true
			if a.Kind != "kimi-cli" {
				t.Fatalf("kimi-worker kind = %q, want kimi-cli", a.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("kimi-worker not in roster: %+v", st.Agents)
	}
}

// init seats now also carry kind into the roster projection (was dropped before).
func TestInitSeatsCarryKindIntoRoster(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md:claude-code"}); err != nil {
		t.Fatal(err)
	}
	st, _ := At(".").StateProjection()
	if len(st.Agents) == 0 || st.Agents[0].Kind != "claude-code" {
		t.Fatalf("init seat kind not projected: %+v", st.Agents)
	}
}

// A duplicate seat id (collides with an init seat) is rejected.
func TestAddSeat_RejectsDuplicate(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	if err := At(".").As("claude-opus").AddSeat("opencode:worker:AGENTS.md"); err == nil {
		t.Fatal("duplicate seat id should be rejected")
	}
}

// Only a seat with the orchestrator role may add seats (roster management).
func TestAddSeat_RequiresOrchestrator(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	if err := At(".").As("opencode").AddSeat("kimi:worker:AGENTS.md"); err == nil {
		t.Fatal("a worker seat must not be able to add seats")
	}
}
