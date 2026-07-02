package pact

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	if dir != "" {
		c.Dir = dir
	}
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newRepoWithOrigin makes a working repo wired to a bare `origin`, with the base
// branch pushed. Returns (repo, base).
func newRepoWithOrigin(t *testing.T) (string, string) {
	t.Helper()
	repo := newLockRepo(t)
	base, err := gitx.CurrentBranch(repo)
	if err != nil || base == "" {
		t.Fatalf("current branch: %v", err)
	}
	bare := t.TempDir()
	gitRun(t, "", "init", "--bare", "-q", bare)
	gitRun(t, repo, "remote", "add", "origin", bare)
	gitRun(t, repo, "push", "-q", "origin", base)
	return repo, base
}

// advanceOrigin pushes a commit touching file=content onto origin/<base>, as if a
// second writer moved the remote base — without touching the local repo's tree.
func advanceOrigin(t *testing.T, repo, base, file, content string) {
	t.Helper()
	bareURL := func() string {
		c := exec.Command("git", "remote", "get-url", "origin")
		c.Dir = repo
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("remote get-url: %v %s", err, out)
		}
		return string(out[:len(out)-1])
	}()
	tmp := t.TempDir()
	gitRun(t, "", "clone", "-q", bareURL, tmp)
	os.WriteFile(filepath.Join(tmp, file), []byte(content), 0o644)
	gitRun(t, tmp, "add", "-A")
	gitRun(t, tmp, "-c", "user.email=t@t.t", "-c", "user.name=t", "commit", "-q", "-m", "advance origin")
	gitRun(t, tmp, "push", "-q", "origin", "HEAD:"+base)
}

// shipReady drives a feature to all-accepted (worker on its branch), ready to merge.
func shipReady(t *testing.T, repo string) {
	t.Helper()
	p := At(repo)
	if err := p.Init("p", []string{"rev:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Assign("t1", "f", "feat/x", "w", "rev", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	wk := At(repo).As("w")
	if err := wk.Join("w", "worker"); err != nil { // checks out feat/x
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("feature work\n"), 0o644)
	if err := wk.Checkpoint("t1", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := p.As("rev").Accept("t1"); err != nil {
		t.Fatal(err)
	}
}

// With origin/base advanced by another writer, merge fetches, rebases the feature
// on top, and integrates BOTH — base ends as a descendant of origin/base (no
// divergence). Spec coordination-authority P3-3.
func TestMergeFetchAwareIntegratesAdvancedBase(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "rev")
	repo, base := newRepoWithOrigin(t)
	shipReady(t, repo)

	advanceOrigin(t, repo, base, "other.txt", "other writer\n")

	if err := At(repo).As("rev").Merge("f"); err != nil {
		t.Fatalf("merge: %v", err)
	}

	if !gitx.IsAncestor(repo, "origin/"+base, base) {
		t.Fatal("base is not a descendant of origin/base after merge — divergence (push would fork)")
	}
	for _, f := range []string{"other.txt", "impl.txt"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Fatalf("%s not integrated into base after merge", f)
		}
	}
}

// When the feature cannot cleanly rebase onto an advanced origin/base (conflict),
// merge refuses (aborting the rebase) rather than recording a divergent merge.
// Spec coordination-authority P3-3.
func TestMergeFetchAwareRefusesUnrebasable(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "rev")
	repo, base := newRepoWithOrigin(t)
	shipReady(t, repo) // feature adds impl.txt="feature work"

	// origin advances the SAME file with conflicting content → unrebasable.
	advanceOrigin(t, repo, base, "impl.txt", "origin work\n")

	if err := At(repo).As("rev").Merge("f"); err == nil {
		t.Fatal("merge should refuse when the feature cannot cleanly rebase onto origin/base")
	}
	// The feature must NOT be recorded shipped, and the tree must be clean (rebase aborted).
	if dirty, _ := gitx.HasChanges(repo); dirty {
		t.Fatal("working tree left dirty after a refused merge")
	}
}
