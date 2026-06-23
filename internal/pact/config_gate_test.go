package pact

import (
	"testing"
)

// ConfigGate records the gate; GateConfig reads back the latest setting, and a
// validate pass tolerates the new event type. Spec: per-project hard gate.
func TestConfigGateRoundtrip(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)

	p := At(repo)
	if err := p.Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.GateConfig(); ok {
		t.Fatal("fresh project should have no gate config")
	}

	want := "pnpm build && pnpm typecheck && pnpm lint && pnpm format:check && pnpm test"
	if err := p.As("orch").ConfigGate(want); err != nil {
		t.Fatal(err)
	}
	got, ok := p.GateConfig()
	if !ok || got != want {
		t.Fatalf("GateConfig = (%q,%v), want (%q,true)", got, ok, want)
	}

	// A later config overrides the earlier one.
	want2 := "pnpm test"
	if err := p.As("orch").ConfigGate(want2); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.GateConfig(); got != want2 {
		t.Fatalf("GateConfig after override = %q, want %q", got, want2)
	}

	// Empty command is rejected.
	if err := p.As("orch").ConfigGate("  "); err == nil {
		t.Fatal("empty gate command should be rejected")
	}

	// The new event type must not break validation.
	if err := p.Validate(); err != nil {
		t.Fatalf("validate after config_gate: %v", err)
	}
}
