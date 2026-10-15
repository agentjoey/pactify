package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

const kimiNewSession = `{"sessionId":"sess-1",
 "configOptions":[
  {"type":"select","id":"model","name":"Model","category":"model","currentValue":"kimi-code/k3-256k",
   "options":[{"value":"kimi-code/kimi-for-coding","name":"K2.8 Preview"},{"value":"kimi-code/k3-256k","name":"K3-256k"}]},
  {"type":"select","id":"thinking","name":"Thinking","category":"thought_level","currentValue":"high",
   "options":[{"value":"low","name":"Thinking Low"},{"value":"high","name":"Thinking High"}]}],
 "modes":{"currentModeId":"default","availableModes":[{"id":"default","name":"Default"},{"id":"plan","name":"Plan","description":"Plan without changes"}]}}`

func TestNewSessionDetailedParsesConfigAndModes(t *testing.T) {
	fs := &configServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
		"session/new": func(_ *fakeServer, p json.RawMessage) any {
			var got map[string]any
			if err := json.Unmarshal(p, &got); err != nil {
				t.Error(err)
			}
			if got["cwd"] != "/work/repo" || len(got) != 2 {
				t.Errorf("session/new params = %s", p)
			}
			if servers, ok := got["mcpServers"].([]any); !ok || len(servers) != 0 {
				t.Errorf("mcpServers = %#v, want empty array", got["mcpServers"])
			}
			return json.RawMessage(kimiNewSession)
		},
	}}
	c := newConfigTestClient(t, fs)
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	res, err := c.NewSessionDetailed(ctx(t), "/work/repo")
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "sess-1" || c.cwd != "/work/repo" {
		t.Fatalf("session = %+v, cwd = %q", res, c.cwd)
	}
	m, ok := FindConfig(res.ConfigOptions, "model")
	if !ok || m.Name != "Model" || m.Type != "select" || m.CurrentValue != "kimi-code/k3-256k" || len(m.Options) != 2 || m.Options[0].Value != "kimi-code/kimi-for-coding" || m.Options[0].Name != "K2.8 Preview" {
		t.Fatalf("model option = %+v", m)
	}
	if th, ok := FindConfig(res.ConfigOptions, "thought_level"); !ok || th.ID != "thinking" {
		t.Fatalf("thought_level lookup by category failed: %+v", th)
	}
	if res.CurrentModeID != "default" || len(res.Modes) != 2 || res.Modes[1].ID != "plan" || res.Modes[1].Description != "Plan without changes" {
		t.Fatalf("modes = %+v current=%q", res.Modes, res.CurrentModeID)
	}
}

func TestNewSessionDetailedOptionalFields(t *testing.T) {
	c := newConfigTestClient(t, &configServer{})
	res, err := c.NewSessionDetailed(ctx(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "sess-1" || len(res.ConfigOptions) != 0 || len(res.Modes) != 0 || res.CurrentModeID != "" || c.cwd != "." {
		t.Fatalf("session = %+v, cwd = %q", res, c.cwd)
	}
}

func TestSetConfigOptionSendsIdsAndReturnsOptions(t *testing.T) {
	var got map[string]any
	fs := &configServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
		"session/set_config_option": func(_ *fakeServer, p json.RawMessage) any {
			if err := json.Unmarshal(p, &got); err != nil {
				t.Error(err)
			}
			return map[string]any{"configOptions": []any{map[string]any{"id": "model", "category": "model", "currentValue": "kimi-code/kimi-for-coding"}}}
		},
	}}
	c := newConfigTestClient(t, fs)
	opts, err := c.SetConfigOption(ctx(t), "sess-1", "model", "kimi-code/kimi-for-coding")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["sessionId"] != "sess-1" || got["configId"] != "model" || got["value"] != "kimi-code/kimi-for-coding" {
		t.Fatalf("params = %v", got)
	}
	if len(opts) != 1 || opts[0].CurrentValue != "kimi-code/kimi-for-coding" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestSetModeAndCancel(t *testing.T) {
	var got map[string]any
	fs := &configServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
		"session/set_mode": func(_ *fakeServer, p json.RawMessage) any {
			if err := json.Unmarshal(p, &got); err != nil {
				t.Error(err)
			}
			return map[string]any{}
		},
	}}
	c := newConfigTestClient(t, fs)
	if err := c.SetMode(ctx(t), "sess-1", "plan"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["sessionId"] != "sess-1" || got["modeId"] != "plan" {
		t.Fatalf("set_mode params = %v", got)
	}
	if err := c.Cancel("sess-1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return fs.sawNotification("session/cancel") })
	fs.mu.Lock()
	frame := fs.notifications[0]
	fs.mu.Unlock()
	var notification map[string]json.RawMessage
	if err := json.Unmarshal(frame, &notification); err != nil {
		t.Fatal(err)
	}
	if len(notification) != 3 || string(notification["jsonrpc"]) != `"2.0"` || string(notification["params"]) != `{"sessionId":"sess-1"}` {
		t.Fatalf("cancel frame = %s, want JSON-RPC notification without id", frame)
	}
	if _, err := c.NewSession(ctx(t), "."); err != nil {
		t.Fatalf("client unusable after cancel: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Cancel("sess-1"); err == nil {
		t.Fatal("Cancel on closed client should fail")
	}
}

func TestFindConfigPrefersIDOverCategory(t *testing.T) {
	opts := []ConfigOption{{ID: "other", Category: "model"}, {ID: "model", Category: "custom"}}
	if got, ok := FindConfig(opts, "model"); !ok || got.ID != "model" {
		t.Fatalf("FindConfig = %+v, %v", got, ok)
	}
	if got, ok := FindConfig(opts, "missing"); ok || got.ID != "" {
		t.Fatalf("missing FindConfig = %+v, %v", got, ok)
	}
	if _, ok := FindConfig(nil, "model"); ok {
		t.Fatal("nil options matched")
	}
}

func TestSessionConfigurationErrors(t *testing.T) {
	operations := []struct {
		method string
		call   func(*Client) error
	}{
		{"session/new", func(c *Client) error { _, err := c.NewSessionDetailed(ctx(t), "."); return err }},
		{"session/set_config_option", func(c *Client) error { _, err := c.SetConfigOption(ctx(t), "sess-1", "model", "bad"); return err }},
		{"session/set_mode", func(c *Client) error { return c.SetMode(ctx(t), "sess-1", "bad") }},
	}
	for _, op := range operations {
		t.Run(op.method, func(t *testing.T) {
			fs := &configServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
				op.method: func(*fakeServer, json.RawMessage) any { return &rpcError{Code: -32602, Message: "unsupported value"} },
			}}
			err := op.call(newConfigTestClient(t, fs))
			var rpcErr *rpcError
			if !errors.As(err, &rpcErr) || rpcErr.Code != -32602 || rpcErr.Message != "unsupported value" {
				t.Fatalf("error = %v, want original RPC error", err)
			}
		})
		if op.method == "session/set_mode" {
			continue // set_mode has no result fields to decode.
		}
		t.Run(op.method+"/malformed_result", func(t *testing.T) {
			fs := &configServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
				op.method: func(*fakeServer, json.RawMessage) any { return "unexpected result type" },
			}}
			if err := op.call(newConfigTestClient(t, fs)); err == nil || !strings.Contains(err.Error(), op.method+" result:") {
				t.Fatalf("error = %v, want contextual decode error", err)
			}
		})
	}
}

// Extend the shared fake through composition to keep this task's changes in
// acp_test.go; unhandled requests retain the existing server's behavior.
type configServer struct {
	fakeServer
	onMethod      map[string]func(*fakeServer, json.RawMessage) any
	notified      []string
	notifications []json.RawMessage
}

func (fs *configServer) sawNotification(method string) bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, m := range fs.notified {
		if m == method {
			return true
		}
	}
	return false
}

func (fs *configServer) run() {
	for {
		line, err := fs.reqR.ReadBytes('\n')
		if len(line) > 0 {
			var m struct {
				ID     *json.RawMessage `json:"id"`
				Method string           `json:"method"`
				Params json.RawMessage  `json:"params"`
			}
			if json.Unmarshal(line, &m) == nil {
				switch {
				case m.Method != "" && m.ID == nil:
					fs.mu.Lock()
					fs.notified = append(fs.notified, m.Method)
					fs.notifications = append(fs.notifications, append(json.RawMessage(nil), line...))
					fs.mu.Unlock()
				case m.ID != nil && fs.onMethod[m.Method] != nil:
					result := fs.onMethod[m.Method](&fs.fakeServer, m.Params)
					if rpcErr, ok := result.(*rpcError); ok {
						fs.replyErr(*m.ID, rpcErr.Code, rpcErr.Message)
					} else {
						fs.reply(*m.ID, result)
					}
				default:
					fs.handle(line)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func newConfigTestClient(t *testing.T, fs *configServer) *Client {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	fs.reqR, fs.respW = bufio.NewReader(inR), outW
	fs.perms = map[int]chan json.RawMessage{}
	go fs.run()
	c := newClient(inW, outR, func() error {
		_ = inW.Close()
		_ = outW.Close()
		return nil
	}, ".")
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for notification")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestHandshakeAndPrompt(t *testing.T) {
	fs := &fakeServer{
		onPrompt: func(fs *fakeServer, sid, text string) string {
			fs.sendUpdate(sid, map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": "hello"},
			})
			return "end_turn"
		},
	}
	c := newTestClient(t, fs)

	var updates []SessionUpdate
	var mu sync.Mutex
	c.OnSessionUpdate(func(u SessionUpdate) {
		mu.Lock()
		updates = append(updates, u)
		mu.Unlock()
	})

	init, err := c.Initialize(ctx(t))
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if init.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version = %d, want %d", init.ProtocolVersion, ProtocolVersion)
	}

	sid, err := c.NewSession(ctx(t), ".")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sid != "sess-1" {
		t.Fatalf("session id = %q, want sess-1", sid)
	}

	stop, err := c.Prompt(ctx(t), sid, "do the thing")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if stop != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", stop)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(updates) != 1 {
		t.Fatalf("got %d updates, want 1", len(updates))
	}
	if updates[0].SessionID != sid {
		t.Fatalf("update session id = %q, want %q", updates[0].SessionID, sid)
	}
	if updates[0].Kind != "agent_message_chunk" {
		t.Fatalf("update kind = %q, want agent_message_chunk", updates[0].Kind)
	}
}

func TestUsageParsedFromUpdate(t *testing.T) {
	fs := &fakeServer{
		onPrompt: func(fs *fakeServer, sid, text string) string {
			fs.sendUpdate(sid, map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": "hi"},
				"usage":         map[string]any{"inputTokens": 120, "outputTokens": 34, "cost": 0.0021},
			})
			return "end_turn"
		},
	}
	c := newTestClient(t, fs)

	var got *Usage
	var mu sync.Mutex
	c.OnSessionUpdate(func(u SessionUpdate) {
		if u.Usage != nil {
			mu.Lock()
			got = u.Usage
			mu.Unlock()
		}
	})
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	sid, err := c.NewSession(ctx(t), ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Prompt(ctx(t), sid, "go"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("no usage delivered")
	}
	if got.InputTokens != 120 || got.OutputTokens != 34 {
		t.Fatalf("tokens = %d/%d, want 120/34", got.InputTokens, got.OutputTokens)
	}
	if got.Cost != 0.0021 {
		t.Fatalf("cost = %v, want 0.0021", got.Cost)
	}
}

func TestPermissionAutoSelect(t *testing.T) {
	fs := &fakeServer{
		onPrompt: func(fs *fakeServer, sid, text string) string {
			raw := fs.requestPermission(sid, "tc-1", "write file", []map[string]any{
				{"optionId": "allow-1", "kind": "allow_once", "name": "Allow"},
				{"optionId": "deny-1", "kind": "reject_once", "name": "Deny"},
			})
			var resp struct {
				Outcome struct {
					Outcome  string `json:"outcome"`
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				t.Errorf("permission response: %v", err)
			}
			if resp.Outcome.Outcome != "selected" || resp.Outcome.OptionID != "allow-1" {
				t.Errorf("outcome = %+v, want selected/allow-1", resp.Outcome)
			}
			return "end_turn"
		},
	}
	c := newTestClient(t, fs)
	c.OnPermissionRequest(func(req PermissionRequest) PermissionOutcome {
		// approve: choose the allow_once option
		for _, o := range req.Options {
			if o.Kind == "allow_once" {
				return PermissionOutcome{OptionID: o.OptionID}
			}
		}
		return PermissionOutcome{Cancelled: true}
	})
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	sid, _ := c.NewSession(ctx(t), ".")
	if _, err := c.Prompt(ctx(t), sid, "go"); err != nil {
		t.Fatal(err)
	}
}

func TestPermissionDefaultCancel(t *testing.T) {
	fs := &fakeServer{
		onPrompt: func(fs *fakeServer, sid, text string) string {
			raw := fs.requestPermission(sid, "tc-1", "write file", []map[string]any{
				{"optionId": "allow-1", "kind": "allow_once"},
			})
			var resp struct {
				Outcome struct {
					Outcome string `json:"outcome"`
				} `json:"outcome"`
			}
			_ = json.Unmarshal(raw, &resp)
			if resp.Outcome.Outcome != "cancelled" {
				t.Errorf("outcome = %q, want cancelled (no handler)", resp.Outcome.Outcome)
			}
			return "end_turn"
		},
	}
	c := newTestClient(t, fs) // no OnPermissionRequest registered
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	sid, _ := c.NewSession(ctx(t), ".")
	if _, err := c.Prompt(ctx(t), sid, "go"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSessionSupported(t *testing.T) {
	fs := &fakeServer{loadSession: true}
	c := newTestClient(t, fs)
	init, err := c.Initialize(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if !init.LoadSession {
		t.Fatal("expected LoadSession capability advertised")
	}
	if err := c.LoadSession(ctx(t), "sess-1"); err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
}

func TestLoadSessionUnsupported(t *testing.T) {
	fs := &fakeServer{loadSession: false}
	c := newTestClient(t, fs)
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadSession(ctx(t), "sess-1"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("LoadSession err = %v, want ErrNotSupported", err)
	}
}

func TestLoadSessionServerError(t *testing.T) {
	fs := &fakeServer{loadSession: true, loadErr: true}
	c := newTestClient(t, fs)
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadSession(ctx(t), "sess-1"); err == nil {
		t.Fatal("expected error from session/load, got nil")
	}
}

func TestMalformedFrameSkipped(t *testing.T) {
	fs := &fakeServer{
		onPrompt: func(fs *fakeServer, sid, text string) string {
			fs.sendRaw("this is not json {{{")
			fs.sendUpdate(sid, map[string]any{"sessionUpdate": "agent_message_chunk"})
			return "end_turn"
		},
	}
	c := newTestClient(t, fs)

	var count int
	var mu sync.Mutex
	c.OnSessionUpdate(func(u SessionUpdate) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	sid, _ := c.NewSession(ctx(t), ".")
	stop, err := c.Prompt(ctx(t), sid, "go")
	if err != nil {
		t.Fatalf("Prompt after malformed frame: %v", err)
	}
	if stop != "end_turn" {
		t.Fatalf("stop = %q, want end_turn", stop)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("delivered %d updates, want 1 (malformed skipped)", count)
	}
}

func TestPendingFailOnTransportClose(t *testing.T) {
	// A server that never answers session/prompt, then the transport closes:
	// the blocked Prompt call must unblock with an error, and later calls fail.
	fs := &fakeServer{
		onPrompt: func(fs *fakeServer, sid, text string) string {
			// Close the client's stdout so the reader hits EOF while Prompt waits.
			if pc, ok := fs.respW.(io.Closer); ok {
				_ = pc.Close()
			}
			select {} // never reply
		},
	}
	c := newTestClient(t, fs)
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	sid, _ := c.NewSession(ctx(t), ".")
	if _, err := c.Prompt(ctx(t), sid, "go"); err == nil {
		t.Fatal("expected Prompt to fail when transport closed, got nil")
	}
	// Subsequent calls fail immediately (dead client).
	if _, err := c.NewSession(ctx(t), "."); err == nil {
		t.Fatal("expected NewSession to fail on dead client, got nil")
	}
}

func TestSpawnProcessDeathFailsPending(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	// A real subprocess that reads nothing and exits immediately: its stdout
	// closes, the reader hits EOF, and the client must go dead so calls error
	// instead of hanging.
	c, err := Spawn(context.Background(), sh, []string{"-c", "exit 0"}, nil, ".")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer c.Close()
	// Initialize will never get a response; it must fail (dead client), not hang.
	if _, err := c.Initialize(ctx(t)); err == nil {
		t.Fatal("expected Initialize to fail against a dead subprocess, got nil")
	}
}

func TestContextCancelUnblocksCall(t *testing.T) {
	// A server that accepts the prompt but never answers: a cancelled context
	// must unblock the call with ctx.Err(), leaving the client usable-dead-free.
	fs := &fakeServer{
		onPrompt: func(fs *fakeServer, sid, text string) string {
			select {} // never reply
		},
	}
	c := newTestClient(t, fs)
	if _, err := c.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	sid, _ := c.NewSession(ctx(t), ".")
	cctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := c.Prompt(cctx, sid, "go")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Prompt err = %v, want context.Canceled", err)
	}
}

func TestFilteredEnvironDropsPactifySecrets(t *testing.T) {
	t.Setenv("PACT_RELAY_TOKEN", "super-secret")
	t.Setenv("PACTIFY_HOME", "/some/home")
	t.Setenv("PATH", "/usr/bin:/bin")

	has := func(key string) bool {
		for _, e := range filteredEnviron() {
			if strings.HasPrefix(e, key+"=") {
				return true
			}
		}
		return false
	}

	if has("PACT_RELAY_TOKEN") {
		t.Error("PACT_RELAY_TOKEN should be filtered out")
	}
	if has("PACTIFY_HOME") {
		t.Error("PACTIFY_HOME should be filtered out")
	}
	if !has("PATH") {
		t.Error("PATH should be preserved")
	}
}

func TestPerSessionUpdateRouting(t *testing.T) {
	c := newClient(discardWriteCloser{}, blockingReader{}, func() error { return nil }, ".")
	defer c.Close()

	var got []string
	var mu sync.Mutex
	record := func(name string) func(SessionUpdate) {
		return func(u SessionUpdate) {
			mu.Lock()
			got = append(got, name+":"+string(u.SessionID))
			mu.Unlock()
		}
	}

	c.OnSessionUpdate(record("global"))
	c.OnSessionUpdateFor("A", record("A"))
	c.OnSessionUpdateFor("B", record("B"))

	c.dispatchUpdate(SessionUpdate{SessionID: "A", Kind: "k"})
	c.dispatchUpdate(SessionUpdate{SessionID: "B", Kind: "k"})
	c.dispatchUpdate(SessionUpdate{SessionID: "C", Kind: "k"})

	mu.Lock()
	defer mu.Unlock()
	want := []string{"A:A", "B:B", "global:C"}
	if !slicesEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPerSessionUpdateClearSession(t *testing.T) {
	c := newClient(discardWriteCloser{}, blockingReader{}, func() error { return nil }, ".")
	defer c.Close()

	var got []string
	var mu sync.Mutex
	record := func(name string) func(SessionUpdate) {
		return func(u SessionUpdate) {
			mu.Lock()
			got = append(got, name+":"+string(u.SessionID))
			mu.Unlock()
		}
	}

	c.OnSessionUpdate(record("global"))
	c.OnSessionUpdateFor("A", record("A"))

	c.dispatchUpdate(SessionUpdate{SessionID: "A", Kind: "k"})
	c.ClearSession("A")
	c.dispatchUpdate(SessionUpdate{SessionID: "A", Kind: "k"})

	mu.Lock()
	defer mu.Unlock()
	want := []string{"A:A", "global:A"}
	if !slicesEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPerSessionUpdateGlobalOnlyRegression(t *testing.T) {
	c := newClient(discardWriteCloser{}, blockingReader{}, func() error { return nil }, ".")
	defer c.Close()

	var got []string
	var mu sync.Mutex
	c.OnSessionUpdate(func(u SessionUpdate) {
		mu.Lock()
		got = append(got, "global:"+string(u.SessionID))
		mu.Unlock()
	})

	c.dispatchUpdate(SessionUpdate{SessionID: "A", Kind: "k"})
	c.dispatchUpdate(SessionUpdate{SessionID: "B", Kind: "k"})

	mu.Lock()
	defer mu.Unlock()
	want := []string{"global:A", "global:B"}
	if !slicesEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPerSessionPermissionRouting(t *testing.T) {
	cw := newNotifyingWriter()
	c := newClient(cw, blockingReader{}, func() error { return nil }, ".")
	defer c.Close()

	choose := func(name string) func(PermissionRequest) PermissionOutcome {
		return func(req PermissionRequest) PermissionOutcome {
			return PermissionOutcome{OptionID: name}
		}
	}

	c.OnPermissionRequest(choose("global"))
	c.OnPermissionRequestFor("A", choose("A"))
	c.OnPermissionRequestFor("B", choose("B"))

	cases := []struct {
		sid  string
		want string
	}{
		{"A", "A"},
		{"B", "B"},
		{"C", "global"},
	}

	for i, tc := range cases {
		cw.Reset()
		id := json.RawMessage(fmt.Sprintf(`%d`, i+1))
		params := fmt.Sprintf(`{"sessionId":%q,"toolCall":{"toolCallId":"tc","title":"t"},"options":[]}`, tc.sid)
		c.handleServerRequest(id, "session/request_permission", json.RawMessage(params))

		resp := cw.ReadResponse(t)
		if resp.Result.Outcome.Outcome != "selected" || resp.Result.Outcome.OptionID != tc.want {
			t.Fatalf("sid %s: outcome = %s/%s, want selected/%s", tc.sid, resp.Result.Outcome.Outcome, resp.Result.Outcome.OptionID, tc.want)
		}
	}
}

func TestPerSessionPermissionClearSession(t *testing.T) {
	cw := newNotifyingWriter()
	c := newClient(cw, blockingReader{}, func() error { return nil }, ".")
	defer c.Close()

	c.OnPermissionRequest(func(req PermissionRequest) PermissionOutcome {
		return PermissionOutcome{OptionID: "global"}
	})
	c.OnPermissionRequestFor("A", func(req PermissionRequest) PermissionOutcome {
		return PermissionOutcome{OptionID: "A"}
	})

	run := func(want string) {
		t.Helper()
		cw.Reset()
		c.handleServerRequest(json.RawMessage(`1`), "session/request_permission", json.RawMessage(`{"sessionId":"A","toolCall":{"toolCallId":"tc","title":"t"},"options":[]}`))
		resp := cw.ReadResponse(t)
		if resp.Result.Outcome.OptionID != want {
			t.Fatalf("optionId = %q, want %q", resp.Result.Outcome.OptionID, want)
		}
	}

	run("A")
	c.ClearSession("A")
	run("global")
}

func TestPerSessionPermissionDefaultCancel(t *testing.T) {
	cw := newNotifyingWriter()
	c := newClient(cw, blockingReader{}, func() error { return nil }, ".")
	defer c.Close()

	c.handleServerRequest(json.RawMessage(`1`), "session/request_permission", json.RawMessage(`{"sessionId":"X","toolCall":{"toolCallId":"tc","title":"t"},"options":[]}`))

	resp := cw.ReadResponse(t)
	if resp.Result.Outcome.Outcome != "cancelled" {
		t.Fatalf("outcome = %q, want cancelled", resp.Result.Outcome.Outcome)
	}
}

// helpers for per-session routing tests

type blockingReader struct{}

func (blockingReader) Read(p []byte) (int, error) {
	select {}
}

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }

type notifyingWriter struct {
	mu  sync.Mutex
	buf *bytes.Buffer
	ch  chan struct{}
}

func newNotifyingWriter() *notifyingWriter {
	return &notifyingWriter{
		buf: &bytes.Buffer{},
		ch:  make(chan struct{}, 1),
	}
}

func (w *notifyingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buf.Write(p)
	w.mu.Unlock()
	select {
	case w.ch <- struct{}{}:
	default:
	}
	return n, err
}

func (w *notifyingWriter) Close() error { return nil }

func (w *notifyingWriter) Reset() {
	w.mu.Lock()
	w.buf.Reset()
	w.mu.Unlock()
}

type permissionResponse struct {
	Result struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	} `json:"result"`
}

func (w *notifyingWriter) ReadResponse(t *testing.T) permissionResponse {
	t.Helper()
	select {
	case <-w.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission response")
	}
	w.mu.Lock()
	data := append([]byte(nil), w.buf.Bytes()...)
	w.mu.Unlock()

	var resp permissionResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
