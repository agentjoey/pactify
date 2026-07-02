package pact

import (
	"strings"
	"testing"
)

// The coordination verbs — assign, start, cancel, withdraw, merge, config gate,
// config base-branch — shape the shared plan, so only a roster seat carrying the
// orchestrator role may drive them. Before this gate any registered worker seat
// could fabricate assignments, retire someone's work, rewrite the safety gate or
// base branch, or drive a merge. Join/Checkpoint/Accept/Changes stay
// identity-gated (owner/reviewer) and are unaffected.
func TestOrchestratorRoleGateRejectsWorkerSeat(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)
	if err := At(repo).Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := At(repo).As("orch").Assign("t1", "f", "feat/x", "w", "orch", "", nil); err != nil {
		t.Fatal(err)
	}

	wk := At(repo).As("w")
	verbs := map[string]func() error{
		"assign":             func() error { return wk.Assign("t2", "f", "feat/x", "w", "orch", "", nil) },
		"start":              func() error { return wk.Start("t1") },
		"cancel":             func() error { return wk.Cancel("t1") },
		"withdraw":           func() error { return wk.Withdraw("f") },
		"merge":              func() error { return wk.Merge("f") },
		"config gate":        func() error { return wk.ConfigGate("go test ./...") },
		"config base-branch": func() error { return wk.ConfigBaseBranch("main") },
	}
	for verb, call := range verbs {
		err := call()
		if err == nil {
			t.Errorf("%s: worker seat must be rejected", verb)
			continue
		}
		// The error must be actionable: name the seat and the missing role.
		if !strings.Contains(err.Error(), `"w"`) || !strings.Contains(err.Error(), "orchestrator role") {
			t.Errorf("%s: error must name the seat and missing role, got: %v", verb, err)
		}
	}

	// The same verbs pass for the orchestrator seat (merge is exercised by the
	// full-flow merge tests; here the plan-shaping subset suffices).
	orch := At(repo).As("orch")
	if err := orch.Start("t1"); err != nil {
		t.Fatalf("orchestrator start: %v", err)
	}
	if err := orch.ConfigGate("go test ./..."); err != nil {
		t.Fatalf("orchestrator config gate: %v", err)
	}
	if err := orch.Cancel("t1"); err != nil {
		t.Fatalf("orchestrator cancel: %v", err)
	}
	if err := orch.Withdraw("f"); err != nil {
		t.Fatalf("orchestrator withdraw: %v", err)
	}
}
