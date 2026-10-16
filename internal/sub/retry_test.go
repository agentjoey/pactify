package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestTransientRetry(t *testing.T) {
	for _, tc := range []struct {
		name, failure                string
		retries, failures, wantTurns int
		wantStatus                   Status
		wantClass                    string
	}{
		{"success", "503 UNAVAILABLE", 2, 1, 2, StatusSucceeded, ""},
		{"default", "503 UNAVAILABLE", -1, 9, 3, StatusFailed, "transient"},
		{"maximum", "503 UNAVAILABLE", 5, 9, 6, StatusFailed, "transient"},
		{"exhausted", "503 UNAVAILABLE", 2, 9, 3, StatusFailed, "transient"},
		{"disabled", "503 UNAVAILABLE", 0, 1, 1, StatusFailed, "transient"},
		{"quota", "usage limit", 2, 1, 1, StatusFailed, "quota"},
		{"auth", "unauthorized", 2, 1, 1, StatusFailed, "auth"},
		{"model", "requested model is not offered by this agent", 2, 1, 1, StatusFailed, "model"},
		{"canceled", "Task canceled", 2, 1, 1, StatusFailed, "canceled"},
		{"other", "broken", 2, 1, 1, StatusFailed, "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			st, _ := store.Load("j1")
			st.Request.Harness = "codex"
			if tc.retries >= 0 {
				st.Request.Retries = &tc.retries
			}
			_ = store.Save(st)
			var turns atomic.Int32
			s.script = func(s *fakeSession) {
				n := int(turns.Add(1))
				s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: n * 10, OutputTokens: n, CostUSD: float64(n) / 10}, Raw: json.RawMessage(fmt.Sprintf(`{"threadId":"main","tokenUsage":{"total":{"inputTokens":%d,"outputTokens":%d}}}`, n*10, n))}
				s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: fmt.Sprintf("turn %d", n)}
				e := cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				if n <= tc.failures {
					e.State = cockpit.StateTurnFailed
					e.Err = tc.failure
					if tc.failure == "503 UNAVAILABLE" {
						e.Raw = json.RawMessage(`{"error":{"codexErrorInfo":"internalServerError"}}`)
					}
				}
				s.events <- e
			}
			var delays []time.Duration
			h := newFakeHarness(b)
			h.TransientRetry = true
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{RetryBackoff: func(n int) time.Duration { delays = append(delays, retryBackoff(n)); return 0 }})
			if err != nil || r.Status != tc.wantStatus || r.FailureClass != tc.wantClass || int(turns.Load()) != tc.wantTurns || len(r.Retries) != tc.wantTurns-1 || r.FinalText != fmt.Sprintf("turn %d", tc.wantTurns) || r.InputTokens != tc.wantTurns*10 || r.OutputTokens != tc.wantTurns {
				t.Fatalf("result=%+v turns=%d err=%v", r, turns.Load(), err)
			}
			for i, retry := range r.Retries {
				if retry.Class != "transient" || retry.Error != tc.failure || retry.At.IsZero() {
					t.Fatalf("retry=%+v", retry)
				}
				want := 30 * time.Second
				if i > 0 {
					want = 120 * time.Second
				}
				if delays[i] != want {
					t.Fatalf("delay=%s", delays[i])
				}
			}
			if len(r.Retries) > 0 && !strings.Contains(r.Markdown(), fmt.Sprintf("Retries: %d", len(r.Retries))) {
				t.Fatal(r.Markdown())
			}
		})
	}
}

func TestRetryBackoffCancellation(t *testing.T) {
	for _, viaContext := range []bool{false, true} {
		t.Run(fmt.Sprint(viaContext), func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			st, _ := store.Load("j1")
			st.Request.Harness = "codex"
			_ = store.Save(st)
			var turns atomic.Int32
			s.script = func(s *fakeSession) {
				turns.Add(1)
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "503 UNAVAILABLE", Raw: json.RawMessage(`{"error":{"codexErrorInfo":"internalServerError"}}`)}
			}
			h := newFakeHarness(b)
			h.TransientRetry = true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			waiting := make(chan struct{})
			done := make(chan Result, 1)
			go func() {
				r, _ := RunJob(ctx, store, "j1", h, WorkerConfig{RetryBackoff: func(int) time.Duration { close(waiting); return time.Hour }})
				done <- r
			}()
			select {
			case <-waiting:
			case <-time.After(3 * time.Second):
				t.Fatal("did not retry")
			}
			st, _ = store.Load("j1")
			row := store.Summarize(st, nil, time.Now())
			if row.RetryAttempt != 1 || row.RetryLimit != 2 || row.Status != StatusRunning {
				t.Fatalf("summary=%+v", row)
			}
			if viaContext {
				cancel()
			} else {
				if _, err := store.RequestCancel("j1"); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case r := <-done:
				if r.Status != StatusCanceled || r.FailureClass != "canceled" || len(r.Retries) != 0 || turns.Load() != 1 {
					t.Fatalf("result=%+v turns=%d", r, turns.Load())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("backoff ignored cancellation")
			}
		})
	}
}

func TestTransientRetryUnsupported(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	st, _ := store.Load("j1")
	st.Request.Harness = "codex"
	_ = store.Save(st)
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "503 UNAVAILABLE", Raw: json.RawMessage(`{"error":{"codexErrorInfo":"internalServerError"}}`)}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil || r.FailureClass != "transient" || len(r.Retries) != 0 || !contains(r.Warnings, "transient retry unsupported for codex") {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestRetryCostAndSummary(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	r := Result{Status: StatusSucceeded, Retries: []Retry{{Class: "transient", Error: "EOF", At: time.Now()}, {Class: "transient", Error: "EOF", At: time.Now()}}}
	if err := store.Finish("j1", &r, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load("j1")
	row := store.Summarize(st, nil, time.Now())
	if row.RetryCount != 2 {
		t.Fatalf("summary=%+v", row)
	}
	report, err := store.CostReport(time.Now().Add(-time.Hour), "harness", time.Now())
	if err != nil || report.Total.Retries != 1 || report.Groups[0].Retries != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestRetryStatusText(t *testing.T) {
	for _, tc := range []struct {
		row  JobSummary
		want string
	}{
		{JobSummary{Status: StatusRunning, RetryAttempt: 1, RetryLimit: 2}, "retrying (1/2)"},
		{JobSummary{Status: StatusRunning, RetryAttempt: 2, RetryLimit: 2, RetryCount: 1}, "retrying (2/2)"},
		{JobSummary{Status: StatusSucceeded, RetryCount: 2}, "succeeded (retries: 2)"},
	} {
		if got := tc.row.StatusText(time.Now()); got != tc.want {
			t.Fatalf("got=%q want=%q", got, tc.want)
		}
	}
}

func TestCollectorRetryUsage(t *testing.T) {
	for _, name := range []string{"codex", "agy", "opencode", "kimi"} {
		t.Run(name, func(t *testing.T) {
			var c Collector
			c.Add(cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 10, OutputTokens: 1, CostUSD: 0.25}})
			c.Add(cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "old"})
			c.Add(cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "EOF"})
			c.Add(cockpit.Event{Kind: cockpit.EventSystem, State: "transient_retry", Text: name})
			if c.TurnDone || c.FailureMessage != "" || c.FinalText() != "" {
				t.Fatalf("stale turn: %+v", c)
			}
			u := cockpit.Usage{InputTokens: 30, OutputTokens: 3, CostUSD: 0.75}
			if name == "kimi" {
				u = cockpit.Usage{InputTokens: 20, OutputTokens: 2, CostUSD: 0.5}
			}
			c.Add(cockpit.Event{Kind: cockpit.EventUsage, Usage: &u})
			if c.Usage.InputTokens != 30 || c.Usage.OutputTokens != 3 || c.Usage.CostUSD != 0.75 {
				t.Fatalf("usage=%+v", c.Usage)
			}
		})
	}
}

func TestRetryPromptFailureDoesNotInterruptNextTurn(t *testing.T) {
	for _, terminal := range []bool{true, false} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			store, s, _ := workerFixture(t, "write")
			st, _ := store.Load("j1")
			st.Request.Harness = "agy"
			_ = store.Save(st)
			var turns atomic.Int32
			b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
				return &lifecycleSession{fakeSession: s, prompt: func(context.Context) error {
					n := turns.Add(1)
					if n == 1 {
						if terminal {
							s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "UNAVAILABLE (code 503)"}
						}
						return fmt.Errorf("UNAVAILABLE (code 503)")
					}
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "recovered"}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
					return nil
				}, interrupt: func() error { return s.Interrupt(context.Background()) }}, nil
			}}
			h := newFakeHarness(nil)
			h.Backend = func() cockpit.Backend { return b }
			h.TransientRetry = true
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return 0 }})
			if err != nil || r.Status != StatusSucceeded || r.FinalText != "recovered" || len(r.Retries) != 1 || s.canceled.Load() {
				t.Fatalf("result=%+v interrupted=%v err=%v", r, s.canceled.Load(), err)
			}
		})
	}
}

type retrySession struct {
	*fakeSession
	prompt func(context.Context, cockpit.UserMessage) error
	id     string
}

func (s *retrySession) Prompt(ctx context.Context, m cockpit.UserMessage) error {
	return s.prompt(ctx, m)
}
func (s *retrySession) ThreadID() string { return s.id }

func TestRetryKeepsSessionPromptAndLastFailure(t *testing.T) {
	for _, last := range []string{"", "Individual quota reached", "another failure"} {
		t.Run(last, func(t *testing.T) {
			store, s, _ := workerFixture(t, "write")
			st, _ := store.Load("j1")
			st.Request.Harness = "agy"
			st.Request.Model = "chosen"
			st.Request.Effort = "low"
			_ = store.Save(st)
			var starts, turns int
			b := lifecycleBackend{start: func(_ context.Context, opts cockpit.StartOpts) (cockpit.Session, error) {
				starts++
				if opts.Model != "chosen" || opts.Effort != "low" {
					t.Errorf("opts=%+v", opts)
				}
				return &retrySession{fakeSession: s, id: "same-session", prompt: func(_ context.Context, m cockpit.UserMessage) error {
					turns++
					want := st.Request.Brief
					if turns > 1 {
						want = retryPrompt
						current, _ := store.Load("j1")
						row := store.Summarize(current, nil, time.Now())
						if row.StatusText(time.Now()) != "retrying (1/2)" {
							t.Errorf("summary=%+v", row)
						}
					}
					if m.Text != want {
						t.Errorf("prompt=%q want=%q", m.Text, want)
					}
					e := cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "UNAVAILABLE (code 503)"}
					s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: turns * 10, OutputTokens: turns, CostUSD: float64(turns) / 4}}
					if turns > 1 {
						e.Err = last
						if last == "" {
							e.State = cockpit.StateTurnCompleted
						}
						s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "last reply"}
					}
					s.events <- e
					return nil
				}}, nil
			}}
			h := newFakeHarness(nil)
			h.Backend = func() cockpit.Backend { return b }
			h.TransientRetry = true
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return 0 }})
			if last != "" {
				current, _ := store.Load("j1")
				if current.Error != last {
					t.Errorf("final state error=%q want=%q", current.Error, last)
				}
			}
			wantClass := map[string]string{"": "", "Individual quota reached": "quota", "another failure": "other", "Task canceled": "canceled"}[last]
			if err != nil || r.FailureClass != wantClass || starts != 1 || turns != 2 || len(r.Retries) != 1 || r.SessionID != "same-session" || r.FinalText != "last reply" || r.InputTokens != 20 || r.OutputTokens != 2 || r.CostUSD != 0.5 {
				t.Fatalf("result=%+v starts=%d turns=%d err=%v", r, starts, turns, err)
			}
		})
	}
}

func TestRetryWithoutSessionID(t *testing.T) {
	store, s, _ := workerFixture(t, "write")
	st, _ := store.Load("j1")
	st.Request.Harness = "agy"
	_ = store.Save(st)
	b := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
		return &retrySession{fakeSession: s, prompt: func(context.Context, cockpit.UserMessage) error {
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "UNAVAILABLE (code 503)"}
			return nil
		}}, nil
	}}
	h := newFakeHarness(nil)
	h.Backend = func() cockpit.Backend { return b }
	h.TransientRetry = true
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || r.FailureClass != "transient" || len(r.Retries) != 0 || !contains(r.Warnings, "transient retry unsupported for agy") {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestRetryDoesNotOverrideIdleOrCancel(t *testing.T) {
	for _, cancelJob := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelJob), func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			st, _ := store.Load("j1")
			st.Request.Harness = "agy"
			_ = store.Save(st)
			s.script = func(*fakeSession) {}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelJob {
				timer := time.AfterFunc(20*time.Millisecond, cancel)
				defer timer.Stop()
			}
			h := newFakeHarness(b)
			h.TransientRetry = true
			r, err := RunJob(ctx, store, "j1", h, WorkerConfig{IdleTimeout: 50 * time.Millisecond, RetryBackoff: func(int) time.Duration { t.Error("must not retry"); return 0 }})
			want := "idle_timeout"
			if cancelJob {
				want = "canceled"
			}
			if err != nil || r.FailureClass != want || len(r.Retries) != 0 {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
}
