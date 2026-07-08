package cockpit

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
)

func TestMapACPUpdateToEventMessage(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}`)
	u := acp.SessionUpdate{Kind: "agent_message_chunk", Raw: raw}

	e, ok := MapACPUpdateToEvent(u)
	if !ok {
		t.Fatal("expected event")
	}
	want := Event{Kind: EventMessage, Text: "hello", Raw: raw}
	if !reflect.DeepEqual(e, want) {
		t.Fatalf("got %+v, want %+v", e, want)
	}
}

func TestMapACPUpdateToEventUsage(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"usage","usage":{"inputTokens":10,"outputTokens":5,"cost":0.0012}}`)
	u := acp.SessionUpdate{
		Kind:  "usage",
		Usage: &acp.Usage{InputTokens: 10, OutputTokens: 5, Cost: 0.0012},
		Raw:   raw,
	}

	e, ok := MapACPUpdateToEvent(u)
	if !ok {
		t.Fatal("expected event")
	}
	want := Event{
		Kind: EventUsage,
		Usage: &Usage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
			CostUSD:      0.0012,
		},
		Raw: raw,
	}
	if !reflect.DeepEqual(e, want) {
		t.Fatalf("got %+v, want %+v", e, want)
	}
}

func TestMapACPUpdateToEventTool(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"tool_call","title":"ls","kind":"bash"}`)
	u := acp.SessionUpdate{Kind: "tool_call", Raw: raw}
	e, _ := MapACPUpdateToEvent(u)
	if e.Kind != EventTool || e.Tool == nil || e.Tool.Phase != "start" || e.Tool.Name != "ls" {
		t.Fatalf("tool_call event mismatch: %+v", e)
	}

	raw2 := json.RawMessage(`{"sessionUpdate":"tool_call_update","title":"ls"}`)
	u2 := acp.SessionUpdate{Kind: "tool_call_update", Raw: raw2}
	e2, _ := MapACPUpdateToEvent(u2)
	if e2.Kind != EventTool || e2.Tool == nil || e2.Tool.Phase != "output" || e2.Tool.Name != "ls" {
		t.Fatalf("tool_call_update event mismatch: %+v", e2)
	}
}

func TestMapDecisionToOutcomeAllow(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "opt-deny", Kind: "deny"},
		{OptionID: "opt-allow", Kind: "allow"},
	}
	out := MapDecisionToOutcome(DecisionAllow, opts)
	if out.OptionID != "opt-allow" || out.Cancelled {
		t.Fatalf("got %+v, want allow option", out)
	}
}

func TestMapDecisionToOutcomeAllowForSession(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "opt-allow", Kind: "allow"},
		{OptionID: "opt-always", Kind: "always-session"},
	}
	out := MapDecisionToOutcome(DecisionAllowForSession, opts)
	if out.OptionID != "opt-always" || out.Cancelled {
		t.Fatalf("got %+v, want always-session option", out)
	}
}

func TestMapDecisionToOutcomeDeny(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "opt-allow", Kind: "allow"},
		{OptionID: "opt-reject", Kind: "reject"},
	}
	out := MapDecisionToOutcome(DecisionDeny, opts)
	if out.OptionID != "opt-reject" || out.Cancelled {
		t.Fatalf("got %+v, want reject option", out)
	}
}

func TestMapDecisionToOutcomeDenyFallsBackToCancelled(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "opt-allow", Kind: "allow"},
	}
	out := MapDecisionToOutcome(DecisionDeny, opts)
	if !out.Cancelled {
		t.Fatalf("got %+v, want cancelled fallback", out)
	}
}

func TestPermissionBridgeNoDeadlock(t *testing.T) {
	approvals := make(chan ApprovalRequest, 1)
	done := make(chan struct{})
	defer close(done)

	handler := newPermissionHandler(approvals, done)

	req := acp.PermissionRequest{
		ToolCall: acp.PermissionToolCall{ToolCallID: "tc1", Title: "rm -rf /"},
		Options: []acp.PermissionOption{
			{OptionID: "opt-allow", Kind: "allow"},
			{OptionID: "opt-deny", Kind: "deny"},
		},
	}

	outcomeCh := make(chan acp.PermissionOutcome, 1)
	go func() {
		outcomeCh <- handler(req)
	}()

	var ar ApprovalRequest
	select {
	case ar = <-approvals:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}

	if ar.ToolName != "rm -rf /" {
		t.Fatalf("tool name = %q, want %q", ar.ToolName, "rm -rf /")
	}
	if err := ar.Respond(DecisionAllow); err != nil {
		t.Fatalf("Respond failed: %v", err)
	}

	select {
	case out := <-outcomeCh:
		if out.OptionID != "opt-allow" || out.Cancelled {
			t.Fatalf("got %+v, want allow option", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission outcome")
	}
}

func TestPermissionBridgeDoneCancels(t *testing.T) {
	approvals := make(chan ApprovalRequest, 1)
	done := make(chan struct{})
	close(done)

	handler := newPermissionHandler(approvals, done)
	out := handler(acp.PermissionRequest{
		Options: []acp.PermissionOption{{OptionID: "opt-allow", Kind: "allow"}},
	})
	if !out.Cancelled {
		t.Fatalf("got %+v, want cancelled when session done", out)
	}
}

func TestACPSessionCloseIdempotent(t *testing.T) {
	// We cannot build an acp.Client directly (newClient is unexported), so use a
	// minimal Backend compile check and exercise Close on a session whose client
	// field is nil by constructing it via the unexported constructor with a nil
	// client. Close clears handlers and closes channels once.
	s := newACPSession(nil, "sid-1")
	if err := s.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
	for range s.Events() {
		t.Fatal("closed events channel should not yield")
	}
	for range s.Approvals() {
		t.Fatal("closed approvals channel should not yield")
	}
}

func TestACPBackendSmokeKimi(t *testing.T) {
	if os.Getenv("COCKPIT_SMOKE") != "1" {
		t.Skip("set COCKPIT_SMOKE=1 to run real-agent smoke")
	}
	if _, err := exec.LookPath("kimi"); err != nil {
		t.Skip("kimi not in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	b := newACPBackend("kimi-cli")
	sess, err := b.Start(ctx, StartOpts{RepoDir: t.TempDir(), Seat: "kimi-smoke"})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.Close()

	go func() {
		_ = sess.Prompt(ctx, UserMessage{Text: "hi"})
	}()

	saw := false
	timeout := time.After(20 * time.Second)
	for !saw {
		select {
		case e := <-sess.Events():
			if e.Kind == EventMessage || e.Kind == EventUsage {
				saw = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for an ACP event")
		case <-ctx.Done():
			t.Fatalf("context done: %v", ctx.Err())
		}
	}
}
