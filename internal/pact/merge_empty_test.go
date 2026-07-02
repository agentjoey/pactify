package pact

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A feature whose declared branch exists but carries NO commits over base (the
// worker checkpointed without committing any work) must NOT be recorded shipped —
// the merge integrates nothing, so "shipped" would be a phantom (pact state ahead
// of git; base unchanged). Merge must refuse so it escalates loudly. This mirrors
// linx, where .pact is gitignored so a no-work checkpoint commits nothing.
func TestMergeRefusesEmptyFeatureBranch(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "rev")
	repo := newLockRepo(t)

	// Gitignore .pact (as linx does) so a no-work checkpoint commits nothing and the
	// feature branch genuinely stays at base — otherwise the tracked ledger change
	// would itself commit and mask the empty branch.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".pact/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"add", ".gitignore"}, {"commit", "-q", "-m", "ignore .pact"}} {
		c := exec.Command("git", a...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}

	p := At(repo)
	if err := p.Init("p", []string{"rev:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	// Commit the baked entry files (CLAUDE.md/AGENTS.md) onto base first — as linx
	// has them long-committed — so a later no-work checkpoint has truly nothing to add.
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "scaffold"}} {
		c := exec.Command("git", a...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	if err := p.Assign("t1", "f", "feat/x", "w", "rev", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	wk := At(repo).As("w")
	if err := wk.Join("w", "worker"); err != nil { // on feat/x
		t.Fatal(err)
	}
	// Checkpoint WITHOUT writing any file → no commit (ledger is gitignored) → feat/x
	// stays at base.
	if err := wk.Checkpoint("t1", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := p.As("rev").Accept("t1"); err != nil {
		t.Fatal(err)
	}
	if err := p.As("rev").Merge("f"); err == nil {
		t.Fatal("merge shipped an empty feature branch (no commits over base) — should refuse")
	}
}
