package pact

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
)

// A checkpoint must never commit to base: when the task's feature declares its own
// branch but HEAD is on base, checkpoint refuses (work belongs on the feature
// branch; only `pactify merge` writes base). Spec coordination-authority P3-1.
func TestCheckpointRefusesOnBaseWhenFeatureHasBranch(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "rev")
	repo := newLockRepo(t)
	base, _ := gitx.CurrentBranch(repo)

	p := At(repo)
	if err := p.Init("p", []string{"rev:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Assign("T1", "F", "feat/x", "w", "rev", ".pact/tasks/T1.md", nil); err != nil {
		t.Fatal(err)
	}

	// Worker does NOT join (so HEAD stays on base), then tries to checkpoint.
	if err := gitx.Checkout(repo, base); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x\n"), 0o644)
	err := At(repo).As("w").Checkpoint("T1", "ok")
	if err == nil {
		t.Fatal("checkpoint on base with a feature branch declared should be refused")
	}

	// And nothing was committed to base.
	if log := gitLog(t, repo); contains(log, "checkpoint by w") {
		t.Fatalf("a checkpoint commit landed on base: %s", log)
	}
}

// The happy path still works: on the feature branch, checkpoint commits there.
func TestCheckpointCommitsOnFeatureBranch(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "rev")
	repo := newLockRepo(t)

	p := At(repo)
	if err := p.Init("p", []string{"rev:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Assign("T1", "F", "feat/x", "w", "rev", ".pact/tasks/T1.md", nil); err != nil {
		t.Fatal(err)
	}
	wk := At(repo).As("w")
	if err := wk.Join("w", "worker"); err != nil { // checks out feat/x
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x\n"), 0o644)
	if err := wk.Checkpoint("T1", "ok"); err != nil {
		t.Fatalf("checkpoint on feature branch should succeed: %v", err)
	}
	if cur, _ := gitx.CurrentBranch(repo); cur != "feat/x" {
		t.Fatalf("expected HEAD on feat/x, got %q", cur)
	}
}

func gitLog(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("git", "log", "--all", "--oneline")
	c.Dir = dir
	out, _ := c.CombinedOutput()
	return string(out)
}
