package pact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
)

// When PACT_DIR is an ABSOLUTE path (how the orchestrate runner pins a worker to
// the driver's worktree), pact must root BOTH the ledger AND its git ops at that
// repo — even when the process cwd is somewhere else entirely. Otherwise the
// worker's checkpoint commits to the wrong place / fails, the branch stays empty,
// and the driver never sees it (the opencode delivery bug).
func TestCheckpointHonorsAbsolutePactDir(t *testing.T) {
	// Set up a normal project in `repo` (dir-aware, no cwd dependence).
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)
	if err := At(repo).As("orch").Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := At(repo).As("orch").Assign("t1", "f", "feat/x", "w", "orch", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	base, _ := gitx.CurrentBranch(repo)

	// Now act as the WORKER the way the MCP server does: process cwd is elsewhere,
	// PACT_DIR is the absolute path to the repo's .pact. The package-level / At(".")
	// path is what the MCP tools use.
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_DIR", filepath.Join(repo, ".pact"))
	t.Setenv("PACT_AGENT_ID", "w")

	if err := At(".").Join("w", "worker"); err != nil { // must checkout feat/x IN repo
		t.Fatalf("join from foreign cwd with abs PACT_DIR: %v", err)
	}
	// Worker's real work lands in the repo (its actual worktree).
	if err := os.WriteFile(filepath.Join(repo, "work.txt"), []byte("real work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := At(".").Checkpoint("t1", "ok"); err != nil {
		t.Fatalf("checkpoint from foreign cwd with abs PACT_DIR: %v", err)
	}

	// The commit must have landed on feat/x IN repo (branch advanced over base).
	if !gitx.BranchExists(repo, "feat/x") {
		t.Fatal("feat/x not created in repo — the worker's commit went elsewhere")
	}
	if gitx.IsAncestor(repo, "feat/x", base) {
		t.Fatal("feat/x has no commits over base — work not committed to the repo")
	}
}
