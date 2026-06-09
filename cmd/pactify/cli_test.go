package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pactify")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/pactify")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd() // .../cmd/pactify
	return filepath.Dir(filepath.Dir(wd))
}

func TestCLIHelpAndFullFlow(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	run := func(env []string, args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Dir = dir
		c.Env = append(os.Environ(), env...)
		out, err := c.CombinedOutput()
		return string(out), err
	}
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.CombinedOutput()
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	exec.Command("git", "-C", dir, "add", "-A").Run()
	exec.Command("git", "-C", dir, "commit", "-q", "-m", "base").Run()

	if out, err := run(nil, "help"); err != nil || !strings.Contains(out, "init") {
		t.Fatalf("help: %v %s", err, out)
	}
	orch := []string{"PACT_AGENT_ID=claude-opus"}
	work := []string{"PACT_AGENT_ID=opencode"}
	if out, err := run(orch, "init", "--project", "p", "--seat", "claude-opus:orchestrator,reviewer:CLAUDE.md", "--seat", "opencode:worker:AGENTS.md"); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	if out, err := run(orch, "assign", "T1", "--feature", "F", "--branch", "feat/x", "--owner", "opencode", "--reviewer", "claude-opus", "--spec", ".pact/tasks/T1.md"); err != nil {
		t.Fatalf("assign: %v %s", err, out)
	}
	if out, err := run(work, "join", "opencode", "--roles", "worker"); err != nil {
		t.Fatalf("join: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("c"), 0o644)
	if out, err := run(work, "checkpoint", "T1", "--evidence", "ok"); err != nil {
		t.Fatalf("checkpoint: %v %s", err, out)
	}
	if out, err := run(orch, "accept", "T1"); err != nil {
		t.Fatalf("accept: %v %s", err, out)
	}
	if out, err := run(orch, "merge", "F"); err != nil {
		t.Fatalf("merge: %v %s", err, out)
	}
	if out, err := run(orch, "validate"); err != nil {
		t.Fatalf("validate: %v %s", err, out)
	}
	st, _ := run(orch, "status")
	if !strings.Contains(st, "status: shipped") {
		t.Fatalf("feature not shipped: %s", st)
	}
}

func TestCLIFailsClosedWithoutAgentID(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	c := exec.Command(bin, "init", "--project", "p", "--seat", "a:worker:A.md")
	c.Dir = dir
	c.Env = append(os.Environ(), "PACT_AGENT_ID=")
	if out, err := c.CombinedOutput(); err == nil {
		t.Fatalf("must fail closed, got: %s", out)
	}
}
