package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/gitx"
)

// StintRPC is the pact.stint payload the driver sends to a worker machine
// (mirrors the wire PactStintRequest minus machineId, which Dispatch adds).
type StintRPC struct {
	Project   string
	Task      string
	Seat      string
	AgentKind string
	Briefing  string
	Branch    string
}

// StintDispatch couriers a stint to a target machine. Prod: a relay socket emit
// (see cmd layer); tests: a fake that plays the worker machine.
type StintDispatch func(machineID string, stint StintRPC) error

// RemoteRunner routes each seat's stint to its host machine: seats with a host in
// Hosts run remotely (push → dispatch → poll origin for the task's checkpoint →
// merge it back); seats without a host run on the Local runner. The driver loop
// is unchanged — this is just a Runner, and a remote failure/timeout surfaces as
// the same soft failure a local agent crash does.
type RemoteRunner struct {
	Hosts    map[string]string // seat id → machineId ("" / absent = local)
	Local    Runner            // the usual CmdRunner
	Dispatch StintDispatch
	// Poll cadence + how long to wait for the remote checkpoint before declaring
	// the stint dead (the remote patrol equivalent). Zero values get defaults.
	PollInterval time.Duration
	Timeout      time.Duration
}

func (r *RemoteRunner) Run(ctx context.Context, lc LaunchContext) error {
	host := r.Hosts[lc.Seat]
	if host == "" {
		return r.Local.Run(ctx, lc)
	}
	if r.Dispatch == nil {
		return fmt.Errorf("remote seat %q: no dispatch configured", lc.Seat)
	}
	branch, err := gitx.CurrentBranch(lc.RepoDir)
	if err != nil || branch == "" {
		return fmt.Errorf("remote stint: resolve current branch: %v", err)
	}
	if !gitx.HasRemote(lc.RepoDir, "origin") {
		return fmt.Errorf("remote stint: project has no origin remote (multi-machine requires one)")
	}
	// Publish the driver's state (the assign + ledger) for the worker to fetch.
	if err := gitx.Push(lc.RepoDir, "origin", branch); err != nil {
		return fmt.Errorf("remote stint: push %s: %w", branch, err)
	}
	if err := r.Dispatch(host, StintRPC{
		Project: lc.Project, Task: lc.Task, Seat: lc.Seat,
		AgentKind: lc.Kind, Briefing: lc.Briefing, Branch: branch,
	}); err != nil {
		return fmt.Errorf("remote stint: dispatch to %s: %w", host, err)
	}

	// Completion signal (git = truth): poll origin for the task's checkpoint
	// event landing on the branch ledger, then merge it into the driver's tree
	// (the union merge driver folds concurrent ledger writes).
	interval := r.PollInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		_ = gitx.Fetch(lc.RepoDir, "origin", branch)
		if remoteLedgerHasCheckpoint(lc.RepoDir, "origin/"+branch, lc.Task) {
			if err := gitx.Merge(lc.RepoDir, "origin/"+branch); err != nil {
				return fmt.Errorf("remote stint: merge origin/%s: %w", branch, err)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("remote stint on %s: no checkpoint for %s within %s", host, lc.Task, timeout)
		}
	}
}

// remoteLedgerHasCheckpoint reads the ledger blob at ref (git show, no working-
// tree disturbance) and reports whether it contains a checkpoint for task.
func remoteLedgerHasCheckpoint(dir, ref, task string) bool {
	out, err := exec.Command("git", "-C", dir, "show", ref+":.pact/log.jsonl").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		var e struct {
			EventType string `json:"event_type"`
			TaskID    string `json:"task_id"`
		}
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.EventType == "checkpoint" && e.TaskID == task {
			return true
		}
	}
	return false
}
