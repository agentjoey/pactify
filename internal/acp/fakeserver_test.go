package acp

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeServer is an in-process ACP agent used by the tests. It speaks the same
// newline-delimited JSON-RPC 2.0 the real vendor CLIs do, over two io.Pipe pairs,
// so the whole client path is exercised with zero dependency on a real CLI.
//
// It reads client requests from reqR and writes server messages to respW. Prompt
// handling runs in its own goroutine so the read loop keeps draining while the
// server awaits the client's permission response (mirroring how the real client
// dispatches server→client requests concurrently).
type fakeServer struct {
	reqR  *bufio.Reader
	respW io.Writer

	loadSession bool // advertise the session/load capability in initialize
	loadErr     bool // session/load responds with a JSON-RPC error
	authMethod  string

	// onPrompt drives one turn: it may emit updates / request permission via the
	// fs helpers, then returns the stop reason for the session/prompt response.
	onPrompt func(fs *fakeServer, sid, text string) string

	wmu    sync.Mutex // serializes writes to respW
	mu     sync.Mutex
	nextID int
	perms  map[int]chan json.RawMessage
}

func (fs *fakeServer) run() {
	if fs.perms == nil {
		fs.perms = map[int]chan json.RawMessage{}
	}
	for {
		line, err := fs.reqR.ReadBytes('\n')
		if len(line) > 0 {
			fs.handle(line)
		}
		if err != nil {
			return
		}
	}
}

func (fs *fakeServer) handle(line []byte) {
	var m struct {
		ID     *json.RawMessage `json:"id"`
		Method string           `json:"method"`
		Params json.RawMessage  `json:"params"`
		Result json.RawMessage  `json:"result"`
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}
	// A response to one of our server→client requests (permission).
	if m.Method == "" && m.ID != nil {
		var id int
		if json.Unmarshal(*m.ID, &id) == nil {
			fs.mu.Lock()
			ch := fs.perms[id]
			delete(fs.perms, id)
			fs.mu.Unlock()
			if ch != nil {
				ch <- m.Result
			}
		}
		return
	}
	switch m.Method {
	case "initialize":
		caps := map[string]any{"loadSession": fs.loadSession}
		res := map[string]any{
			"protocolVersion":   ProtocolVersion,
			"agentCapabilities": caps,
		}
		if fs.authMethod != "" {
			res["authMethods"] = []map[string]any{{"id": fs.authMethod}}
		}
		fs.reply(*m.ID, res)
	case "session/new":
		fs.reply(*m.ID, map[string]any{"sessionId": "sess-1"})
	case "session/load":
		if fs.loadErr {
			fs.replyErr(*m.ID, -32000, "session expired")
			return
		}
		fs.reply(*m.ID, map[string]any{})
	case "session/prompt":
		var p struct {
			SessionID string `json:"sessionId"`
			Prompt    []struct {
				Text string `json:"text"`
			} `json:"prompt"`
		}
		_ = json.Unmarshal(m.Params, &p)
		text := ""
		if len(p.Prompt) > 0 {
			text = p.Prompt[0].Text
		}
		id := *m.ID
		go func() {
			stop := "end_turn"
			if fs.onPrompt != nil {
				stop = fs.onPrompt(fs, p.SessionID, text)
			}
			fs.reply(id, map[string]any{"stopReason": stop})
		}()
	}
}

// writeMsg marshals v and writes it as one ndjson frame.
func (fs *fakeServer) writeMsg(v any) {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	fs.wmu.Lock()
	_, _ = fs.respW.Write(b)
	fs.wmu.Unlock()
}

func (fs *fakeServer) reply(id json.RawMessage, result any) {
	fs.writeMsg(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (fs *fakeServer) replyErr(id json.RawMessage, code int, msg string) {
	fs.writeMsg(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

// sendUpdate emits a session/update notification carrying the given update object.
func (fs *fakeServer) sendUpdate(sid string, update map[string]any) {
	fs.writeMsg(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params":  map[string]any{"sessionId": sid, "update": update},
	})
}

// sendRaw writes a raw line verbatim (used to inject malformed frames).
func (fs *fakeServer) sendRaw(line string) {
	fs.wmu.Lock()
	_, _ = io.WriteString(fs.respW, line+"\n")
	fs.wmu.Unlock()
}

// requestPermission issues a server→client session/request_permission request and
// blocks until the client responds, returning the raw response result.
func (fs *fakeServer) requestPermission(sid, toolCallID, title string, options []map[string]any) json.RawMessage {
	fs.mu.Lock()
	id := fs.nextID
	fs.nextID++
	ch := make(chan json.RawMessage, 1)
	fs.perms[id] = ch
	fs.mu.Unlock()
	fs.writeMsg(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": sid,
			"toolCall":  map[string]any{"toolCallId": toolCallID, "title": title},
			"options":   options,
		},
	})
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		return nil
	}
}

// newTestClient wires a Client to an in-process fakeServer over two pipes and
// starts both loops. It returns the client and the server; the client is closed
// via t.Cleanup.
func newTestClient(t *testing.T, fs *fakeServer) *Client {
	t.Helper()
	// client stdin (client writes → server reads)
	inR, inW := io.Pipe()
	// client stdout (server writes → client reads)
	outR, outW := io.Pipe()
	fs.reqR = bufio.NewReader(inR)
	fs.respW = outW
	go fs.run()
	c := newClient(inW, outR, func() error {
		_ = inW.Close()
		_ = outW.Close()
		return nil
	}, ".")
	t.Cleanup(func() { _ = c.Close() })
	return c
}
