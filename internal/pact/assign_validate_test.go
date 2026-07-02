package pact

import (
	"strings"
	"testing"
)

// Assign is the door through which task/feature/branch names enter the ledger
// and, later, git argv (CheckoutOrCreate, MergeNoFF's message, AddWorktree) —
// a value like "-evil" would there be parsed as a git flag. So Assign holds
// taskID and feature to the seat slug pattern and vets branch as a git branch
// name, rejecting hostile shapes at the boundary with actionable errors.
func TestAssignValidatesIdsAndBranch(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)
	if err := At(repo).Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	orch := At(repo).As("orch")

	// A slash-y branch is normal git practice and must stay accepted.
	if err := orch.Assign("t1-good", "f-good", "feat/x", "w", "orch", "", nil); err != nil {
		t.Fatalf("valid assign rejected: %v", err)
	}

	cases := []struct {
		name                  string
		task, feature, branch string
		wantErr               string
	}{
		{"dash-prefixed branch reads as a git flag", "t2", "f-good", "-evil", "not a valid git branch name"},
		{"uppercase task id", "T2", "f-good", "feat/x", "not a slug"},
		{"task id with a space", "t 2", "f-good", "feat/x", "not a slug"},
		{"uppercase feature", "t2", "F", "feat/x", "not a slug"},
		{"dash-prefixed task id", "-evil", "f-good", "feat/x", "not a slug"},
	}
	for _, tc := range cases {
		err := orch.Assign(tc.task, tc.feature, tc.branch, "w", "orch", "", nil)
		if err == nil {
			t.Errorf("%s: assign(%q,%q,%q) must be rejected", tc.name, tc.task, tc.feature, tc.branch)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error %q must contain %q", tc.name, err, tc.wantErr)
		}
	}
}
