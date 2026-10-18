package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// checkpointRepo builds a project with one assigned task (t1, owned by
// opencode) and returns dir plus a runner bound to the worker seat.
func checkpointRepo(t *testing.T) (string, func(args ...string) (string, error)) {
	t.Helper()
	bin := buildBinary(t)
	dir := t.TempDir()
	home := t.TempDir()

	run := func(args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"PACTIFY_HOME="+home,
			"PACTIFY_ALLOW_TEMP_REGISTER=1",
			"PACT_AGENT_ID=opencode",
		)
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

	orch := exec.Command(bin, "init", "--project", "p",
		"--seat", "claude-opus:orchestrator,reviewer:CLAUDE.md",
		"--seat", "opencode:worker:AGENTS.md")
	orch.Dir = dir
	orch.Env = append(os.Environ(), "PACTIFY_HOME="+home, "PACTIFY_ALLOW_TEMP_REGISTER=1", "PACT_AGENT_ID=claude-opus")
	if out, err := orch.CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	as := exec.Command(bin, "assign", "t1", "--feature", "f", "--branch", "feat/x",
		"--owner", "opencode", "--reviewer", "claude-opus", "--spec", ".pact/tasks/t1.md")
	as.Dir = dir
	as.Env = append(os.Environ(), "PACTIFY_HOME="+home, "PACTIFY_ALLOW_TEMP_REGISTER=1", "PACT_AGENT_ID=claude-opus")
	if out, err := as.CombinedOutput(); err != nil {
		t.Fatalf("assign: %v %s", err, out)
	}
	if out, err := run("join", "opencode", "--roles", "worker", "--task", "t1"); err != nil {
		t.Fatalf("join: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("c"), 0o644)
	return dir, run
}

func writeRunStatus(t *testing.T, dir, body string) {
	t.Helper()
	p := filepath.Join(dir, ".pact", "orchestrate", "status.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
}

// Runtime files left by the removed orchestrator cannot block pact checkpoints.
func TestCheckpointIgnoresLegacyRunStatus(t *testing.T) {
	dir, run := checkpointRepo(t)
	writeRunStatus(t, dir, `{"task":"another-task","done":false,"escalated":false,"updated_at":"2099-01-01T00:00:00Z"}`)
	if out, err := run("checkpoint", "t1", "--evidence", "ok"); err != nil {
		t.Fatalf("legacy status must not block checkpoint: %v\n%s", err, out)
	}
	log, err := os.ReadFile(filepath.Join(dir, ".pact", "log.jsonl"))
	if err != nil || !strings.Contains(string(log), `"checkpoint"`) {
		t.Fatalf("checkpoint must reach the ledger: %v", err)
	}
}
