package lockx

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// A second Acquire on the same path blocks while the first holder is live, and
// proceeds only after release — the mutual exclusion pact relies on to serialize
// base-branch merges across processes/worktrees.
func TestAcquireSerializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base.lock")

	release1, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// While held, a bounded second acquire must time out (it is blocked).
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); err == nil {
		t.Fatal("second acquire succeeded while the lock was held; want it blocked")
	}

	// After release, a fresh acquire succeeds promptly.
	release1()
	release2, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}

// A cancelled context aborts a blocked acquire without leaking.
func TestAcquireRespectsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base.lock")
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, path); err == nil {
		t.Fatal("acquire with a cancelled ctx should fail")
	}
}
