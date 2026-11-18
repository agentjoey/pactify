package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// Cursor sends blocking extension requests that other ACP agents never send. An
// unanswered one stalls the turn, so the client must always reply.
func TestClientAnswersCursorExtensionRequests(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := newClient(inW, outR, func() error { _ = inW.Close(); return outW.Close() }, ".")
	t.Cleanup(func() { _ = c.Close() })
	server := bufio.NewReader(inR)
	send := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = outW.Write(append(b, '\n'))
	}
	read := func() map[string]any {
		line, err := server.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	// The pipes are unbuffered, so the server sends while the test reads replies.
	go func() {
		for id, method := range map[int]string{1: "cursor/ask_question", 2: "cursor/create_plan", 3: "cursor/task"} {
			send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": map[string]any{}})
		}
		// A request from another agent keeps its existing behavior: no reply.
		send(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "fs/read_text_file", "params": map[string]any{}})
	}()
	replies := map[int]map[string]any{}
	for range 3 {
		m := read()
		replies[int(m["id"].(float64))] = m
	}
	for _, id := range []int{1, 2} {
		outcome := replies[id]["result"].(map[string]any)["outcome"].(map[string]any)["outcome"]
		if outcome != "cancelled" {
			t.Fatalf("id %d outcome = %v", id, outcome)
		}
	}
	if code := replies[3]["error"].(map[string]any)["code"]; code != float64(-32601) {
		t.Fatalf("unsupported cursor method code = %v", code)
	}
}

func TestClientAuthenticate(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := newClient(inW, outR, func() error { _ = inW.Close(); return outW.Close() }, ".")
	t.Cleanup(func() { _ = c.Close() })
	server := bufio.NewReader(inR)
	got := make(chan map[string]any, 1)
	go func() {
		line, _ := server.ReadBytes('\n')
		var m map[string]any
		_ = json.Unmarshal(line, &m)
		got <- m
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{}})
		_, _ = outW.Write(append(b, '\n'))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Authenticate(ctx, "cursor_login"); err != nil {
		t.Fatal(err)
	}
	m := <-got
	if m["method"] != "authenticate" || m["params"].(map[string]any)["methodId"] != "cursor_login" {
		t.Fatalf("request = %v", m)
	}
}

// Without credentials Cursor never answers authenticate; a bounded context
// must release the caller.
func TestClientAuthenticateHonorsContext(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := newClient(inW, outR, func() error { _ = inW.Close(); return outW.Close() }, ".")
	t.Cleanup(func() { _ = c.Close() })
	go func() { _, _ = io.Copy(io.Discard, inR) }()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.Authenticate(ctx, "cursor_login"); err == nil {
		t.Fatal("silent agent returned success")
	}
}
