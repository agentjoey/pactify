package cockpit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
)

func TestACPStopReasonsV2(t *testing.T) {
	for _, kind := range []string{"kimi-cli", "opencode"} {
		for _, stop := range []string{"end_turn", "cancelled", "refusal", "max_tokens", "max_turn_requests"} {
			t.Run(kind+"/"+stop, func(t *testing.T) {
				s := newACPSession(nil, "sid")
				defer s.Close()
				s.promptFn = func(context.Context, string) (acp.StopReason, error) { return acp.StopReason(stop), nil }
				if err := s.Prompt(context.Background(), UserMessage{Text: "test"}); err != nil {
					t.Fatal(err)
				}
				var terminal Event
				for i := 0; i < len(s.events); {
					e := <-s.events
					if e.Kind == EventState {
						terminal = e
					}
				}
				want := StateTurnFailed
				if stop == "end_turn" {
					want = StateTurnCompleted
				}
				if terminal.State != want {
					t.Fatalf("stop=%s event=%+v", stop, terminal)
				}
				if want == StateTurnFailed {
					wantClass := stop
					if stop == "cancelled" {
						wantClass = "canceled"
					}
					harness := "kimi"
					if kind == "opencode" {
						harness = kind
					}
					if got := ClassifyFailure(harness, terminal.Err, terminal.Raw, time.Now()); got.Class != wantClass {
						t.Fatalf("class=%+v want=%s", got, wantClass)
					}
				}
			})
		}
	}
}

func TestHarnessBackpressureV2(t *testing.T) {
	for _, backend := range []string{"acp", "codex"} {
		t.Run(backend, func(t *testing.T) {
			var events <-chan Event
			var send func(Event)
			var closeSession func() error
			if backend == "acp" {
				s := newACPSession(nil, "sid")
				events = s.Events()
				send = s.dispatchEvent
				closeSession = s.Close
			} else {
				s := &codexSession{events: make(chan Event, 64), approvals: make(chan ApprovalRequest), done: make(chan struct{})}
				events = s.Events()
				send = s.dispatchEvent
				closeSession = s.Close
			}
			defer closeSession()
			done := make(chan struct{})
			go func() {
				defer close(done)
				for i := 0; i < 10000; i++ {
					send(Event{Kind: EventMessage, Text: "字a"})
					if i%200 == 0 {
						send(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start"}})
					}
				}
				send(Event{Kind: EventState, State: StateTurnCompleted})
			}()
			time.Sleep(20 * time.Millisecond)
			var text strings.Builder
			tools := 0
			timer := time.After(10 * time.Second)
		loop:
			for {
				select {
				case e := <-events:
					if e.Kind == EventMessage {
						text.WriteString(e.Text)
					}
					if e.Tool != nil && e.Tool.Phase == "start" {
						tools++
					}
					if e.State == StateTurnCompleted {
						break loop
					}
				case <-timer:
					t.Fatal("producer stuck")
				}
			}
			<-done
			if text.String() != strings.Repeat("字a", 10000) || tools != 50 {
				t.Fatalf("bytes=%d tools=%d", text.Len(), tools)
			}
		})
	}
}

func TestOpencodeFailureEvidenceV2(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"name":"APIError","data":{"statusCode":503,"isRetryable":false,"message":"unavailable"}}`, "transient"},
		{`{"name":"APIError","data":{"statusCode":400,"isRetryable":true}}`, "transient"},
		{`{"name":"APIError","data":{"statusCode":429,"isRetryable":true,"message":"Insufficient balance"}}`, "quota"},
		{`{"name":"APIError","data":{"statusCode":429,"message":"rate limited"}}`, "transient"},
		{`{"name":"UnknownError","data":{"message":"Model not found"}}`, "other"},
	} {
		if got := ClassifyFailure("opencode", "", json.RawMessage(tc.raw), time.Now()); got.Class != tc.want {
			t.Errorf("%s: got=%s want=%s", tc.raw, got.Class, tc.want)
		}
	}
}

func TestACPFinalTextV2(t *testing.T) {
	s := newACPSession(nil, "sid")
	defer s.Close()
	s.promptFn = func(context.Context, string) (acp.StopReason, error) {
		for i := 0; i < 10000; i++ {
			s.dispatchEvent(Event{Kind: EventMessage, Text: "字a"})
			if i%200 == 0 {
				s.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start"}})
			}
		}
		return "end_turn", nil
	}
	done := make(chan error, 1)
	go func() { done <- s.Prompt(context.Background(), UserMessage{}) }()
	time.Sleep(20 * time.Millisecond)
	tools := 0
	for e := range s.Events() {
		if e.Tool != nil && e.Tool.Phase == "start" {
			tools++
		}
		if e.State == StateTurnCompleted {
			if !e.Final || e.Text != strings.Repeat("字a", 199) || tools != 50 {
				t.Fatalf("final=%v bytes=%d tools=%d", e.Final, len(e.Text), tools)
			}
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
