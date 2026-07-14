package pact

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
)

// Correctness regression — review finding M9.
//
// Merge runs the git merge (MergeNoFF into base) and THEN appends the merge event
// to the ledger. A crash between the two leaves base with the merge commit but the
// ledger without the event → the feature is "accepted, not shipped". Every retry
// then tripped the empty-feature guard (base already contains the branch, so
// IsAncestor is true) and refused, stranding the feature unshippable forever. The
// re-run must instead RECOVER: recognize the merge already landed and just record
// the event.

func m9FeatureStatus(t *testing.T, repo, feature string) string {
	t.Helper()
	st, err := At(repo).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st.Features {
		if f.ID == feature {
			return f.Status
		}
	}
	t.Fatalf("feature %s not found", feature)
	return ""
}

func TestSEC_M9_MergeRecoversFromCrashAfterGitMerge(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "rev")
	repo := newLockRepo(t)
	base, err := gitx.CurrentBranch(repo)
	if err != nil || base == "" {
		t.Fatalf("current branch: %q %v", base, err)
	}
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, e := c.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %v %s", args, e, out)
		}
	}

	// Gitignore .pact (as linx does) so the worker's checkpoint commits only real
	// work — the feature branch genuinely diverges from base by one commit.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".pact/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".gitignore")
	git("commit", "-q", "-m", "ignore .pact")

	p := At(repo)
	if err := p.Init("p", []string{"rev:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "scaffold")

	if err := p.Assign("t1", "f", "feat/x", "w", "rev", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	wk := At(repo).As("w")
	if err := wk.Join("w", "worker"); err != nil { // checks out feat/x
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("work"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wk.Checkpoint("t1", "done"); err != nil { // commits impl.txt on feat/x
		t.Fatal(err)
	}
	if err := p.As("rev").Accept("t1"); err != nil {
		t.Fatal(err)
	}

	// Reproduce the crash: do the git merge into base but DON'T record the pact
	// merge event (as if the process died after MergeNoFF, before appendAndRender).
	if err := gitx.Checkout(repo, base); err != nil {
		t.Fatal(err)
	}
	if err := gitx.MergeNoFF(repo, "feat/x", "Merge f (feat/x)"); err != nil {
		t.Fatal(err)
	}

	// Precondition: base has the merge, but the ledger doesn't → not shipped yet.
	if got := m9FeatureStatus(t, repo, "f"); got == "shipped" {
		t.Fatalf("bad setup: feature already shipped before recovery (%q)", got)
	}
	if !gitx.IsAncestor(repo, "feat/x", base) {
		t.Fatal("bad setup: feat/x is not merged into base")
	}

	// Recovery: re-running merge must COMPLETE it (record the event → shipped), not
	// refuse the feature as empty.
	if err := p.As("rev").Merge("f"); err != nil {
		t.Fatalf("merge must recover a crashed merge, got: %v", err)
	}
	if got := m9FeatureStatus(t, repo, "f"); got != "shipped" {
		t.Fatalf("feature status = %q after recovery, want shipped", got)
	}
}
