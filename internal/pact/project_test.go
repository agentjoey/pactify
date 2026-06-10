package pact_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// newGitRepo mirrors engine_test.go's newRepo but WITHOUT chdir and WITHOUT
// setting PACT_DIR: the process cwd stays foreign and .pact resolves under the
// repo dir via the dir-aware API.
func newGitRepo(t *testing.T) string {
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

func TestProjectLifecycleFromForeignCwd(t *testing.T) {
	repo := newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other) // process cwd is NOT the repo
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	t.Setenv("PACT_DIR", "") // ensure default .pact resolution inside repo

	p := pact.At(repo)
	if err := p.Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Assign("T1", "F", "feat/x", "opencode", "claude-opus", ".pact/tasks/T1.md"); err != nil {
		t.Fatal(err)
	}

	// act as the worker via the actor override (no env mutation):
	w := pact.At(repo).As("opencode")
	if err := w.Join("opencode", "worker"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x"), 0o644)
	if err := w.Checkpoint("T1", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := w.Accept("T1"); err == nil {
		t.Fatal("worker self-accept must fail through the dir-aware path")
	}
	if err := p.As("claude-opus").Accept("T1"); err != nil {
		t.Fatal(err)
	}
	if err := p.As("claude-opus").Merge("F"); err != nil {
		t.Fatal(err)
	}

	st, _ := os.ReadFile(filepath.Join(repo, ".pact/STATE.yml"))
	if !strings.Contains(string(st), "status: shipped") {
		t.Fatalf("not shipped: %s", st)
	}
	// process cwd must never change (resolve symlinks: macOS TempDir is under
	// /var -> /private/var, so compare canonical paths).
	wd, _ := os.Getwd()
	wdReal, _ := filepath.EvalSymlinks(wd)
	otherReal, _ := filepath.EvalSymlinks(other)
	if wdReal != otherReal {
		t.Fatalf("process cwd must never change: got %q want %q", wd, other)
	}
}
