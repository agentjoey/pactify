package sub

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

const malformedAgyCall = "Your previous response contained an improperly formatted function call: Malformed function call: Failed to parse function call: Function call is empty - no input to parse.\nPlease retry with a properly formatted function call\nRetries remaining: 3"

func TestAgyMalformedTerminalReplay(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	st, _ := store.Load("j1")
	st.Request.Harness = "agy"
	_ = store.Save(st)
	var turns atomic.Int32
	s.script = func(s *fakeSession) {
		if turns.Add(1) == 1 {
			s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "Previous task summary"}
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: malformedAgyCall}
		} else {
			s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "Verified final result"}
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
		}
	}
	h := newFakeHarness(b)
	h.TransientRetry = true
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return 0 }})
	if err != nil || r.Status != StatusSucceeded || len(r.Retries) != 1 || turns.Load() != 2 || r.FinalText != "Verified final result" || r.SessionID != "th-fake" {
		t.Fatalf("result=%+v turns=%d err=%v", r, turns.Load(), err)
	}
}

type slowFlashQuotaBackend struct {
	cockpit.Backend
	calls atomic.Int32
}

func (b *slowFlashQuotaBackend) Quota(ctx context.Context, _ cockpit.StartOpts) (cockpit.QuotaObservation, error) {
	if b.calls.Add(1) == 1 {
		select {
		case <-time.After(7 * time.Second):
		case <-ctx.Done():
			return cockpit.QuotaObservation{}, ctx.Err()
		}
	}
	return cockpit.QuotaObservation{ObservedAt: time.Now(), Buckets: []cockpit.QuotaBucket{{ID: "gemini-weekly", RemainingFraction: .5, ResetAt: time.Now().Add(time.Hour)}}}, nil
}

func TestAgySevenSecondOpeningQuotaDoesNotBlockPrompt(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	quota := &slowFlashQuotaBackend{Backend: b}
	h := newFakeHarness(b)
	h.Backend = func() cockpit.Backend { return quota }
	prompted := make(chan struct{})
	s.script = func(s *fakeSession) {
		close(prompted)
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "Actual result"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	done := make(chan Result, 1)
	go func() { r, _ := RunJob(context.Background(), store, "j1", h, WorkerConfig{}); done <- r }()
	select {
	case <-prompted:
	case <-time.After(time.Second):
		t.Error("opening quota blocked the prompt")
	}
	r := <-done
	if r.Status != StatusSucceeded || len(r.QuotaSnapshots) != 2 || contains(r.Warnings, "Quota snapshot unavailable") {
		t.Fatalf("result=%+v", r)
	}
	saved := readReviewResult(t, store, "j1")
	if len(saved.QuotaSnapshots) != 2 {
		t.Fatalf("saved=%+v", saved)
	}
}

func TestActiveToolUsesLongerIdleBudget(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "run_command"}}
		time.Sleep(1200 * time.Millisecond)
		s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "end", Name: "run_command"}}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{IdleTimeout: 500 * time.Millisecond})
	if err != nil || r.Status != StatusSucceeded || s.canceled.Load() {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestActiveToolIdleConfigurationAndCompletion(t *testing.T) {
	for _, mode := range []string{"active timeout", "completed tool", "overlapping tools"} {
		t.Run(mode, func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			t.Setenv("PACTIFY_SUB_ACTIVE_TOOL_IDLE_TIMEOUT", "1500ms")
			s.script = func(s *fakeSession) {
				s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "run_command"}}
				if mode == "overlapping tools" {
					s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "run_command"}}
				}
				if mode != "active timeout" {
					s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "end", Name: "run_command"}}
				}
				if mode == "overlapping tools" {
					time.Sleep(750 * time.Millisecond)
					s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "end", Name: "run_command"}}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r, err := RunJob(ctx, store, "j1", newFakeHarness(b), WorkerConfig{IdleTimeout: 500 * time.Millisecond})
			want := StatusFailed
			if mode == "overlapping tools" {
				want = StatusSucceeded
			}
			if err != nil || r.Status != want || want == StatusFailed && r.FailureClass != "idle_timeout" {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
}
