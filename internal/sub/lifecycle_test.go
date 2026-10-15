package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/lockx"
)

type lifecycleBackend struct {
	start func(context.Context, cockpit.StartOpts) (cockpit.Session, error)
}

func (b lifecycleBackend) Start(ctx context.Context, o cockpit.StartOpts) (cockpit.Session, error) {
	return b.start(ctx, o)
}
func (b lifecycleBackend) Resume(ctx context.Context, _ string) (cockpit.Session, error) {
	return b.start(ctx, cockpit.StartOpts{})
}

type lifecycleSession struct {
	*fakeSession
	prompt    func(context.Context) error
	interrupt func() error
}

func (s *lifecycleSession) Prompt(ctx context.Context, _ cockpit.UserMessage) error {
	return s.prompt(ctx)
}
func (s *lifecycleSession) Interrupt(context.Context) error { return s.interrupt() }

func TestLifecycleCancelBeforePrompt(t *testing.T) {
	store, s, _ := workerFixture(t, "write")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var prompts atomic.Int32
	b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
		cancel()
		return &lifecycleSession{fakeSession: s, prompt: func(context.Context) error { prompts.Add(1); return nil }, interrupt: func() error {
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed}
			return nil
		}}, nil
	}}
	r, _ := RunJob(ctx, store, "j1", Harness{Backend: func() cockpit.Backend { return b }}, WorkerConfig{})
	if prompts.Load() != 0 || r.Status != StatusCanceled {
		t.Fatalf("prompts after cancel=%d status=%s", prompts.Load(), r.Status)
	}
}

func TestLifecycleCancelDeniesGracePeriodApproval(t *testing.T) {
	store, s, _ := workerFixture(t, "write")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	decided := make(chan cockpit.Decision, 1)
	b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
		return &lifecycleSession{fakeSession: s, prompt: func(context.Context) error { cancel(); return nil }, interrupt: func() error {
			s.approvals <- cockpit.ApprovalRequest{Respond: func(d cockpit.Decision) error {
				decided <- d
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed}
				return nil
			}}
			return nil
		}}, nil
	}}
	_, _ = RunJob(ctx, store, "j1", Harness{Backend: func() cockpit.Backend { return b }}, WorkerConfig{})
	select {
	case d := <-decided:
		if d != cockpit.DecisionDeny {
			t.Fatalf("approval after cancel=%s", d)
		}
	default:
		t.Fatal("approval was not denied")
	}
}

func TestLifecycleClaimSerializesWithQueuedCancel(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	release, err := lockx.Acquire(context.Background(), filepath.Join(store.Dir("j1"), "state.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	}()
	select {
	case <-done:
		t.Fatal("worker executed while cancellation held the job lock")
	case <-time.After(100 * time.Millisecond):
	}
	st, _ := store.Load("j1")
	st.Status = StatusCanceled
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("claim did not finish")
	}
	st, _ = store.Load("j1")
	if st.Status != StatusCanceled {
		t.Fatalf("canceled job became %s", st.Status)
	}
}

func TestLifecycleTerminalCancelIsNotOverwritten(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		st, _ := store.Load("j1")
		st.Status = StatusCanceled
		st.Error = "cancel won"
		data, _ := json.Marshal(Result{JobID: "j1", Status: StatusCanceled, FinalText: "cancel result"})
		if err := os.WriteFile(store.ResultPath("j1"), data, 0600); err != nil {
			t.Error(err)
		}
		if err := store.Save(st); err != nil {
			t.Error(err)
		}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	_, _ = RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	st, _ := store.Load("j1")
	data, _ := os.ReadFile(store.ResultPath("j1"))
	if st.Status != StatusCanceled || !contains([]string{string(data)}, "cancel result") {
		t.Fatalf("terminal canceled output overwritten: %s %s", st.Status, data)
	}
}

func TestLifecycleStartFailureConcealsKnownSession(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	var concealed string
	// A failed backend can identify an already-created session without returning a usable Session.
	b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) { return nil, &identifiedStartError{} }}
	h := Harness{Backend: func() cockpit.Backend { return b }, Conceal: func(c ConcealContext) error { concealed = c.SessionID; return nil }}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err == nil || concealed != "created-before-error" || r.SessionID != concealed {
		t.Fatalf("session=%q concealed=%q err=%v", r.SessionID, concealed, err)
	}
}

type identifiedStartError struct{}

func (*identifiedStartError) Error() string     { return "requested model unavailable" }
func (*identifiedStartError) SessionID() string { return "created-before-error" }

// ACP may publish a local turn_failed when Prompt's context expires, before its
// remote turn has actually stopped. Draining that event must not skip Interrupt.
func TestLifecycleLocalTurnFailureDoesNotSkipCancelInterrupt(t *testing.T) {
	store, s, _ := workerFixture(t, "write")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interrupted := make(chan struct{})
	var once sync.Once
	b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
		return &lifecycleSession{fakeSession: s, prompt: func(context.Context) error {
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed}
			return nil
		}, interrupt: func() error { once.Do(func() { close(interrupted) }); return nil }}, nil
	}}
	calls := 0
	now := func() time.Time {
		calls++
		if calls == 3 {
			cancel()
		}
		return time.Now()
	}
	r, _ := RunJob(ctx, store, "j1", Harness{Backend: func() cockpit.Backend { return b }}, WorkerConfig{Now: now})
	select {
	case <-interrupted:
	default:
		t.Fatalf("local failure skipped cancel interrupt; result=%s", r.Status)
	}
}

func TestLifecyclePanicRetainsWorkerFailureReason(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) { panic("startup crashed") }}
	func() {
		defer func() {
			if p := recover(); p != nil {
				// subWorker's outer panic handler also attempts fallback publication.
				r := Result{JobID: "j1", Status: StatusFailed, Warnings: []string{fmt.Sprint(p)}}
				_ = store.Finish("j1", &r, fmt.Sprint(p), time.Now())
			}
		}()
		_, _ = RunJob(context.Background(), store, "j1", Harness{Backend: func() cockpit.Backend { return b }}, WorkerConfig{})
	}()
	st, err := store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.ResultPath("j1"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusFailed || !strings.Contains(st.Error, "startup crashed") || !strings.Contains(string(data), "startup crashed") {
		t.Fatalf("panic cause lost: %+v %s", st, data)
	}
}

func TestLifecycleWorkerClockStillOwnsTerminalTimestamps(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	now := time.Date(2026, 10, 15, 1, 2, 3, 0, time.UTC)
	_, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	if !st.FinishedAt.Equal(now) || !st.UpdatedAt.Equal(now) {
		t.Fatalf("worker clock was bypassed: finished=%s updated=%s", st.FinishedAt, st.UpdatedAt)
	}
}
