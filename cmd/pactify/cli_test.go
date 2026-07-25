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
	// Isolate the registry: init/orchestrate now auto-register, and tests must
	// not touch the developer's real ~/.pactify/projects.json.
	home := t.TempDir()
	run := func(env []string, args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Dir = dir
		c.Env = append(append(os.Environ(), "PACTIFY_HOME="+home), env...)
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
	} else if !strings.Contains(out, "registered") {
		t.Fatalf("init must auto-register the project for the dashboard: %q", out)
	}
	// The project is now visible without a manual `pactify register`.
	if out, _ := run(nil, "list"); !strings.Contains(out, dir) {
		t.Fatalf("auto-registered project must appear in `list`: %q", out)
	}
	if out, err := run(orch, "assign", "t1", "--feature", "f", "--branch", "feat/x", "--owner", "opencode", "--reviewer", "claude-opus", "--spec", ".pact/tasks/t1.md"); err != nil {
		t.Fatalf("assign: %v %s", err, out)
	}
	// seat identity: `seat use` binds the working copy default (no env needed),
	// `seat` reports the resolved identity + source. With env set, env wins.
	if out, err := run(orch, "seat", "use", "opencode"); err != nil {
		t.Fatalf("seat use: %v %s", err, out)
	}
	if b, err := os.ReadFile(filepath.Join(dir, ".pact/seat")); err != nil || strings.TrimSpace(string(b)) != "opencode" {
		t.Fatalf("seat use must write .pact/seat: %v %q", err, b)
	}
	if excl, _ := os.ReadFile(filepath.Join(dir, ".git/info/exclude")); !strings.Contains(string(excl), ".pact/seat") {
		t.Fatal("seat use must exclude .pact/seat from git")
	}
	noEnv := []string{"PACT_AGENT_ID="} // override any ambient identity → file layer
	if out, _ := run(noEnv, "seat"); !strings.Contains(out, "opencode") || !strings.Contains(out, "file") {
		t.Fatalf("bare `seat` (no env) must report the file identity: %q", out)
	}
	if out, _ := run(orch, "seat"); !strings.Contains(out, "claude-opus") || !strings.Contains(out, "env") {
		t.Fatalf("`seat` with env must report env identity winning: %q", out)
	}
	if out, err := run(work, "join", "opencode", "--roles", "worker", "--task", "t1"); err != nil {
		t.Fatalf("join: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("c"), 0o644)
	if out, err := run(work, "checkpoint", "t1", "--evidence", "ok"); err != nil {
		t.Fatalf("checkpoint: %v %s", err, out)
	}
	if out, err := run(orch, "accept", "t1", "--evidence", "verify green 16/16"); err != nil {
		t.Fatalf("accept: %v %s", err, out)
	}
	if log, err := os.ReadFile(filepath.Join(dir, ".pact/log.jsonl")); err != nil || !strings.Contains(string(log), "verify green 16/16") {
		t.Fatalf("accept --evidence must reach the ledger: %v\n%s", err, log)
	}
	if out, err := run(orch, "merge", "f"); err != nil {
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
	c.Env = append(os.Environ(), "PACT_AGENT_ID=", "PACTIFY_HOME="+t.TempDir())
	if out, err := c.CombinedOutput(); err == nil {
		t.Fatalf("must fail closed, got: %s", out)
	}
}

func TestAgentScanRegisterUnregister(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	t.Setenv("PACTIFY_HOME", dir)

	// scan exits 0 and contains a known kind
	out, err := exec.Command(bin, "agent", "scan").CombinedOutput()
	if err != nil {
		t.Fatalf("agent scan: %v %s", err, out)
	}
	if !strings.Contains(string(out), "opencode") {
		t.Fatalf("agent scan missing opencode: %s", out)
	}

	// register opencode
	out, err = exec.Command(bin, "agent", "register", "opencode", "--label", "test-oc").CombinedOutput()
	if err != nil {
		t.Fatalf("agent register opencode: %v %s", err, out)
	}

	// scan now shows registered
	out, err = exec.Command(bin, "agent", "scan").CombinedOutput()
	if err != nil {
		t.Fatalf("agent scan after register: %v %s", err, out)
	}
	if !strings.Contains(string(out), "[registered]") {
		t.Fatalf("agent scan missing [registered] after register: %s", out)
	}

	// unregister opencode
	out, err = exec.Command(bin, "agent", "unregister", "opencode").CombinedOutput()
	if err != nil {
		t.Fatalf("agent unregister opencode: %v %s", err, out)
	}

	// scan no longer shows registered
	out, err = exec.Command(bin, "agent", "scan").CombinedOutput()
	if err != nil {
		t.Fatalf("agent scan after unregister: %v %s", err, out)
	}
	if strings.Contains(string(out), "[registered]") {
		t.Fatalf("agent scan still shows [registered] after unregister: %s", out)
	}

	// register bogus exits non-zero and lists known kinds
	out, err = exec.Command(bin, "agent", "register", "bogus").CombinedOutput()
	if err == nil {
		t.Fatalf("agent register bogus must fail: %s", out)
	}
	if !strings.Contains(string(out), "antigravity") {
		t.Fatalf("agent register bogus should list known kinds: %s", out)
	}
}

// TestCLIOrchestrateHelpAndDryRun smoke-tests the orchestrate command: --help
// surfaces the flags, and --dry-run on an inited project exits cleanly without
// launching any agent.
func TestCLIOrchestrateHelpAndDryRun(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	// Isolate the registry: init/orchestrate now auto-register, and tests must
	// not touch the developer's real ~/.pactify/projects.json.
	home := t.TempDir()
	run := func(env []string, args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Dir = dir
		c.Env = append(append(os.Environ(), "PACTIFY_HOME="+home), env...)
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

// TestCLIPlanApply smoke-tests the plan command: --help surfaces the planner
// flags, `plan apply --help` carries --run, and `plan apply <feature>` over a
// committed plan manifest + spec assigns its tasks. The smoke exercises only the
// apply path — launching a real planner agent needs a live LLM and is covered by
// integration acceptance, not this unit smoke.
func TestCLIPlanApply(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	// Isolate the registry: init/orchestrate now auto-register, and tests must
	// not touch the developer's real ~/.pactify/projects.json.
	home := t.TempDir()
	run := func(env []string, args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Dir = dir
		c.Env = append(append(os.Environ(), "PACTIFY_HOME="+home), env...)
		out, err := c.CombinedOutput()
		return string(out), err
	}

	planHelp, err := run(nil, "plan", "--help")
	if err != nil {
		t.Fatalf("plan --help: %v %s", err, planHelp)
	}
	for _, flag := range []string{"--feature", "--auto", "--run", "--planner-kind"} {
		if !strings.Contains(planHelp, flag) {
			t.Fatalf("plan --help missing %s:\n%s", flag, planHelp)
		}
	}

	applyHelp, err := run(nil, "plan", "apply", "--help")
	if err != nil {
		t.Fatalf("plan apply --help: %v %s", err, applyHelp)
	}
	if !strings.Contains(applyHelp, "--run") {
		t.Fatalf("plan apply --help missing --run:\n%s", applyHelp)
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

	env := []string{"PACT_AGENT_ID=claude"}
	if out, err := run(env, "init", "--project", "p", "--seat", "claude:orchestrator,reviewer:CLAUDE.md", "--seat", "w:worker:AGENTS.md"); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".pact", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pact", "tasks", "f1-t1.md"), []byte("# t1\n\nverify: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	planJSON := `{
  "feature": "f1",
  "branch": "feat-f1",
  "tasks": [
    {"id": "t1", "owner": "w", "reviewer": "claude", "spec": ".pact/tasks/f1-t1.md", "verify": "true", "deps": []}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, ".pact", "plan-f1.json"), []byte(planJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, err := run(env, "plan", "apply", "f1"); err != nil {
		t.Fatalf("plan apply f1: %v %s", err, out)
	}

	st, err := run(env, "status")
	if err != nil {
		t.Fatalf("status: %v %s", err, st)
	}
	if !strings.Contains(st, "id: t1") || !strings.Contains(st, "status: assigned") {
		t.Fatalf("plan apply did not assign t1:\n%s", st)
	}
}

func TestCLIRoleSetBindList(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	home := t.TempDir()
	run := func(args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "PACTIFY_HOME="+home)
		out, err := c.CombinedOutput()
		return string(out), err
	}

	if out, err := run("role", "set", "frontend", "--kind", "claude-code", "--model", "claude-opus-4-8"); err != nil {
		t.Fatalf("role set: %v %s", err, out)
	}
	if out, err := run("role", "bind", "w2", "frontend"); err != nil {
		t.Fatalf("role bind: %v %s", err, out)
	}
	out, err := run("role", "list")
	if err != nil {
		t.Fatalf("role list: %v %s", err, out)
	}
	if !strings.Contains(out, "frontend") || !strings.Contains(out, "claude-code") || !strings.Contains(out, "w2") {
		t.Fatalf("role list must show the profile and the binding:\n%s", out)
	}
	// Binding to an undefined role fails loudly.
	if out, err := run("role", "bind", "w3", "nope"); err == nil {
		t.Fatalf("binding an unknown role must fail: %s", out)
	}
}
