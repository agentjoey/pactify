package orchestrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
)

// 2026-07-19 orchestrate e2e F2: with --run-timeout 1 an ACP worker stint ran
// 120s and was never killed. loop.launch wraps the runner call in
// context.WithTimeout, so AcpRunner.Run MUST return promptly once the ctx
// deadline passes even when the agent's Prompt never comes back — on BOTH
// watchdog configurations. These two tests pin each path.

// slowPrompt blocks until released — but honors Close, like the real client:
// acp.Client.call selects on the conn dying, and Spawn's CommandContext kills
// the child, so a real Prompt unblocks with an error once Close/kill lands.
func slowPrompt(release <-chan struct{}) func(*fakeAcpConn) (acp.StopReason, error) {
	return func(fc *fakeAcpConn) (acp.StopReason, error) {
		select {
		case <-release:
			return "end_turn", nil
		case <-fc.closed:
			return "", errors.New("acp: connection closed")
		}
	}
}

// wedgedPrompt never returns, no matter what — the pathological case: a child
// that survives the kill, or a transport read that never errors. The runner
// must STILL come back shortly after its deadline (bounded reap), because a
// Run that never returns turns --run-timeout into a no-op (e2e F2's failure
// shape: a stint outliving its deadline with no soft failure recorded).
func wedgedPrompt(block <-chan struct{}) func(*fakeAcpConn) (acp.StopReason, error) {
	return func(*fakeAcpConn) (acp.StopReason, error) {
		<-block
		return "end_turn", nil
	}
}

func runWithDeadline(t *testing.T, r AcpRunner) (time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := r.Run(ctx, LaunchContext{Seat: "w", Task: "t1", Kind: "kimi-cli", RepoDir: t.TempDir()})
	return time.Since(start), err
}

func TestAcpRunnerHonorsCtxDeadlineWithIdleWatchdog(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	fc := newFakeAcpConn()
	fc.prompt = slowPrompt(release)
	r := NewAcpRunner(time.Hour, PermissionAuto) // idle watchdog armed but far away
	r.Spawn = captureSpawn(fc, nil)
	elapsed, err := runWithDeadline(t, r)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want ctx deadline error, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("runner sat on a dead ctx for %v", elapsed)
	}
}

func TestAcpRunnerHonorsCtxDeadlineWithoutIdleWatchdog(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	fc := newFakeAcpConn()
	fc.prompt = slowPrompt(release)
	r := NewAcpRunner(0, PermissionAuto) // Idle<=0: the bare wait path
	r.Spawn = captureSpawn(fc, nil)
	elapsed, err := runWithDeadline(t, r)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want ctx deadline error, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("runner sat on a dead ctx for %v", elapsed)
	}
}

func TestAcpRunnerBoundedReapWhenPromptWedges(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	fc := newFakeAcpConn()
	fc.prompt = wedgedPrompt(block)
	r := NewAcpRunner(time.Hour, PermissionAuto)
	r.Spawn = captureSpawn(fc, nil)
	elapsed, err := runWithDeadline(t, r)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want ctx deadline error even when the prompt wedges, got %v", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("wedged prompt held Run for %v — reap must be bounded", elapsed)
	}
}
