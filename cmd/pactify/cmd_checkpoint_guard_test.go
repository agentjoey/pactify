package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

// The UI-GATE incident: a human checkpoints t1 while a driver is mid-stint on
// another task, and Checkpoint's CommitAll sweeps that run's in-flight files
// into t1's commit.
func TestCheckpointRefusedWhileAnotherTaskIsBeingDriven(t *testing.T) {
	dir, run := checkpointRepo(t)
	writeRunStatus(t, dir, `{"feature":"m4","task":"m4-s11","seat":"kimi-worker","done":false,"escalated":false,"updated_at":"`+nowStamp()+`"}`)

	out, err := run("checkpoint", "t1", "--evidence", "ok")

	if err == nil {
		t.Fatalf("checkpoint must fail while another task is being driven; got success:\n%s", out)
	}
	if !strings.Contains(out, "m4-s11") {
		t.Errorf("error must name the running task so the human knows what is in flight, got:\n%s", out)
	}
	if !strings.Contains(out, "--force") {
		t.Errorf("error must state the escape hatch, got:\n%s", out)
	}
	log, _ := os.ReadFile(filepath.Join(dir, ".pact", "log.jsonl"))
	if strings.Contains(string(log), `"checkpoint"`) {
		t.Errorf("refused checkpoint must not reach the ledger:\n%s", log)
	}
}

// The escape hatch stays open: the guard is about not surprising a human, not
// about making a legitimate manual checkpoint impossible.
func TestCheckpointForceOverridesTheGuard(t *testing.T) {
	dir, run := checkpointRepo(t)
	writeRunStatus(t, dir, `{"feature":"m4","task":"m4-s11","done":false,"escalated":false,"updated_at":"`+nowStamp()+`"}`)

	if out, err := run("checkpoint", "t1", "--evidence", "ok", "--force"); err != nil {
		t.Fatalf("--force must go through: %v\n%s", err, out)
	}
	log, _ := os.ReadFile(filepath.Join(dir, ".pact", "log.jsonl"))
	if !strings.Contains(string(log), `"checkpoint"`) {
		t.Errorf("forced checkpoint must reach the ledger:\n%s", log)
	}
}

// Load-bearing: the briefing tells every worker to finish with
// `pactify checkpoint <task>`, so the run's own task must never be blocked —
// otherwise this guard breaks every orchestrated handoff.
func TestCheckpointOfTheDrivenTaskStillWorks(t *testing.T) {
	dir, run := checkpointRepo(t)
	writeRunStatus(t, dir, `{"feature":"f","task":"t1","seat":"opencode","done":false,"escalated":false,"updated_at":"`+nowStamp()+`"}`)

	if out, err := run("checkpoint", "t1", "--evidence", "ok"); err != nil {
		t.Fatalf("worker checkpoint of the task under drive must pass: %v\n%s", err, out)
	}
}

// A crashed driver leaves done=false forever; it must not wedge the repo shut.
func TestCheckpointNotBlockedByStaleRun(t *testing.T) {
	dir, run := checkpointRepo(t)
	stale := time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339)
	writeRunStatus(t, dir, `{"task":"m4-s11","done":false,"escalated":false,"updated_at":"`+stale+`"}`)

	if out, err := run("checkpoint", "t1", "--evidence", "ok"); err != nil {
		t.Fatalf("stale run must not block a manual checkpoint: %v\n%s", err, out)
	}
}
