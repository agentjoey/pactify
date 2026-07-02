package pact

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/lockx"
)

// Merge contends on the shared base-integration lock: while another holder owns it
// (a concurrent merge in flight), Merge queues and — bounded by the timeout —
// fails rather than racing onto base. This proves the P1 lock is actually wired
// into the merge critical section (spec coordination-authority P1).
func TestMergeBlocksOnHeldBaseLock(t *testing.T) {
	// in-package test: shrink the wait so a held lock surfaces fast.
	old := baseIntegrationLockTimeout
	baseIntegrationLockTimeout = 120 * time.Millisecond
	defer func() { baseIntegrationLockTimeout = old }()

	repo := newLockRepo(t)
	t.Setenv("PACT_AGENT_ID", "rev")
	t.Setenv("PACT_DIR", "")

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
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x"), 0o644)
	if err := wk.Checkpoint("t1", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := p.As("rev").Accept("t1"); err != nil {
		t.Fatal(err)
	}

	// Hold the same lock the merge will reach for (shared git common dir).
	lockPath, err := gitx.GitPath(repo, "pactify-base.lock")
	if err != nil {
		t.Fatal(err)
	}
	release, err := lockx.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatalf("pre-acquire lock: %v", err)
	}

	mergeErr := p.As("rev").Merge("f")
	if mergeErr == nil {
		release()
		t.Fatal("merge succeeded while base lock was held; want it blocked on the lock")
	}

	// After releasing, the merge proceeds and ships.
	release()
	if err := p.As("rev").Merge("f"); err != nil {
		t.Fatalf("merge after release: %v", err)
	}
	st, _ := os.ReadFile(filepath.Join(repo, ".pact/STATE.yml"))
	if !contains(string(st), "status: shipped") {
		t.Fatalf("not shipped after lock release: %s", st)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func newLockRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.CombinedOutput()
	}
	return dir
}
