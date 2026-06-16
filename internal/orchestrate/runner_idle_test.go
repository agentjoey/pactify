package orchestrate

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A process that produces no output for longer than the idle window is killed,
// and the error is classified as an idle timeout (so the loop counts a soft
// failure and retries the worker rather than treating it as a hard error).
func TestOsExecIdle_KillsSilentProcess(t *testing.T) {
	exec := osExecIdle(150 * time.Millisecond)
	start := time.Now()
	err := exec(context.Background(), "sh", []string{"-c", "sleep 5"}, t.TempDir(), nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected idle-timeout error, got nil")
	}
	if !errors.Is(err, errIdle) {
		t.Fatalf("error = %v, want errIdle", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("idle kill took %s, expected ~150ms (watchdog not firing)", elapsed)
	}
}

// A process that keeps producing output within the idle window runs to
// completion — slow-but-alive is not killed (the whole point vs a blunt total
// timeout).
func TestOsExecIdle_KeepsChattyProcessAlive(t *testing.T) {
	exec := osExecIdle(400 * time.Millisecond)
	// prints every 80ms for ~640ms total — never idle longer than 80ms, but total
	// runtime exceeds the idle window, so a blunt total-timeout would kill it.
	err := exec(context.Background(), "sh", []string{"-c", "for i in 1 2 3 4 5 6 7 8; do echo tick; sleep 0.08; done"}, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("chatty process should complete, got %v", err)
	}
}

// A process that is OUTPUT-silent but keeps changing the working tree (writing
// files) is NOT killed — the patrol's second signal (fs progress) sees it's
// busy. This is the core "don't rigidly time out a working agent" behavior.
func TestOsExecIdle_QuietButWritingFilesNotKilled(t *testing.T) {
	dir := t.TempDir()
	exec := osExecIdle(200 * time.Millisecond)
	// no stdout (redirects to files), but writes a new file every 80ms for ~640ms
	// then exits 0 — output-idle exceeds 200ms, yet fsProgress stays < 200ms.
	script := "for i in 1 2 3 4 5 6 7 8; do echo x > f$i.txt; sleep 0.08; done"
	if err := exec(context.Background(), "sh", []string{"-c", script}, dir, nil); err != nil {
		t.Fatalf("quiet-but-writing agent should not be killed, got %v", err)
	}
}

// A process that is BOTH output-silent AND makes no file changes IS killed — the
// patrol confirms a true stall (this is the opencode-hang case).
func TestOsExecIdle_StalledNoProgressKilled(t *testing.T) {
	orig := fsProgress
	t.Cleanup(func() { fsProgress = orig })
	fsProgress = func(string) time.Duration { return time.Hour } // no progress
	exec := osExecIdle(150 * time.Millisecond)
	err := exec(context.Background(), "sh", []string{"-c", "sleep 5"}, t.TempDir(), nil)
	if !errors.Is(err, errIdle) {
		t.Fatalf("stalled process should be killed as idle, got %v", err)
	}
}

// idle=0 disables the watchdog and runs to completion like the plain exec.
func TestOsExecIdle_DisabledRunsToCompletion(t *testing.T) {
	exec := osExecIdle(0)
	if err := exec(context.Background(), "sh", []string{"-c", "echo hi"}, t.TempDir(), nil); err != nil {
		t.Fatalf("idle=0 should run plainly, got %v", err)
	}
}

// Parent-context cancellation propagates (and is distinguishable from an idle
// kill: it is NOT errIdle).
func TestOsExecIdle_ContextCancelPropagates(t *testing.T) {
	exec := osExecIdle(10 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	err := exec(ctx, "sh", []string{"-c", "sleep 5"}, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if errors.Is(err, errIdle) {
		t.Fatal("context cancel should not be classified as idle timeout")
	}
}
