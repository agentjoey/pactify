package orchestrate

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// A returning driver must not leave a heartbeat writing into a worktree that
// its caller is now free to remove. Hold a real heartbeat inside its timestamp
// callback while the agent cancels the run.
func TestRunWaitsForInFlightHeartbeat(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat/x", spec)
	previous := statusHeartbeatInterval
	statusHeartbeatInterval = time.Millisecond
	defer func() { statusHeartbeatInterval = previous }()

	entered, release := make(chan struct{}), make(chan struct{})
	var hold atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := runnerFunc(func(context.Context, LaunchContext) error {
		hold.Store(true)
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			return context.DeadlineExceeded
		}
		cancel()
		return ctx.Err()
	})
	opts := baseOpts(dir, runner, &okExec{}, &recNotify{})
	opts.Now = func() string {
		if hold.Swap(false) {
			close(entered)
			<-release
		}
		return fixedNow()
	}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opts) }()
	defer func() {
		close(release)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("driver did not stop after heartbeat was released")
		}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("heartbeat did not enter timestamp callback")
	}
	select {
	case err := <-done:
		done <- err // leave teardown able to join on the same return
		t.Fatalf("Run returned while its heartbeat was still writing: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}
