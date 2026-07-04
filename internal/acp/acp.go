// Package acp is a Go client for the Agent Client Protocol (ACP) — JSON-RPC 2.0
// spoken to a stdio subprocess over newline-delimited JSON. It owns one agent
// subprocess and its connection: requests are correlated by id, notifications
// (session/update) and server→client requests (session/request_permission) are
// dispatched to registered handlers. When the process exits or its stdio closes,
// every pending call fails and the client goes permanently dead.
//
// Types are deliberately minimal — only the fields this project needs — with
// json.RawMessage catching the rest. ACP is still evolving: parse liberally,
// emit conservatively. Ported (in spirit, not line-for-line) from the TypeScript
// reference in opencode-remote-control's core/agent (acp-connect / acp-backend).
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// ProtocolVersion is the ACP protocol version this client advertises in the
// initialize handshake.
const ProtocolVersion = 1

// ErrNotSupported is returned by operations the connected server did not advertise
// support for (e.g. LoadSession when initialize reported no loadSession capability).
var ErrNotSupported = errors.New("acp: operation not supported by server")

// SessionID identifies an ACP session (created by NewSession, reusable via LoadSession).
type SessionID string

// StopReason is why a prompt turn ended (e.g. "end_turn", "max_tokens", "cancelled").
type StopReason string

// Usage is the token/cost accounting parsed from a session/update or prompt
// response when it carries a usage field. Zero value = no usage reported.
type Usage struct {
	InputTokens  int
	OutputTokens int
	Cost         float64
}

// SessionUpdate is one session/update notification, surfaced to the OnSessionUpdate
// handler. Kind is the update discriminator ("agent_message_chunk", "tool_call",
// "plan", …); Usage is populated when the update carried usage; Raw is the full
// update object for downstream normalization of fields this package doesn't model.
type SessionUpdate struct {
	SessionID SessionID
	Kind      string
	Usage     *Usage
	Raw       json.RawMessage
}

// PermissionOption is one choice the server offers for a permission request.
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}

// PermissionToolCall identifies the tool call a permission request is about.
type PermissionToolCall struct {
	ToolCallID string `json:"toolCallId"`
	Title      string `json:"title"`
}

// PermissionRequest is a server→client session/request_permission request. The
// registered handler returns a PermissionOutcome, which becomes the response.
type PermissionRequest struct {
	SessionID SessionID
	ToolCall  PermissionToolCall
	Options   []PermissionOption
	Raw       json.RawMessage
}

// PermissionOutcome is a handler's answer to a PermissionRequest: either a chosen
// option or a cancellation. Use Selected to allow/deny by option; leave OptionID
// empty (or set Cancelled) to cancel.
type PermissionOutcome struct {
	OptionID  string
	Cancelled bool
}

// InitializeResult holds the negotiated handshake. AgentCapabilities and any
// unmodeled fields are preserved raw. LoadSession reports whether the server
// advertised the session/load capability.
type InitializeResult struct {
	ProtocolVersion   int
	AgentCapabilities json.RawMessage
	AuthMethods       []AuthMethod
	LoadSession       bool
}

// AuthMethod is one advertised authentication method from initialize.
type AuthMethod struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Client owns one ACP subprocess and the JSON-RPC connection over its stdio. It
// is safe for concurrent use: a single writer goroutine serializes outbound
// frames (via writeCh) and a single reader goroutine dispatches inbound frames.
type Client struct {
	w       io.WriteCloser
	closeFn func() error
	cwd     string

	writeCh chan []byte
	done    chan struct{}
	once    sync.Once

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResponse
	dead    bool
	deadErr error

	loadSession bool

	hmu      sync.Mutex
	onUpdate func(SessionUpdate)
	onPerm   func(PermissionRequest) PermissionOutcome
}

// rpcResponse is the result/error delivered to a pending caller.
type rpcResponse struct {
	Result json.RawMessage
	Err    *rpcError
}

// rpcError is a JSON-RPC 2.0 error object; it implements error.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("acp: rpc error %d: %s", e.Code, e.Message)
}

// Spawn starts command with args in dir, appends env onto the inherited
// environment, and takes over the child's stdin/stdout for the JSON-RPC
// connection. The child's stderr streams to the parent (so agent diagnostics stay
// visible). The returned Client is live immediately; when the process exits its
// stdout closes and the client goes dead, failing all pending and future calls.
func Spawn(ctx context.Context, command string, args, env []string, dir string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start %q: %w", command, err)
	}
	c := newClient(stdin, stdout, func() error {
		_ = stdin.Close()
		// Killing the process (if still running) unblocks Wait and reaps it.
		_ = cmd.Process.Kill()
		return nil
	}, dir)
	// Reap the process; its exit closes stdout, so the reader loop already
	// terminates the client — this just ensures no zombie and covers the case
	// where the process dies without closing stdout cleanly.
	go func() {
		werr := cmd.Wait()
		msg := "acp: agent process exited"
		if werr != nil {
			msg = fmt.Sprintf("acp: agent process exited: %v", werr)
		}
		c.terminate(errors.New(msg))
	}()
	return c, nil
}

// newClient builds a Client over an already-connected transport: w is the frame
// sink (child stdin), r is the frame source (child stdout), closeFn tears the
// transport down, and cwd is the default working directory (used by LoadSession).
// It starts the reader and writer goroutines. This is the seam the tests inject a
// pipe-backed fake server through, keeping Spawn's os/exec path out of the tests.
func newClient(w io.WriteCloser, r io.Reader, closeFn func() error, cwd string) *Client {
	c := &Client{
		w:       w,
		closeFn: closeFn,
		cwd:     cwd,
		writeCh: make(chan []byte),
		done:    make(chan struct{}),
		pending: map[int64]chan rpcResponse{},
	}
	go c.writeLoop()
	go c.readLoop(r)
	return c
}

// OnSessionUpdate registers the handler for session/update notifications. Only the
// most recently registered handler receives updates; nil clears it.
func (c *Client) OnSessionUpdate(fn func(SessionUpdate)) {
	c.hmu.Lock()
	c.onUpdate = fn
	c.hmu.Unlock()
}

// OnPermissionRequest registers the handler for server→client
// session/request_permission requests. The handler's returned PermissionOutcome
// becomes the response. With no handler registered, requests are auto-cancelled.
func (c *Client) OnPermissionRequest(fn func(PermissionRequest) PermissionOutcome) {
	c.hmu.Lock()
	c.onPerm = fn
	c.hmu.Unlock()
}

// Initialize performs the ACP handshake, negotiating protocol version and
// capabilities, and records whether session/load is supported.
func (c *Client) Initialize(ctx context.Context) (InitializeResult, error) {
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "pactify", "version": "0"},
	}
	raw, err := c.call(ctx, "initialize", params)
	if err != nil {
		return InitializeResult{}, err
	}
	var wire struct {
		ProtocolVersion   int             `json:"protocolVersion"`
		AgentCapabilities json.RawMessage `json:"agentCapabilities"`
		AuthMethods       []AuthMethod    `json:"authMethods"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return InitializeResult{}, fmt.Errorf("acp: initialize result: %w", err)
	}
	load := parseLoadSession(wire.AgentCapabilities)
	c.mu.Lock()
	c.loadSession = load
	c.mu.Unlock()
	return InitializeResult{
		ProtocolVersion:   wire.ProtocolVersion,
		AgentCapabilities: wire.AgentCapabilities,
		AuthMethods:       wire.AuthMethods,
		LoadSession:       load,
	}, nil
}

// parseLoadSession reads the loadSession capability flag from an agentCapabilities
// object, defaulting to false for absent/malformed input.
func parseLoadSession(caps json.RawMessage) bool {
	if len(caps) == 0 {
		return false
	}
	var c struct {
		LoadSession bool `json:"loadSession"`
	}
	_ = json.Unmarshal(caps, &c)
	return c.LoadSession
}

// NewSession creates a fresh session rooted at cwd and returns its id. cwd also
// becomes the client's default working directory for a later LoadSession.
func (c *Client) NewSession(ctx context.Context, cwd string) (SessionID, error) {
	if cwd != "" {
		c.cwd = cwd
	}
	raw, err := c.call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return "", err
	}
	var wire struct {
		SessionID SessionID `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return "", fmt.Errorf("acp: session/new result: %w", err)
	}
	return wire.SessionID, nil
}

// LoadSession re-establishes a previously-created session so the agent resumes its
// context. It returns ErrNotSupported when the server did not advertise the
// loadSession capability at initialize.
func (c *Client) LoadSession(ctx context.Context, id SessionID) error {
	c.mu.Lock()
	supported := c.loadSession
	c.mu.Unlock()
	if !supported {
		return ErrNotSupported
	}
	_, err := c.call(ctx, "session/load", map[string]any{
		"sessionId":  id,
		"cwd":        c.cwd,
		"mcpServers": []any{},
	})
	return err
}

// Prompt sends text as the user turn for session sid and blocks until the turn
// ends, returning the stop reason. Usage carried on the prompt response is
// surfaced through the OnSessionUpdate handler (as an update with Usage set).
func (c *Client) Prompt(ctx context.Context, sid SessionID, text string) (StopReason, error) {
	params := map[string]any{
		"sessionId": sid,
		"prompt":    []map[string]any{{"type": "text", "text": text}},
	}
	raw, err := c.call(ctx, "session/prompt", params)
	if err != nil {
		return "", err
	}
	var wire struct {
		StopReason StopReason      `json:"stopReason"`
		Usage      json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return "", fmt.Errorf("acp: session/prompt result: %w", err)
	}
	if u := parseUsage(wire.Usage); u != nil {
		c.dispatchUpdate(SessionUpdate{SessionID: sid, Kind: "usage", Usage: u, Raw: raw})
	}
	return wire.StopReason, nil
}

// Close tears down the connection and marks the client dead. Idempotent.
func (c *Client) Close() error {
	c.terminate(errors.New("acp: client closed"))
	return nil
}

// call sends a request and blocks for its response, the context's cancellation, or
// the client dying — whichever comes first.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.dead {
		err := c.deadErr
		c.mu.Unlock()
		return nil, err
	}
	id := c.nextID
	c.nextID++
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	b, err := json.Marshal(request{Jsonrpc: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		c.dropPending(id)
		return nil, fmt.Errorf("acp: marshal %s: %w", method, err)
	}
	if err := c.write(b); err != nil {
		c.dropPending(id)
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.dropPending(id)
		return nil, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		err := c.deadErr
		c.mu.Unlock()
		return nil, err
	case resp := <-ch:
		if resp.Err != nil {
			return nil, resp.Err
		}
		return resp.Result, nil
	}
}

// request is an outbound JSON-RPC request/notification (ID nil → notification).
type request struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func (c *Client) dropPending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// write hands a frame to the writer goroutine, or fails if the client is dead.
func (c *Client) write(b []byte) error {
	select {
	case <-c.done:
		c.mu.Lock()
		err := c.deadErr
		c.mu.Unlock()
		return err
	case c.writeCh <- b:
		return nil
	}
}

// writeLoop is the sole writer: it appends the newline frame delimiter and writes
// each queued frame to the child's stdin in order.
func (c *Client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case b := <-c.writeCh:
			frame := append(b, '\n')
			if _, err := c.w.Write(frame); err != nil {
				c.terminate(fmt.Errorf("acp: write: %w", err))
				return
			}
		}
	}
}

// readLoop is the sole reader: it splits the child's stdout into ndjson frames and
// dispatches each. On EOF/read error it terminates the client. Frames are read
// with ReadBytes so arbitrarily long updates aren't truncated.
func (c *Client) readLoop(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			c.dispatch(line)
		}
		if err != nil {
			c.terminate(fmt.Errorf("acp: connection closed: %w", err))
			return
		}
	}
}

// dispatch routes one inbound frame. A malformed frame is skipped (never fatal).
func (c *Client) dispatch(line []byte) {
	var m struct {
		ID     *json.RawMessage `json:"id"`
		Method string           `json:"method"`
		Params json.RawMessage  `json:"params"`
		Result json.RawMessage  `json:"result"`
		Error  *rpcError        `json:"error"`
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return // malformed frame — skip, don't crash
	}
	switch {
	case m.Method != "" && m.ID != nil:
		c.handleServerRequest(*m.ID, m.Method, m.Params)
	case m.Method != "":
		c.handleNotification(m.Method, m.Params)
	case m.ID != nil:
		var id int64
		if json.Unmarshal(*m.ID, &id) != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- rpcResponse{Result: m.Result, Err: m.Error}
		}
	}
}

// handleNotification dispatches server notifications. Only session/update is
// modeled; others are ignored.
func (c *Client) handleNotification(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var p struct {
		SessionID SessionID       `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	var meta struct {
		SessionUpdate string          `json:"sessionUpdate"`
		Usage         json.RawMessage `json:"usage"`
	}
	_ = json.Unmarshal(p.Update, &meta)
	c.dispatchUpdate(SessionUpdate{
		SessionID: p.SessionID,
		Kind:      meta.SessionUpdate,
		Usage:     parseUsage(meta.Usage),
		Raw:       p.Update,
	})
}

func (c *Client) dispatchUpdate(u SessionUpdate) {
	c.hmu.Lock()
	fn := c.onUpdate
	c.hmu.Unlock()
	if fn != nil {
		fn(u)
	}
}

// handleServerRequest answers a server→client request. session/request_permission
// runs the registered handler (default: cancel) in its own goroutine so the reader
// keeps draining, then replies with the outcome.
func (c *Client) handleServerRequest(id json.RawMessage, method string, params json.RawMessage) {
	if method != "session/request_permission" {
		return
	}
	var p struct {
		SessionID SessionID          `json:"sessionId"`
		ToolCall  PermissionToolCall `json:"toolCall"`
		Options   []PermissionOption `json:"options"`
	}
	_ = json.Unmarshal(params, &p)
	req := PermissionRequest{
		SessionID: p.SessionID,
		ToolCall:  p.ToolCall,
		Options:   p.Options,
		Raw:       params,
	}
	go func() {
		c.hmu.Lock()
		fn := c.onPerm
		c.hmu.Unlock()
		outcome := PermissionOutcome{Cancelled: true}
		if fn != nil {
			outcome = fn(req)
		}
		c.replyPermission(id, outcome)
	}()
}

// replyPermission writes the response to a permission request with the same id.
func (c *Client) replyPermission(id json.RawMessage, o PermissionOutcome) {
	var outcome map[string]any
	if o.Cancelled || o.OptionID == "" {
		outcome = map[string]any{"outcome": "cancelled"}
	} else {
		outcome = map[string]any{"outcome": "selected", "optionId": o.OptionID}
	}
	b, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{"outcome": outcome},
	})
	if err != nil {
		return
	}
	_ = c.write(b)
}

// terminate marks the client dead (once), unblocks every pending and future call
// with err, stops the loops, and tears the transport down.
func (c *Client) terminate(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.dead = true
		c.deadErr = err
		c.mu.Unlock()
		close(c.done)
		if c.closeFn != nil {
			_ = c.closeFn()
		}
	})
}

// parseUsage extracts a Usage from a usage object, tolerating the common field
// name variants ACP agents emit. Returns nil when no usage is present.
func parseUsage(raw json.RawMessage) *Usage {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return nil
	}
	u := &Usage{
		InputTokens:  firstInt(m, "inputTokens", "input_tokens", "promptTokens", "prompt_tokens"),
		OutputTokens: firstInt(m, "outputTokens", "output_tokens", "completionTokens", "completion_tokens"),
		Cost:         firstFloat(m, "cost", "totalCost", "total_cost"),
	}
	return u
}

func firstInt(m map[string]json.RawMessage, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			var n int
			if json.Unmarshal(v, &n) == nil {
				return n
			}
		}
	}
	return 0
}

func firstFloat(m map[string]json.RawMessage, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			var f float64
			if json.Unmarshal(v, &f) == nil {
				return f
			}
		}
	}
	return 0
}
