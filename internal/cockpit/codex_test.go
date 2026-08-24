package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
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

		// 5. notifications. These are shaped exactly as codex app-server emits
		// them (see TestCodexNotificationMapping for the captured originals):
		// the two `delta` fields are plain strings, and the token counters are
		// nested under tokenUsage.{last,total}.
		turn := map[string]any{"id": "turn-pipe-1", "items": []any{}, "itemsView": "notLoaded"}
		notifications := []map[string]any{
			{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{"threadId": "thread-pipe-123", "turn": mergeMap(turn, map[string]any{"status": "inProgress"})}},
			{"jsonrpc": "2.0", "method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread-pipe-123", "turnId": "turn-pipe-1", "itemId": "msg-1", "delta": "hello"}},
			{"jsonrpc": "2.0", "method": "item/started", "params": map[string]any{"threadId": "thread-pipe-123", "turnId": "turn-pipe-1", "startedAtMs": 1, "item": map[string]any{"type": "commandExecution", "id": "exec-1", "command": "ls", "cwd": "/tmp/repo", "status": "inProgress", "commandActions": []any{}}}},
			{"jsonrpc": "2.0", "method": "item/commandExecution/outputDelta", "params": map[string]any{"threadId": "thread-pipe-123", "turnId": "turn-pipe-1", "itemId": "exec-1", "delta": "output-line"}},
			{"jsonrpc": "2.0", "method": "item/completed", "params": map[string]any{"threadId": "thread-pipe-123", "turnId": "turn-pipe-1", "completedAtMs": 2, "item": map[string]any{"type": "commandExecution", "id": "exec-1", "command": "ls", "cwd": "/tmp/repo", "status": "completed", "commandActions": []any{}}}},
			{"jsonrpc": "2.0", "method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread-pipe-123", "turnId": "turn-pipe-1", "tokenUsage": map[string]any{
				"last":               map[string]any{"inputTokens": 10, "outputTokens": 5, "totalTokens": 15, "cachedInputTokens": 0, "reasoningOutputTokens": 0},
				"total":              map[string]any{"inputTokens": 110, "outputTokens": 55, "totalTokens": 165, "cachedInputTokens": 0, "reasoningOutputTokens": 0},
				"modelContextWindow": 258400,
			}}},
			{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": "thread-pipe-123", "turn": mergeMap(turn, map[string]any{"status": "completed", "error": nil})}},
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

// ---- protocol mapping ------------------------------------------------------
//
// The params in the table below are VERBATIM frames captured from a live
// `codex app-server` (codex-cli 0.144.4) on 2026-08-24, driven over stdio in a
// throwaway CODEX_HOME. The only edit is that absolute scratch paths were
// shortened to /tmp/repo; every field name, nesting level and value shape is
// exactly what codex emitted. Cases that could not be provoked from a live
// session (mcpToolCall items, multi-file fileChange) are marked and were
// derived from internal/cockpit/codexschema/codex_app_server_protocol.schemas.json.

type codexNotifCase struct {
	name   string
	method string
	params string
	want   []Event
}

func codexNotifCases() []codexNotifCase {
	return []codexNotifCase{
		{
			// CAPTURED. AgentMessageDeltaNotification.delta is a plain string.
			name:   "agentMessage delta is a plain string",
			method: "item/agentMessage/delta",
			params: `{"threadId":"01a033fe-1b37-7b01-aeb0-f6aa3ee15526","turnId":"01a033fe-1bec-7893-8a10-39cbb770668b","itemId":"msg_0fa8f35148090dfe016a8c496dbdfc87d09fd75ce2b0669962","delta":"I’ll"}`,
			want:   []Event{{Kind: EventMessage, Text: "I’ll"}},
		},
		{
			// CAPTURED. CommandExecutionOutputDeltaNotification.delta likewise.
			name:   "commandExecution outputDelta is a plain string",
			method: "item/commandExecution/outputDelta",
			params: `{"threadId":"01a033ff-fd51-7940-b6c0-21bafbcb3184","turnId":"01a033ff-fdb5-7c30-bb5c-06b92bfeda00","itemId":"exec-d2a4e08c-6c07-4194-a2a8-22820723f568","delta":"line-2\n"}`,
			want:   []Event{{Kind: EventTool, Tool: &ToolEvent{Phase: "output", Text: "line-2\n"}}},
		},
		{
			// CAPTURED.
			name:   "commandExecution item/started names the command",
			method: "item/started",
			params: `{"item":{"type":"commandExecution","id":"exec-c9b1f522","command":"/bin/zsh -lc 'echo hello-from-pactify .'","cwd":"/tmp/repo","processId":"8255","source":"unifiedExecStartup","status":"inProgress","commandActions":[{"type":"unknown","command":"echo hello-from-pactify ."}],"aggregatedOutput":null,"exitCode":null,"durationMs":null},"threadId":"t","turnId":"u","startedAtMs":1787578698971}`,
			want:   []Event{{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: "/bin/zsh -lc 'echo hello-from-pactify .'"}}},
		},
		{
			// CAPTURED. fileChange items carry `changes[].path` — never
			// command/name/path at the item level.
			name:   "fileChange item/started names the changed file",
			method: "item/started",
			params: `{"item":{"type":"fileChange","id":"exec-9bc110cc","changes":[{"path":"/tmp/repo/note.txt","kind":{"type":"add"},"diff":"hello-pactify\n"}],"status":"inProgress"},"threadId":"t","turnId":"u","startedAtMs":1787578784883}`,
			want:   []Event{{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: "/tmp/repo/note.txt"}}},
		},
		{
			// CAPTURED.
			name:   "fileChange item/completed names the changed file",
			method: "item/completed",
			params: `{"item":{"type":"fileChange","id":"exec-9bc110cc","changes":[{"path":"/tmp/repo/note.txt","kind":{"type":"add"},"diff":"hello-pactify\n"}],"status":"completed"},"threadId":"t","turnId":"u","completedAtMs":1787578784979}`,
			want:   []Event{{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: "/tmp/repo/note.txt"}}},
		},
		{
			// SCHEMA-DERIVED (a multi-file patch was not provoked live).
			// FileChangeThreadItem.changes is an array of FileUpdateChange.
			name:   "fileChange with several files summarises the rest",
			method: "item/completed",
			params: `{"item":{"type":"fileChange","id":"exec-1","changes":[{"path":"/tmp/repo/a.txt","kind":{"type":"add"},"diff":"a\n"},{"path":"/tmp/repo/b.txt","kind":{"type":"update","move_path":null},"diff":"b\n"}],"status":"completed"},"threadId":"t","turnId":"u","completedAtMs":1}`,
			want:   []Event{{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: "/tmp/repo/a.txt (+1 more)"}}},
		},
		{
			// SCHEMA-DERIVED (no MCP server was configured in the capture
			// session). McpToolCallThreadItem requires arguments/id/server/
			// status/tool/type; the variant tag is "mcpToolCall", NOT "mcpTool".
			name:   "mcpToolCall item/started is recognised",
			method: "item/started",
			params: `{"item":{"type":"mcpToolCall","id":"mcp-1","server":"github","tool":"create_issue","arguments":{"title":"x"},"status":"inProgress","result":null,"error":null,"durationMs":null},"threadId":"t","turnId":"u","startedAtMs":1}`,
			want:   []Event{{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: "github/create_issue"}}},
		},
		{
			// SCHEMA-DERIVED, same source as above.
			name:   "mcpToolCall item/completed is recognised",
			method: "item/completed",
			params: `{"item":{"type":"mcpToolCall","id":"mcp-1","server":"github","tool":"create_issue","arguments":{"title":"x"},"status":"completed","result":null,"error":null,"durationMs":12},"threadId":"t","turnId":"u","completedAtMs":1}`,
			want:   []Event{{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: "github/create_issue"}}},
		},
		{
			// CAPTURED.
			name:   "agentMessage item/completed is the final-message marker",
			method: "item/completed",
			params: `{"item":{"type":"agentMessage","id":"msg_1","text":"The command printed ` + "`hello-from-pactify .`" + `","phase":"final_answer","memoryCitation":null},"threadId":"t","turnId":"u","completedAtMs":1787578701555}`,
			want:   []Event{{Kind: EventMessage, Final: true}},
		},
		{
			// CAPTURED. userMessage items must not appear on the tool timeline.
			name:   "userMessage item/started emits nothing",
			method: "item/started",
			params: `{"item":{"type":"userMessage","id":"u1","clientId":null,"content":[{"type":"text","text":"hi","text_elements":[]}]},"threadId":"t","turnId":"u","startedAtMs":1}`,
			want:   nil,
		},
		{
			// CAPTURED. ThreadTokenUsageUpdatedNotification nests the counters
			// under tokenUsage.{last,total}. `last` is the per-request delta:
			// in this very capture the previous frame reported total=15264 and
			// this one reports total=30572 == 15264 + last(15308), so summing
			// `last` reconstructs `total` while summing `total` double-counts.
			name:   "tokenUsage reports the per-request delta",
			method: "thread/tokenUsage/updated",
			params: `{"threadId":"t","turnId":"u","tokenUsage":{"total":{"totalTokens":30572,"inputTokens":30468,"cachedInputTokens":19968,"outputTokens":104,"reasoningOutputTokens":0},"last":{"totalTokens":15308,"inputTokens":15293,"cachedInputTokens":9984,"outputTokens":15,"reasoningOutputTokens":0},"modelContextWindow":258400}}`,
			want:   []Event{{Kind: EventUsage, Usage: &Usage{InputTokens: 15293, OutputTokens: 15, TotalTokens: 15308}}},
		},
		{
			// CAPTURED.
			name:   "turn/started",
			method: "turn/started",
			params: `{"threadId":"t","turn":{"id":"01a033fe-1bec","items":[],"itemsView":"notLoaded","status":"inProgress","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}`,
			want:   []Event{{Kind: EventState, State: "turn_started"}},
		},
		{
			// CAPTURED. turn/completed carries a Turn, which has NO usage field,
			// so it must not try to emit one.
			name:   "turn/completed success",
			method: "turn/completed",
			params: `{"threadId":"t","turn":{"id":"01a033fe-1bec","items":[],"itemsView":"notLoaded","status":"completed","error":null,"startedAt":1787578686,"completedAt":1787578701,"durationMs":15160}}`,
			want:   []Event{{Kind: EventState, State: "turn_completed"}},
		},
		{
			// CAPTURED (by issuing a real turn/interrupt mid-turn).
			name:   "turn/completed interrupted is still a terminal turn",
			method: "turn/completed",
			params: `{"threadId":"t","turn":{"id":"01a033ff-fdb5","items":[],"itemsView":"notLoaded","status":"interrupted","error":null,"startedAt":1787578809,"completedAt":1787578821,"durationMs":12123}}`,
			want:   []Event{{Kind: EventState, State: "turn_completed"}},
		},
		{
			// CAPTURED (by starting a turn with a nonexistent model). This is
			// how a failed turn actually arrives — there is no "turn/failed"
			// method anywhere in the protocol.
			name:   "turn/completed failed reports the turn error",
			method: "turn/completed",
			params: `{"threadId":"t","turn":{"id":"01a03400-cbf6","items":[],"itemsView":"notLoaded","status":"failed","error":{"message":"The 'definitely-not-a-real-model-xyz' model is not supported when using Codex with a ChatGPT account.","codexErrorInfo":"other","additionalDetails":null},"startedAt":1787578862,"completedAt":1787578865,"durationMs":3301}}`,
			want: []Event{
				{Kind: EventState, State: "turn_failed"},
				{Kind: EventError, Err: "The 'definitely-not-a-real-model-xyz' model is not supported when using Codex with a ChatGPT account."},
			},
		},
		{
			// CAPTURED. ErrorNotification.error is a TurnError.
			name:   "error notification",
			method: "error",
			params: `{"error":{"message":"boom","codexErrorInfo":"other","additionalDetails":null},"willRetry":false,"threadId":"t","turnId":"u"}`,
			want:   []Event{{Kind: EventError, Err: "boom"}},
		},
	}
}

func TestCodexNotificationMapping(t *testing.T) {
	for _, tc := range codexNotifCases() {
		t.Run(tc.name, func(t *testing.T) {
			c, got := newCodexRecordingClient()
			c.handleNotification(tc.method, json.RawMessage(tc.params))
			assertEvents(t, *got, tc.want)
			for i, e := range *got {
				if len(e.Raw) == 0 {
					t.Errorf("event[%d].Raw is empty, want the original params", i)
				}
			}
		})
	}
}

// TestCodexTurnFailureIsNotReportedTwice covers the real ordering seen in the
// capture: codex sends the `error` notification AND then turn/completed with
// the same TurnError. The cockpit stream must show one error, not two.
func TestCodexTurnFailureIsNotReportedTwice(t *testing.T) {
	const msg = "The 'x' model is not supported when using Codex with a ChatGPT account."
	c, got := newCodexRecordingClient()

	c.handleNotification("turn/started", json.RawMessage(`{"threadId":"t","turn":{"id":"turn-1","items":[],"status":"inProgress"}}`))
	c.handleNotification("error", json.RawMessage(`{"error":{"message":`+jsonQuote(msg)+`,"codexErrorInfo":"other","additionalDetails":null},"willRetry":false,"threadId":"t","turnId":"turn-1"}`))
	c.handleNotification("turn/completed", json.RawMessage(`{"threadId":"t","turn":{"id":"turn-1","items":[],"status":"failed","error":{"message":`+jsonQuote(msg)+`,"codexErrorInfo":"other","additionalDetails":null}}}`))

	assertEvents(t, *got, []Event{
		{Kind: EventState, State: "turn_started"},
		{Kind: EventError, Err: msg},
		{Kind: EventState, State: "turn_failed"},
	})

	// A NEW turn failing with the same text must still be reported.
	c.handleNotification("turn/started", json.RawMessage(`{"threadId":"t","turn":{"id":"turn-2","items":[],"status":"inProgress"}}`))
	c.handleNotification("turn/completed", json.RawMessage(`{"threadId":"t","turn":{"id":"turn-2","items":[],"status":"failed","error":{"message":`+jsonQuote(msg)+`}}}`))
	if n := len(*got); n != 6 {
		t.Fatalf("got %d events, want 6: %+v", n, *got)
	}
	if (*got)[5].Kind != EventError || (*got)[5].Err != msg {
		t.Errorf("second turn's failure was swallowed: %+v", (*got)[5])
	}
}

// TestCodexThreadResumeIsARequest pins thread/resume as a ClientRequest: it must
// be sent with an id and its ThreadResumeResponse must be read back. Sent as a
// notification it gets no answer, so a failed resume looks like a success.
func TestCodexThreadResumeIsARequest(t *testing.T) {
	srv := newFakeCodexServer(t)
	defer srv.Close()

	srv.handle("thread/resume", func(req wireMessage) any {
		var p struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ThreadID != "thread-abc" {
			t.Errorf("thread/resume threadId = %q, want thread-abc", p.ThreadID)
		}
		// Verbatim-shaped ThreadResumeResponse (trimmed to the fields the
		// client reads plus the required siblings).
		return map[string]any{
			"thread":            map[string]any{"id": "thread-abc"},
			"cwd":               "/tmp/repo",
			"model":             "gpt-5",
			"modelProvider":     "openai",
			"sandbox":           "read-only",
			"approvalPolicy":    "on-request",
			"approvalsReviewer": "user",
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id, err := srv.client.threadResume(ctx, "thread-abc")
	if err != nil {
		t.Fatalf("threadResume: %v", err)
	}
	if id != "thread-abc" {
		t.Fatalf("threadResume returned %q, want thread-abc", id)
	}
	if got := srv.methodOf("thread/resume"); !got.isRequest {
		t.Errorf("thread/resume was sent as a notification (no id); it is a ClientRequest")
	}
}

// TestCodexThreadResumeSurfacesFailure: because resume is a request, a server
// error now actually reaches the caller.
func TestCodexThreadResumeSurfacesFailure(t *testing.T) {
	srv := newFakeCodexServer(t)
	defer srv.Close()
	srv.handleErr("thread/resume", -32602, "no such thread")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := srv.client.threadResume(ctx, "gone"); err == nil {
		t.Fatal("threadResume on a missing thread should fail")
	}
}

// TestCodexTurnInterruptIsARequestWithTurnID pins TurnInterruptParams =
// {threadId, turnId} and turn/interrupt being a ClientRequest.
func TestCodexTurnInterruptIsARequestWithTurnID(t *testing.T) {
	srv := newFakeCodexServer(t)
	defer srv.Close()

	srv.handle("turn/start", func(wireMessage) any {
		return map[string]any{"turn": map[string]any{
			"id": "turn-42", "items": []any{}, "status": "inProgress",
		}}
	})
	var gotThread, gotTurn string
	srv.handle("turn/interrupt", func(req wireMessage) any {
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		gotThread, gotTurn = p.ThreadID, p.TurnID
		return map[string]any{}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.client.turnStart(ctx, "thread-1", "hi"); err != nil {
		t.Fatalf("turnStart: %v", err)
	}
	if err := srv.client.turnInterrupt(ctx, "thread-1"); err != nil {
		t.Fatalf("turnInterrupt: %v", err)
	}
	if gotThread != "thread-1" || gotTurn != "turn-42" {
		t.Errorf("turn/interrupt params = {threadId:%q turnId:%q}, want {thread-1 turn-42}", gotThread, gotTurn)
	}
	if got := srv.methodOf("turn/interrupt"); !got.isRequest {
		t.Errorf("turn/interrupt was sent as a notification (no id); it is a ClientRequest")
	}
}

// TestCodexTurnInterruptWithoutActiveTurn: the turn id is recorded from the
// synchronous turn/start response, so an empty id means nothing is running.
// Interrupting then must be a silent no-op rather than an invalid request.
func TestCodexTurnInterruptWithoutActiveTurn(t *testing.T) {
	srv := newFakeCodexServer(t)
	defer srv.Close()
	srv.handle("turn/interrupt", func(wireMessage) any {
		t.Error("turn/interrupt must not be sent when no turn is running")
		return map[string]any{}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.client.turnInterrupt(ctx, "thread-1"); err != nil {
		t.Fatalf("turnInterrupt with no active turn should be a no-op, got %v", err)
	}
}

// TestCodexTurnIDClearedOnCompletion: once the turn ends there is nothing to
// interrupt again.
func TestCodexTurnIDClearedOnCompletion(t *testing.T) {
	c, _ := newCodexRecordingClient()
	c.handleNotification("turn/started", json.RawMessage(`{"threadId":"t","turn":{"id":"turn-9","items":[],"status":"inProgress"}}`))
	if got := c.currentTurnID(); got != "turn-9" {
		t.Fatalf("turnID after turn/started = %q, want turn-9", got)
	}
	c.handleNotification("turn/completed", json.RawMessage(`{"threadId":"t","turn":{"id":"turn-9","items":[],"status":"completed"}}`))
	if got := c.currentTurnID(); got != "" {
		t.Fatalf("turnID after turn/completed = %q, want empty", got)
	}
}

// TestCodexApprovalReplyShapes pins the three approval replies. Command and
// file-change approvals answer with {decision}; a permissions approval answers
// with PermissionsRequestApprovalResponse = {permissions, scope, ...} and has
// no `decision` field at all.
func TestCodexApprovalReplyShapes(t *testing.T) {
	// CAPTURED params for the first two; the permissions params are
	// schema-derived from PermissionsRequestApprovalParams (no live permission
	// prompt could be provoked).
	const cmdParams = `{"threadId":"t","turnId":"u","itemId":"exec-03fcfee3","startedAtMs":1787578789728,"environmentId":"local","command":"/bin/zsh -lc 'wc -l -c note.txt'","cwd":"/tmp/repo","commandActions":[{"type":"unknown","command":"wc -l -c note.txt"}]}`
	const fileParams = `{"threadId":"t","turnId":"u","itemId":"exec-9bc110cc","startedAtMs":1787578784884,"reason":null,"grantRoot":null}`
	const permParams = `{"threadId":"t","turnId":"u","itemId":"item-1","cwd":"/tmp/repo","environmentId":"local","startedAtMs":1,"reason":"needs network","permissions":{"network":{"enabled":true}}}`

	cases := []struct {
		name     string
		method   string
		params   string
		decision Decision
		want     string
	}{
		{"command accept", "item/commandExecution/requestApproval", cmdParams, DecisionAllow, `{"decision":"accept"}`},
		{"command deny", "item/commandExecution/requestApproval", cmdParams, DecisionDeny, `{"decision":"decline"}`},
		{"fileChange session", "item/fileChange/requestApproval", fileParams, DecisionAllowForSession, `{"decision":"acceptForSession"}`},
		{"permissions allow grants what was asked, for this turn", "item/permissions/requestApproval", permParams, DecisionAllow,
			`{"permissions":{"network":{"enabled":true}},"scope":"turn"}`},
		{"permissions allow-for-session widens the scope", "item/permissions/requestApproval", permParams, DecisionAllowForSession,
			`{"permissions":{"network":{"enabled":true}},"scope":"session"}`},
		{"permissions deny grants nothing", "item/permissions/requestApproval", permParams, DecisionDeny,
			`{"permissions":{},"scope":"turn"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeCodexServer(t)
			defer srv.Close()

			srv.request(7, tc.method, json.RawMessage(tc.params))

			var req ApprovalRequest
			select {
			case req = <-srv.approvals:
			case <-time.After(5 * time.Second):
				t.Fatal("no approval surfaced")
			}
			if err := req.Respond(tc.decision); err != nil {
				t.Fatalf("Respond: %v", err)
			}

			reply := srv.waitReply(7)
			var got any
			if err := json.Unmarshal(reply, &got); err != nil {
				t.Fatalf("reply is not JSON: %s", reply)
			}
			var wantAny any
			_ = json.Unmarshal([]byte(tc.want), &wantAny)
			if !reflect.DeepEqual(got, wantAny) {
				t.Errorf("reply result = %s, want %s", reply, tc.want)
			}
		})
	}
}

// ---- helpers ---------------------------------------------------------------

func newCodexRecordingClient() (*codexClient, *[]Event) {
	var got []Event
	c := &codexClient{}
	c.dispatchEvent = func(e Event) { got = append(got, e) }
	return c, &got
}

func assertEvents(t *testing.T, got, want []Event) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d\n got: %s\nwant: %s", len(got), len(want), dumpEvents(got), dumpEvents(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Kind != w.Kind || g.Text != w.Text || g.Final != w.Final || g.State != w.State || g.Err != w.Err {
			t.Errorf("event[%d] = %s, want %s", i, dumpEvents(got[i:i+1]), dumpEvents(want[i:i+1]))
		}
		if !toolEventEqual(g.Tool, w.Tool) {
			t.Errorf("event[%d].Tool = %+v, want %+v", i, g.Tool, w.Tool)
		}
		if !usageEqual(g.Usage, w.Usage) {
			t.Errorf("event[%d].Usage = %+v, want %+v", i, g.Usage, w.Usage)
		}
	}
}

func dumpEvents(evs []Event) string {
	var sb strings.Builder
	for _, e := range evs {
		fmt.Fprintf(&sb, "{kind:%s text:%q final:%v state:%q err:%q tool:%+v usage:%+v} ",
			e.Kind, e.Text, e.Final, e.State, e.Err, e.Tool, e.Usage)
	}
	return sb.String()
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func mergeMap(base, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// fakeCodexServer is a scripted app-server peer over io.Pipe. Unlike the
// end-to-end TestCodexClientPipe it answers on demand, so a test can assert the
// exact frame the client put on the wire (including whether it carried an id).
type fakeCodexServer struct {
	t         *testing.T
	client    *codexClient
	approvals chan ApprovalRequest
	events    chan Event

	inW  *io.PipeWriter
	outR *io.PipeReader

	mu       sync.Mutex
	handlers map[string]func(wireMessage) any
	errors   map[string]*rpcError
	seen     map[string]seenFrame
	replies  map[int]json.RawMessage
	closed   bool
}

type seenFrame struct{ isRequest bool }

func newFakeCodexServer(t *testing.T) *fakeCodexServer {
	t.Helper()
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()

	s := &fakeCodexServer{
		t:         t,
		approvals: make(chan ApprovalRequest, 8),
		events:    make(chan Event, 64),
		inW:       serverToClientW,
		outR:      clientToServerR,
		handlers:  map[string]func(wireMessage) any{},
		errors:    map[string]*rpcError{},
		seen:      map[string]seenFrame{},
		replies:   map[int]json.RawMessage{},
	}

	s.client = newCodexClient(clientToServerW, serverToClientR, func() error {
		_ = clientToServerW.Close()
		_ = serverToClientR.Close()
		return nil
	},
		func(e Event) {
			select {
			case s.events <- e:
			default:
			}
		},
		func(a ApprovalRequest) { s.approvals <- a },
	)

	go s.loop()
	return s
}

func (s *fakeCodexServer) loop() {
	in := bufio.NewReader(s.outR)
	for {
		line, err := in.ReadBytes('\n')
		if len(line) > 0 {
			var m wireMessage
			if json.Unmarshal(line, &m) == nil {
				s.mu.Lock()
				if m.Method != "" {
					s.seen[m.Method] = seenFrame{isRequest: hasJSONKey(line, "id")}
				} else if m.Result != nil {
					s.replies[m.ID] = m.Result
				}
				h := s.handlers[m.Method]
				e := s.errors[m.Method]
				s.mu.Unlock()

				if m.Method != "" && hasJSONKey(line, "id") {
					switch {
					case e != nil:
						s.write(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": e})
					case h != nil:
						s.write(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": h(m)})
					default:
						s.write(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{}})
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *fakeCodexServer) write(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	if _, err := s.inW.Write(append(b, '\n')); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		panic(err)
	}
}

func (s *fakeCodexServer) handle(method string, fn func(wireMessage) any) {
	s.mu.Lock()
	s.handlers[method] = fn
	s.mu.Unlock()
}

func (s *fakeCodexServer) handleErr(method string, code int, msg string) {
	s.mu.Lock()
	s.errors[method] = &rpcError{Code: code, Message: msg}
	s.mu.Unlock()
}

// request sends a server->client JSON-RPC request (an approval prompt).
func (s *fakeCodexServer) request(id int, method string, params json.RawMessage) {
	s.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}

func (s *fakeCodexServer) waitReply(id int) json.RawMessage {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		r, ok := s.replies[id]
		s.mu.Unlock()
		if ok {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.t.Fatalf("no reply to server request id=%d", id)
	return nil
}

func (s *fakeCodexServer) methodOf(method string) seenFrame {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		f, ok := s.seen[method]
		s.mu.Unlock()
		if ok {
			return f
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.t.Fatalf("client never sent %s", method)
	return seenFrame{}
}

func (s *fakeCodexServer) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	_ = s.client.Close()
	_ = s.inW.Close()
	_ = s.outR.Close()
}

// hasJSONKey reports whether a top-level key is present in a JSON object. It is
// how the tests tell a JSON-RPC request (has "id") from a notification.
func hasJSONKey(line []byte, key string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(line, &m) != nil {
		return false
	}
	_, ok := m[key]
	return ok
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
