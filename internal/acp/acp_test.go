package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

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
