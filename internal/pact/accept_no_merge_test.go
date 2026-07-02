package pact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
)

// Accepting the LAST task of a feature must NOT ship/merge it — merge is always a
// separate, explicit step run by the orchestrator. Spec coordination-authority P3-2.
func TestAcceptDoesNotMergeLastTask(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "rev")
	repo := newLockRepo(t)
	base, _ := gitx.CurrentBranch(repo)

	p := At(repo)
	if err := p.Init("p", []string{"rev:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Assign("t1", "f", "feat/x", "w", "rev", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	wk := At(repo).As("w")
	if err := wk.Join("w", "worker"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x\n"), 0o644)
	if err := wk.Checkpoint("t1", "ok"); err != nil {
		t.Fatal(err)
	}

	if err := p.As("rev").Accept("t1"); err != nil { // the only/last task
		t.Fatal(err)
	}

	// Feature must NOT be shipped by accept.
	st, err := p.StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st.Features {
		if f.ID == "f" && f.Status == "shipped" {
			t.Fatal("accept shipped the feature — merge must be a separate explicit step")
		}
	}
	// And the feature branch must not yet be integrated into base.
	if gitx.IsAncestor(repo, "feat/x", base) {
		t.Fatal("accept merged feat/x into base — accept must never trigger a merge")
	}
}
