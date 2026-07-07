package cockpit

import (
	"context"
	"reflect"
	"testing"
)

// 编译期断言：fake 实现接口。
var (
	_ Backend = (*FakeBackend)(nil)
	_ Session = (*FakeSession)(nil)
)

func TestFakeSessionEvents(t *testing.T) {
	ctx := context.Background()
	backend := NewFakeBackend()

	sess, err := backend.Start(ctx, StartOpts{RepoDir: "/tmp/repo", Seat: "kimi-worker"})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.Close()

	s := sess.(*FakeSession)

	want := []Event{
		{Kind: EventMessage, Text: "hello", Final: false},
		{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: "ls", Text: ""}},
		{Kind: EventUsage, Usage: &Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, CostUSD: 0.001}},
		{Kind: EventState, State: "turn_completed"},
		{Kind: EventError, Err: "boom"},
	}

	for _, e := range want {
		s.Emit(e)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	var got []Event
	for e := range s.Events() {
		got = append(got, e)
	}

	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("event %d mismatch: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFakeSessionApprovalRespond(t *testing.T) {
	ctx := context.Background()
	backend := NewFakeBackend()

	sess, err := backend.Start(ctx, StartOpts{Seat: "kimi-worker"})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.Close()

	s := sess.(*FakeSession)

	var decision Decision
	s.EmitApproval(ApprovalRequest{
		Kind:     "command",
		ToolName: "rm",
		Respond: func(d Decision) error {
			decision = d
			return nil
		},
	})

	var req ApprovalRequest
	select {
	case req = <-s.Approvals():
	default:
		t.Fatal("expected an approval request")
	}

	if err := req.Respond(DecisionAllow); err != nil {
		t.Fatalf("first Respond failed: %v", err)
	}
	if decision != DecisionAllow {
		t.Fatalf("decision = %q, want %q", decision, DecisionAllow)
	}

	if err := req.Respond(DecisionDeny); err == nil {
		t.Fatal("second Respond should return an error")
	}
}

func TestFakeSessionPromptAndInterrupt(t *testing.T) {
	ctx := context.Background()
	s := NewFakeSession("thread-42")
	defer s.Close()

	if err := s.Prompt(ctx, UserMessage{Text: "hi"}); err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	if err := s.Prompt(ctx, UserMessage{Text: "again"}); err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	if len(s.Prompts) != 2 || s.Prompts[0].Text != "hi" || s.Prompts[1].Text != "again" {
		t.Fatalf("Prompts = %v, want [hi again]", s.Prompts)
	}

	if err := s.Interrupt(ctx); err != nil {
		t.Fatalf("Interrupt failed: %v", err)
	}
	if !s.InterruptCalled {
		t.Fatal("Interrupt was not recorded")
	}
}

func TestFakeSessionCloseIdempotent(t *testing.T) {
	s := NewFakeSession("thread-x")

	if err := s.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}

	// channel 已关闭，range 应正常退出。
	for range s.Events() {
		t.Fatal("closed channel should not yield events")
	}
	for range s.Approvals() {
		t.Fatal("closed channel should not yield approvals")
	}
}
