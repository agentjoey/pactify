package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// compile-time interface checks.
var (
	_ Backend = (*claudeBackend)(nil)
	_ Session = (*claudeSession)(nil)
)

// TestClaudeClientPipe runs a fake Claude host over io.Pipe and exercises the
// full client/session lifecycle: initialize, thread/start, turn/start, event
// dispatch, asynchronous threadID fill, server-side approval requests, and
// Close idempotency.
func TestClaudeClientPipe(t *testing.T) {
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
		req := mustReadJSONRawID(in, t)
		if req.Method != "initialize" {
			t.Errorf("first request method = %q, want initialize", req.Method)
		}
		claudeMustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  map[string]any{"ok": true},
		})

		// 2. thread/start
		req = mustReadJSONRawID(in, t)
		if req.Method != "thread/start" {
			t.Errorf("second request method = %q, want thread/start", req.Method)
		}
		claudeMustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  map[string]any{},
		})

		// 3. turn/start
		req = mustReadJSONRawID(in, t)
		if req.Method != "turn/start" {
			t.Errorf("third request method = %q, want turn/start", req.Method)
		}
		claudeMustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  map[string]any{},
		})

		// 4. notifications
		claudeMustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"method":  "cockpit/session",
			"params":  map[string]any{"threadId": "thread-claude-abc"},
		})
		claudeMustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"method":  "cockpit/message",
			"params":  map[string]any{"text": "hello", "final": true},
		})

		// 5. server->client approval request
		claudeMustWriteJSON(out, map[string]any{
			"jsonrpc": "2.0",
			"id":      "h1",
			"method":  "approval/request",
			"params": map[string]any{
				"toolName":  "bash",
				"input":     map[string]any{"cmd": "ls"},
				"title":     "run command",
				"requestId": "r1",
				"danger":    "high",
			},
		})

		// 6. read approval response
		resp := mustReadJSONRawID(in, t)
		if string(resp.ID) != `"h1"` {
			t.Errorf("approval response id = %q, want \"h1\"", string(resp.ID))
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
		if body.Result.Decision != "allow" {
			t.Errorf("approval decision = %q, want allow", body.Result.Decision)
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

	sess, err := newClaudeSessionForTest(ctx, clientToServerW, serverToClientR, closeFn, "/tmp/repo")
	if err != nil {
		t.Fatalf("newClaudeSessionForTest failed: %v", err)
	}
	defer sess.Close()

	if sess.ThreadID() != "" {
		t.Errorf("ThreadID() = %q before session notification, want empty", sess.ThreadID())
	}

	if err := sess.Prompt(ctx, UserMessage{Text: "hi"}); err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	var msg Event
	select {
	case msg = <-sess.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for events")
	}
	if msg.Kind != EventMessage {
		t.Errorf("first event Kind = %q, want message", msg.Kind)
	}
	if msg.Text != "hello" {
		t.Errorf("first event Text = %q, want hello", msg.Text)
	}
	if !msg.Final {
		t.Error("first event Final = false, want true")
	}
	if len(msg.Raw) == 0 {
		t.Error("first event Raw is empty")
	}

	if got := sess.ThreadID(); got != "thread-claude-abc" {
		t.Errorf("ThreadID() = %q after session notification, want thread-claude-abc", got)
	}

	var req ApprovalRequest
	select {
	case req = <-sess.Approvals():
	case <-time.After(5 * time.Second):
		t.Fatal("expected approval request")
	}
	if req.Kind != "tool" {
		t.Errorf("approval Kind = %q, want tool", req.Kind)
	}
	if req.ToolName != "bash" {
		t.Errorf("approval ToolName = %q, want bash", req.ToolName)
	}
	if len(req.RawInput) == 0 {
		t.Error("approval RawInput is empty")
	}
	if err := req.Respond(DecisionAllow); err != nil {
		t.Fatalf("Respond failed: %v", err)
	}
	if err := req.Respond(DecisionDeny); err == nil {
		t.Fatal("second Respond should fail")
	}

	select {
	case <-approvalAck:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not ack approval response")
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}

	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
		t.Fatal("fake server did not finish")
	}
}

// TestClaudeBackendSmoke is a gated real-host smoke test. It requires node,
// the vendored host bridge, and Claude credentials in the environment.
func TestClaudeBackendSmoke(t *testing.T) {
	if os.Getenv("COCKPIT_SMOKE") != "1" {
		t.Skip("set COCKPIT_SMOKE=1 to run real Claude host smoke test")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found in PATH")
	}

	hostPath := os.Getenv("PACT_CLAUDE_HOST")
	if hostPath == "" {
		var err error
		hostPath, err = filepath.Abs("vendor/claude-host/host.mjs")
		if err != nil {
			t.Skipf("cannot resolve host bridge: %v", err)
		}
	}
	if _, err := os.Stat(hostPath); err != nil {
		t.Skipf("host bridge not found: %v", err)
	}

	// Point the backend at the explicit host path so the test works even when
	// the binary is outside the repo tree.
	t.Setenv("PACT_CLAUDE_HOST", hostPath)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	backend := NewClaudeBackend()
	sess, err := backend.Start(ctx, StartOpts{RepoDir: t.TempDir(), Seat: "kimi-worker"})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.Close()

	if err := sess.Prompt(ctx, UserMessage{Text: "reply hi"}); err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	deadline := time.After(30 * time.Second)
	for sess.ThreadID() == "" {
		select {
		case <-sess.Events():
			// drain events while waiting for the session id
		case <-deadline:
			t.Fatal("timed out waiting for cockpit/session notification")
		}
	}

	saw := false
	for !saw {
		select {
		case e := <-sess.Events():
			if e.Kind == EventMessage || e.Kind == EventState {
				saw = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for cockpit/message or cockpit/state")
		}
	}
}

type wireMessageRaw struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
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

func mustReadJSONRawID(r *bufio.Reader, t *testing.T) wireMessageRaw {
	t.Helper()
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read json line: %v", err)
	}
	var m wireMessageRaw
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("unmarshal json line %q: %v", string(line), err)
	}
	m.Raw = json.RawMessage(line)
	return m
}

func claudeMustWriteJSON(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		panic(err)
	}
}
