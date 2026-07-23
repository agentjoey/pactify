package orchestrate

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
)

// gitBranchFrom creates branch `name` at HEAD (without switching) so tests can
// set up an integration base distinct from the git default branch.
func gitBranchFrom(t *testing.T, dir, name string) {
	t.Helper()
	c := exec.Command("git", "branch", name)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git branch %s: %v %s", name, err, out)
	}
}

// 2026-07-23 tradelinks feedback: the sandbox created its worktree/feature
// branch off gitx.DefaultBranch (main) instead of the pact-configured base, so
// a project whose integration base is a non-default branch had its workers
// building off main — blind to the authoritative plan committed on the real
// base. sandboxBase must resolve the SAME base merge uses: the pact ledger's
// configured base_branch, falling back to git only when unset.
func TestSandboxBaseUsesPactConfiguredBase(t *testing.T) {
	dir := newProject(t) // inits on the git default branch; base captured there
	gitBranchFrom(t, dir, "integ")
	if err := pact.At(dir).As("orch").ConfigBaseBranch("integ"); err != nil {
		t.Fatal(err)
	}
	if got := sandboxBase(dir); got != "integ" {
		t.Fatalf("sandboxBase must honor the pact-configured base, got %q want integ", got)
	}
}

// Integration: a sandbox run for a project whose pact base is a NON-default
// branch must land the merge on that base — and must NOT move the git default
// branch (main). This is the tradelinks scenario end-to-end.
func TestRunSandbox_LandsOnPactBaseNotGitDefault(t *testing.T) {
	dir := newProject(t)
	def, _ := gitx.CurrentBranch(dir) // the git default the sandbox used to (wrongly) target
	gitBranchFrom(t, dir, "integ")
	if err := pact.At(dir).As("orch").ConfigBaseBranch("integ"); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	integBefore := revParse(t, dir, "integ")
	defBefore := revParse(t, dir, def)

	if err := RunSandbox(context.Background(), sandboxOpts(t, dir)); err != nil {
		t.Fatalf("RunSandbox: %v", err)
	}

	if revParse(t, dir, "integ") == integBefore {
		t.Fatal("pact base 'integ' did not advance — the sandbox did not integrate onto the configured base")
	}
	if revParse(t, dir, def) != defBefore {
		t.Fatalf("git default %q moved — the sandbox must not touch it when the pact base is elsewhere", def)
	}
}
