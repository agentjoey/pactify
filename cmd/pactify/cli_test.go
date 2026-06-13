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
	if out, err := run(nil, "version"); err != nil || !strings.Contains(out, "pactify dev") {
		t.Fatalf("version: %v %s", err, out)
	}
	if out, err := run(nil, "--version"); err != nil || strings.TrimSpace(out) != versionString("dev", "none", "unknown") {
		t.Fatalf("--version should print exactly the version line: %v %q", err, out)
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

func TestRootProjectChdirs(t *testing.T) {
	start, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(start) })
	dir := t.TempDir()
	if err := rootProject(dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.Getwd()
	want, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != want {
		t.Fatalf("cwd = %q, want %q", gotResolved, want)
	}
}

func TestRootProjectEmptyIsNoop(t *testing.T) {
	start, _ := os.Getwd()
	if err := rootProject(""); err != nil {
		t.Fatal(err)
	}
	now, _ := os.Getwd()
	if now != start {
		t.Fatalf("empty --project must not chdir: %q -> %q", start, now)
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

// TestCLIOrchestrateHelpAndDryRun smoke-tests the orchestrate command: --help
// surfaces the flags, and --dry-run on an inited project exits cleanly without
// launching any agent.
func TestCLIOrchestrateHelpAndDryRun(t *testing.T) {
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
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}

	help, err := run(nil, "orchestrate", "--help")
	if err != nil {
		t.Fatalf("orchestrate --help: %v %s", err, help)
	}
	for _, flag := range []string{"--feature", "--resume", "--max-rework", "--max-iters", "--dry-run", "--seat-kind"} {
		if !strings.Contains(help, flag) {
			t.Fatalf("orchestrate --help missing %s:\n%s", flag, help)
		}
	}

	env := []string{"PACT_AGENT_ID=orch"}
	if out, err := run(env, "init", "--project", "p", "--seat", "orch:orchestrator,reviewer:CLAUDE.md", "--seat", "w:worker:AGENTS.md"); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".pact", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pact", "tasks", "t1.md"), []byte("# t1\n\nverify: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run(env, "assign", "t1", "--feature", "f1", "--branch", "feat-f1", "--owner", "w", "--reviewer", "orch", "--spec", ".pact/tasks/t1.md"); err != nil {
		t.Fatalf("assign: %v %s", err, out)
	}

	out, err := run(env, "orchestrate", "--dry-run", "--seat-kind", "w=opencode", "--seat-kind", "orch=claude-code")
	if err != nil {
		t.Fatalf("orchestrate --dry-run: %v %s", err, out)
	}
}
