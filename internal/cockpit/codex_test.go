package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// compile-time interface checks.
var (
	_ Backend = (*codexBackend)(nil)
	_ Session = (*codexSession)(nil)
)

// TestCodexClientPipe runs a fake app-server over io.Pipe and exercises the
// full client/session lifecycle: initialize/initialized/thread/start, event
// dispatch, server-side approval requests, and Close idempotency.
func TestCodexClientPipe(t *testing.T) {
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()

	serverDone := make(chan struct{})
	approvalAck := make(chan struct{})
	go func() {
		defer close(serverDone)
		defer serverToClientW.Close()

		in := bufio.NewReader(clientToServerR)
		out := serverToClientW

		// 1. initialize
		req := mustReadJSON(in, t)
		if req.Method != "initialize" {
			t.Errorf("first request method = %q, want initialize", req.Method)
		}
		mustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"userAgent":      "codex/0.0.0",
				"codexHome":      "/tmp/codex",
				"platformFamily": "fake",
				"platformOs":     "testos",
			},
		})

		// 2. initialized notification
		req = mustReadJSON(in, t)
		if req.Method != "initialized" {
			t.Errorf("second request method = %q, want initialized", req.Method)
		}

		// 3. thread/start
		req = mustReadJSON(in, t)
		if req.Method != "thread/start" {
			t.Errorf("third request method = %q, want thread/start", req.Method)
		}
		mustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"thread": map[string]any{"id": "thread-pipe-123"},
			},
		})

		// 4. turn/start
		req = mustReadJSON(in, t)
		if req.Method != "turn/start" {
			t.Errorf("fourth request method = %q, want turn/start", req.Method)
		}
		mustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  map[string]any{},
		})

		// 5. notifications
		notifications := []map[string]any{
			{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{"threadId": "thread-pipe-123"}},
			{"jsonrpc": "2.0", "method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread-pipe-123", "delta": map[string]any{"text": "hello"}}},
			{"jsonrpc": "2.0", "method": "item/started", "params": map[string]any{"threadId": "thread-pipe-123", "item": map[string]any{"type": "commandExecution", "command": "ls"}}},
			{"jsonrpc": "2.0", "method": "item/commandExecution/outputDelta", "params": map[string]any{"threadId": "thread-pipe-123", "delta": map[string]any{"text": "output-line"}}},
			{"jsonrpc": "2.0", "method": "item/completed", "params": map[string]any{"threadId": "thread-pipe-123", "item": map[string]any{"type": "commandExecution", "command": "ls"}}},
			{"jsonrpc": "2.0", "method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread-pipe-123", "inputTokens": 10, "outputTokens": 5, "totalTokens": 15}},
			{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": "thread-pipe-123"}},
		}
		for _, n := range notifications {
			mustWriteJSON(out, n)
		}

		// 6. server->client approval request
		mustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"id":      100,
			"method":  "item/commandExecution/requestApproval",
			"params": map[string]any{
				"threadId":   "thread-pipe-123",
				"approvalId": "approve-1",
				"turnId":     "turn-1",
				"command":    "rm -rf /",
				"cwd":        "/tmp/repo",
				"reason":     "test approval",
			},
		})

		// 7. read approval response
		resp := mustReadJSON(in, t)
		if resp.ID != 100 {
			t.Errorf("approval response id = %d, want 100", resp.ID)
		}
		var body struct {
			Result struct {
				Decision string `json:"decision"`
			} `json:"result"`
		}
		b, _ := json.Marshal(resp.Raw)
		if err := json.Unmarshal(b, &body); err != nil {
			t.Errorf("unmarshal approval response: %v", err)
			close(approvalAck)
			return
		}
		if body.Result.Decision != "accept" {
			t.Errorf("approval decision = %q, want accept", body.Result.Decision)
		}
		close(approvalAck)
	}()

	closeFn := func() error {
		_ = clientToServerW.Close()
		_ = serverToClientR.Close()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess := newCodexSessionWithClient(nil, "")
	client := newCodexClient(clientToServerW, serverToClientR, closeFn,
		func(e Event) { sess.events <- e },
		func(a ApprovalRequest) { sess.approvals <- a },
	)
	sess.client = client

	if _, err := client.initialize(ctx); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	if err := client.initialized(ctx); err != nil {
		t.Fatalf("initialized failed: %v", err)
	}
	threadID, err := client.threadStart(ctx, "/tmp/repo")
	if err != nil {
		t.Fatalf("threadStart failed: %v", err)
	}
	if threadID != "thread-pipe-123" {
		t.Fatalf("threadID = %q, want thread-pipe-123", threadID)
	}
	sess.threadID = threadID

	if err := sess.Prompt(ctx, UserMessage{Text: "hi"}); err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	// Drain events.
	var events []Event
	drain := func(count int) {
		for len(events) < count {
			select {
			case e := <-sess.Events():
				events = append(events, e)
			case <-time.After(5 * time.Second):
				t.Fatalf("timed out waiting for events, got %d want %d", len(events), count)
			}
		}
	}
	drain(7)

	want := []Event{
		{Kind: EventState, State: "turn_started"},
		{Kind: EventMessage, Text: "hello", Final: false},
		{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: "ls"}},
		{Kind: EventTool, Tool: &ToolEvent{Phase: "output", Text: "output-line"}},
		{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: "ls"}},
		{Kind: EventUsage, Usage: &Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}},
		{Kind: EventState, State: "turn_completed"},
	}
	for i := range want {
		if events[i].Kind != want[i].Kind {
			t.Errorf("event[%d].Kind = %q, want %q", i, events[i].Kind, want[i].Kind)
		}
		if events[i].Text != want[i].Text {
			t.Errorf("event[%d].Text = %q, want %q", i, events[i].Text, want[i].Text)
		}
		if events[i].State != want[i].State {
			t.Errorf("event[%d].State = %q, want %q", i, events[i].State, want[i].State)
		}
		if !toolEventEqual(events[i].Tool, want[i].Tool) {
			t.Errorf("event[%d].Tool = %+v, want %+v", i, events[i].Tool, want[i].Tool)
		}
		if !usageEqual(events[i].Usage, want[i].Usage) {
			t.Errorf("event[%d].Usage = %+v, want %+v", i, events[i].Usage, want[i].Usage)
		}
		if len(events[i].Raw) == 0 {
			t.Errorf("event[%d].Raw is empty, want original params", i)
		}
	}

	// Approval.
	var req ApprovalRequest
	select {
	case req = <-sess.Approvals():
	case <-time.After(5 * time.Second):
		t.Fatal("expected approval request")
	}
	if req.Kind != "command" {
		t.Errorf("approval Kind = %q, want command", req.Kind)
	}
	if req.ToolName != "rm -rf /" {
		t.Errorf("approval ToolName = %q, want rm -rf /", req.ToolName)
	}
	if len(req.RawInput) == 0 {
		t.Error("approval RawInput is empty")
	}
	if err := req.Respond(DecisionAllow); err != nil {
		t.Fatalf("Respond failed: %v", err)
	}

	// Wait for the fake server to observe the accepted response before closing.
	select {
	case <-approvalAck:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not ack approval response")
	}

	if err := req.Respond(DecisionDeny); err == nil {
		t.Fatal("second Respond should fail")
	}

	// Close idempotency.
	if err := sess.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}

	// Wait for fake server goroutine to finish.
	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
		t.Fatal("fake server did not finish")
	}
}

func TestCodexBackendSmoke(t *testing.T) {
	if os.Getenv("COCKPIT_SMOKE") != "1" {
		t.Skip("set COCKPIT_SMOKE=1 to run real codex app-server smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	backend := NewCodexBackend()
	repoDir := t.TempDir()
	sess, err := backend.Start(ctx, StartOpts{RepoDir: repoDir, Seat: "kimi-worker"})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.Close()

	if sess.ThreadID() == "" {
		t.Fatal("expected non-empty threadID")
	}
}

func toolEventEqual(a, b *ToolEvent) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Phase == b.Phase && a.Name == b.Name && a.Text == b.Text
}

func usageEqual(a, b *Usage) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.InputTokens == b.InputTokens && a.OutputTokens == b.OutputTokens &&
		a.TotalTokens == b.TotalTokens && a.CostUSD == b.CostUSD
}

type wireMessage struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
	Raw json.RawMessage `json:"-"`
}

func mustReadJSON(r *bufio.Reader, t *testing.T) wireMessage {
	t.Helper()
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read json line: %v", err)
	}
	var m wireMessage
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("unmarshal json line %q: %v", string(line), err)
	}
	m.Raw = json.RawMessage(line)
	return m
}

func mustWriteJSON(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		panic(err)
	}
}
