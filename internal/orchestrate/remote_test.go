package orchestrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// setupDriverWorker builds a bare origin + driver clone (on feat-f1 with a
// ledger) + worker clone. Returns (driver, worker) paths.
func setupDriverWorker(t *testing.T) (string, string) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	driver := filepath.Join(root, "driver")
	worker := filepath.Join(root, "worker")
	gitT(t, root, "init", "--bare", origin)
	gitT(t, root, "clone", origin, driver)
	gitT(t, driver, "commit", "--allow-empty", "-m", "init")
	gitT(t, driver, "push", "origin", "HEAD:main")
	gitT(t, driver, "checkout", "-b", "feat-f1")
	if err := os.MkdirAll(filepath.Join(driver, ".pact"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := `{"event_id":"e2","ts":"2026-01-01T00:01:00Z","agent_id":"o","role":"orchestrator","event_type":"assign","task_id":"t1","feature":"f1","payload":{"branch":"feat-f1","owner":"w","reviewer":"o","spec":"s.md"}}` + "\n"
	if err := os.WriteFile(filepath.Join(driver, ".pact", "log.jsonl"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, driver, "add", ".pact")
	gitT(t, driver, "commit", "-m", "assign t1")
	gitT(t, root, "clone", origin, worker)
	return driver, worker
}

// workerCheckpoint plays the worker machine: fetch the branch, append a
// checkpoint event to the ledger, commit, push. Error-returning (it runs in a
// goroutine, where t.Fatal is not allowed).
func workerCheckpoint(worker, branch, task string) error {
	gitQ := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = worker
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v\n%s", args, err, out)
		}
		return nil
	}
	if err := gitQ("fetch", "origin", branch); err != nil {
		return err
	}
	if err := gitQ("checkout", branch); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(worker, ".pact", "log.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	line := `{"event_id":"e3","ts":"2026-01-01T00:05:00Z","agent_id":"w","role":"worker","event_type":"checkpoint","task_id":"` + task + `","feature":"f1","payload":{"evidence":"done"}}` + "\n"
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if err := gitQ("add", ".pact"); err != nil {
		return err
	}
	if err := gitQ("commit", "-m", "pact "+task+": checkpoint by w"); err != nil {
		return err
	}
	return gitQ("push", "origin", branch)
}

func TestRemoteRunner_DispatchPollMerge(t *testing.T) {
	driver, worker := setupDriverWorker(t)

	dispatched := make(chan StintRPC, 1)
	rr := &RemoteRunner{
		Hosts: map[string]string{"w": "m-worker"},
		Dispatch: func(machineID string, s StintRPC) error {
			if machineID != "m-worker" {
				t.Errorf("dispatched to %q, want m-worker", machineID)
			}
			dispatched <- s
			// Play the worker machine asynchronously (fetch → checkpoint → push).
			go func() {
				if err := workerCheckpoint(worker, s.Branch, s.Task); err != nil {
					t.Errorf("worker checkpoint: %v", err)
				}
			}()
			return nil
		},
		PollInterval: 100 * time.Millisecond,
		Timeout:      10 * time.Second,
	}

	err := rr.Run(context.Background(), LaunchContext{
		Seat: "w", Kind: "kimi-cli", Task: "t1", Project: "demo",
		Briefing: "do t1", RepoDir: driver,
	})
	if err != nil {
		t.Fatalf("remote run failed: %v", err)
	}
	s := <-dispatched
	if s.Branch != "feat-f1" || s.Task != "t1" || s.AgentKind != "kimi-cli" {
		t.Fatalf("stint rpc wrong: %+v", s)
	}
	// The worker's checkpoint must now be IN THE DRIVER'S local ledger (merged).
	b, err := os.ReadFile(filepath.Join(driver, ".pact", "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"event_type":"checkpoint"`) {
		t.Fatalf("driver ledger missing the remote checkpoint:\n%s", b)
	}
}

func TestRemoteRunner_TimeoutIsSoftFailure(t *testing.T) {
	driver, _ := setupDriverWorker(t)
	rr := &RemoteRunner{
		Hosts:        map[string]string{"w": "m-worker"},
		Dispatch:     func(string, StintRPC) error { return nil }, // worker never responds
		PollInterval: 50 * time.Millisecond,
		Timeout:      300 * time.Millisecond,
	}
	err := rr.Run(context.Background(), LaunchContext{Seat: "w", Kind: "k", Task: "t1", Project: "demo", RepoDir: driver})
	if err == nil || !strings.Contains(err.Error(), "no checkpoint") {
		t.Fatalf("want checkpoint-timeout error, got %v", err)
	}
}

func TestRemoteRunner_LocalSeatFallsThrough(t *testing.T) {
	ran := false
	rr := &RemoteRunner{
		Hosts: map[string]string{"remote-seat": "m-x"},
		Local: runnerFunc(func(context.Context, LaunchContext) error { ran = true; return nil }),
	}
	if err := rr.Run(context.Background(), LaunchContext{Seat: "local-seat"}); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("local seat should use the Local runner")
	}
}

type runnerFunc func(context.Context, LaunchContext) error

func (f runnerFunc) Run(ctx context.Context, lc LaunchContext) error { return f(ctx, lc) }
