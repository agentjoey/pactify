package sub

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// Contract: every harness pactify can delegate to must keep its sessions out of the
// user's own session lists — either the backend hides them itself (codex archives
// the thread on Close) or the harness provides Conceal. A new harness without a
// strategy fails here instead of littering the user's apps (Owner, 2026-10-15).
func TestEveryHarnessHidesItsSessions(t *testing.T) {
	for _, name := range Names() {
		h, _ := Lookup(name)
		if !h.HidesItself && h.Conceal == nil {
			t.Errorf("harness %q has no session-hiding strategy (set HidesItself or Conceal)", name)
		}
	}
}

func TestRunJobConcealsTheSessionAfterTheJob(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	var got ConcealContext
	h := newFakeHarness(b)
	h.Conceal = func(c ConcealContext) error { got = c; return nil }
	before := time.Now()
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load("j1")
	if got.SessionID != "th-fake" || got.Cwd != st.Request.Cwd || got.Since.After(before.Add(time.Second)) || got.Since.IsZero() || !s.closed.Load() {
		t.Fatalf("conceal ctx = %+v (closed=%v)", got, s.closed.Load())
	}
	if r.Status != StatusSucceeded || len(r.Warnings) != 0 {
		t.Fatalf("status=%s warnings=%v", r.Status, r.Warnings)
	}
}

func TestRunJobConcealFailureIsAWarning(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	h := newFakeHarness(b)
	h.Conceal = func(ConcealContext) error { return errors.New("state.json: permission denied") }
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusSucceeded || !contains(r.Warnings, "permission denied") {
		t.Fatalf("status=%s warnings=%v", r.Status, r.Warnings)
	}
}
