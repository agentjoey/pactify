package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

// A harness running inside a job must not delegate again: the worker exports
// PACTIFY_JOB to every harness process.
func TestSubRunRefusedInsideJob(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	t.Setenv("PACTIFY_SUB_FAKE_CODEX", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	store, _ := sub.DefaultStore()
	if _, err := store.CreateRun("previous", sub.Request{Harness: "codex", Cwd: repo, Access: "write"}, true, false); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("previous", &sub.Result{Status: sub.StatusSucceeded, SessionID: "session"}, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	worktrees := func() string {
		out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	beforeJobs, _ := os.ReadDir(store.Root)
	beforeWT := worktrees()
	t.Setenv("PACTIFY_JOB", "outer-job")
	for _, args := range [][]string{
		{"sub", "run", "codex", "--wait=0", "--json"},
		{"sub", "run", "codex", "--worktree", "--wait=0", "--json"},
		{"sub", "run", "--resume=previous", "--worktree", "--wait=0", "--json"},
		{"sub", "run", "codex", "--worktree-from", "previous", "--wait=0", "--json"},
	} {
		out, code := runCLI(t, bin, repo, "brief", args...)
		if code != 2 || !strings.Contains(out, "inside a pactify job") {
			t.Fatalf("%v: code=%d out=%s", args, code, out)
		}
	}
	afterJobs, _ := os.ReadDir(store.Root)
	if len(afterJobs) != len(beforeJobs) || worktrees() != beforeWT {
		t.Fatalf("refused runs created state: jobs %d -> %d, worktrees changed=%v", len(beforeJobs), len(afterJobs), worktrees() != beforeWT)
	}
	if out, code := runCLI(t, bin, repo, "", "sub", "doctor", "codex"); code != 2 || !strings.Contains(out, "inside a pactify job") {
		t.Fatalf("doctor inside a job: code=%d out=%s", code, out)
	}
	// Read-only commands stay available inside a job.
	if out, code := runCLI(t, bin, repo, "", "sub", "status", "previous"); code != 0 {
		t.Fatalf("status inside a job: code=%d out=%s", code, out)
	}
	// An empty marker, as set for agy quota probes, is not a job.
	t.Setenv("PACTIFY_JOB", "")
	if out, code := runCLI(t, bin, repo, "brief", "sub", "run", "codex", "--wait=10s", "--json"); code != 0 {
		t.Fatalf("empty PACTIFY_JOB blocked a run: code=%d out=%s", code, out)
	}
}
