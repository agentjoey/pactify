package cockpit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func dshReviewSession(t *testing.T, mode string) *dshSession {
	t.Helper()
	dshFixture(t, mode)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	s, err := NewDSHBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s.(*dshSession)
}
func TestDSHBackpressureDoesNotLockState(t *testing.T) {
	s := dshReviewSession(t, "ok")
	for i := 0; i < cap(s.events); i++ {
		s.events <- Event{Kind: EventSystem}
	}
	finished := make(chan struct{})
	go func() {
		s.notification(json.RawMessage(`{"sessionId":"root","event":{"type":"assistant/message","data":{"usage":{"inputTokens":1}}}}`))
		close(finished)
	}()
	// The notification cannot finish its emit while the event queue is full.
	select {
	case <-finished:
		t.Fatal("notification unexpectedly finished")
	case <-time.After(50 * time.Millisecond):
	}
	probes := make(chan string, 3)
	go func() { s.Info(); probes <- "Info" }()
	go func() { s.DSHUsage(); probes <- "DSHUsage" }()
	go func() { _ = s.Interrupt(context.Background()); probes <- "Interrupt" }()
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	received := 0
	for received < 3 {
		select {
		case <-probes:
			received++
		case <-timer.C:
			// Release the old implementation's held lock before cleanup, even on failure.
			<-s.events
			<-finished
			t.Fatalf("only %d/3 state operations returned under backpressure", received)
		}
	}
	<-s.events
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("notification did not resume")
	}
}
func TestDSHCacheInputAccounting(t *testing.T) {
	s := dshReviewSession(t, "ok")
	s.notification(json.RawMessage(`{"sessionId":"root","event":{"type":"assistant/message","data":{"usage":{"inputTokens":4939,"cacheReadTokens":107392,"cacheWriteTokens":11,"outputTokens":3,"totalTokens":112345}}}}`))
	e := <-s.Events()
	r := s.DSHUsage()
	if e.Usage == nil || e.Usage.InputTokens != 112342 || r.InputTokens != 112342 {
		t.Fatalf("usage=%+v record=%+v", e.Usage, r)
	}
}
func TestDSHServerRequest(t *testing.T) {
	s := dshReviewSession(t, "server-request")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Prompt(ctx, UserMessage{Text: "continue"}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case e := <-s.Events():
			if e.State == StateTurnFailed {
				t.Fatalf("turn failed: %s", e.Err)
			}
			if e.State == StateTurnCompleted {
				return
			}
		case <-ctx.Done():
			t.Fatal("server request was not answered")
		}
	}
}
func TestDSHErrorDiagnostics(t *testing.T) {
	for _, reason := range []string{
		`{"kind":"error","code":"insufficient_balance","message":"balance exhausted opaque-fixture-key"}`,
		`{"kind":"error","error":{"code":401,"message":"invalid key opaque-fixture-key"}}`,
		`{"kind":"aborted","details":{"code":"stopped","message":"user stopped opaque-fixture-key"}}`,
	} {
		t.Run(reason, func(t *testing.T) {
			t.Setenv("DSH_TEST_REASON", reason)
			t.Setenv("DEEPSEEK_API_KEY", "opaque-fixture-key")
			s := dshReviewSession(t, "error")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := s.Prompt(ctx, UserMessage{Text: "fail"}); err != nil {
				t.Fatal(err)
			}
			for {
				select {
				case e := <-s.Events():
					if e.State == StateTurnCompleted {
						t.Fatal("error completed")
					}
					if e.State == StateTurnFailed {
						var want map[string]any
						_ = json.Unmarshal([]byte(reason), &want)
						code, message := "insufficient_balance", "balance exhausted"
						if strings.Contains(reason, "401") {
							code, message = "401", "invalid key"
						}
						if strings.Contains(reason, "aborted") {
							code, message = "stopped", "user stopped"
						}
						if !strings.Contains(e.Err, code) || !strings.Contains(e.Err, message) || strings.Contains(e.Err, "opaque-fixture-key") || !strings.Contains(e.Err, "[REDACTED]") {
							t.Fatalf("diagnostic: %s", e.Err)
						}
						if want["kind"] == "aborted" && !strings.Contains(e.Err, "aborted") {
							t.Fatal(e.Err)
						}
						return
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
		})
	}
}
func TestDSHRPCErrorMessage(t *testing.T) {
	t.Setenv("DSH_TEST_RPC_ERROR", "request refused opaque-fixture-key")
	t.Setenv("DEEPSEEK_API_KEY", "opaque-fixture-key")
	s := dshReviewSession(t, "ok")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := s.call(ctx, "session/query", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "request refused") || strings.Contains(err.Error(), "opaque-fixture-key") {
		t.Fatalf("RPC error=%v", err)
	}
}

func TestDSHServerRequestWriteDeadline(t *testing.T) {
	s := dshReviewSession(t, "server-stalled")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	if err := s.Prompt(ctx, UserMessage{Text: "test stalled stdin"}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case e := <-s.Events():
			if e.State == StateTurnCompleted {
				t.Fatal("unanswered oversized server request was ignored")
			}
			if e.State == StateTurnFailed {
				if time.Since(started) > 2500*time.Millisecond {
					t.Fatal("reader waited too long")
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("reader blocked on SDK response write")
		}
	}
}

func TestDSHEmptyMethodRequest(t *testing.T) {
	t.Setenv("DSH_TEST_REQUEST_METHOD", "")
	TestDSHServerRequest(t)
}
