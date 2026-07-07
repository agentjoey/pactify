package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// NewCodexBackend returns a Backend that spawns a local `codex app-server`
// subprocess and speaks JSON-RPC 2.0 over stdio (newline-delimited JSON).
func NewCodexBackend() Backend {
	return &codexBackend{}
}

type codexBackend struct{}

// Start spawns codex app-server, performs initialize/initialized, and starts a
// new thread rooted at opts.RepoDir.
func (b *codexBackend) Start(ctx context.Context, opts StartOpts) (Session, error) {
	return newCodexSession(ctx, opts.RepoDir, "")
}

// Resume spawns codex app-server and resumes an existing thread.
func (b *codexBackend) Resume(ctx context.Context, threadID string) (Session, error) {
	return newCodexSession(ctx, "", threadID)
}

// codexSession owns one codex app-server process and its JSON-RPC connection.
type codexSession struct {
	client *codexClient

	threadID string

	events    chan Event
	approvals chan ApprovalRequest

	closeOnce sync.Once
	closeErr  error
}

func newCodexSession(ctx context.Context, repoDir, resumeThreadID string) (*codexSession, error) {
	s := &codexSession{
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 16),
	}

	cmd := exec.CommandContext(ctx, "codex", "app-server")
	cmd.Dir = repoDir
	cmd.Env = filteredEnviron()
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { killGroup(cmd); return nil }

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex: start app-server: %w", err)
	}

	s.client = newCodexClient(stdin, stdout, func() error {
		_ = stdin.Close()
		killGroup(cmd)
		return nil
	}, s.dispatchEvent, s.dispatchApproval)

	go func() {
		_ = cmd.Wait()
		s.client.terminate(errors.New("codex: app-server process exited"))
	}()

	if _, err := s.client.initialize(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("codex: initialize: %w", err)
	}

	if err := s.client.initialized(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("codex: initialized: %w", err)
	}

	if resumeThreadID != "" {
		if err := s.client.threadResume(ctx, resumeThreadID); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("codex: thread/resume: %w", err)
		}
		s.threadID = resumeThreadID
	} else {
		id, err := s.client.threadStart(ctx, repoDir)
		if err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("codex: thread/start: %w", err)
		}
		s.threadID = id
	}

	return s, nil
}

func (s *codexSession) Prompt(ctx context.Context, msg UserMessage) error {
	return s.client.turnStart(ctx, s.threadID, msg.Text)
}

func (s *codexSession) Interrupt(ctx context.Context) error {
	return s.client.turnInterrupt(ctx, s.threadID)
}

func (s *codexSession) Events() <-chan Event { return s.events }

func (s *codexSession) Approvals() <-chan ApprovalRequest { return s.approvals }

func (s *codexSession) ThreadID() string { return s.threadID }

func (s *codexSession) Close() error {
	s.closeOnce.Do(func() {
		if s.client != nil {
			s.closeErr = s.client.Close()
		}
		close(s.events)
		close(s.approvals)
	})
	return s.closeErr
}

func (s *codexSession) dispatchEvent(e Event) {
	select {
	case s.events <- e:
	default:
	}
}

func (s *codexSession) dispatchApproval(a ApprovalRequest) {
	select {
	case s.approvals <- a:
	default:
	}
}

// newCodexSessionWithClient wires a codexSession around an already-constructed
// client. It is used by pipe-backed tests so they can drive the JSON-RPC layer
// without spawning a real subprocess.
func newCodexSessionWithClient(c *codexClient, threadID string) *codexSession {
	return &codexSession{
		client:    c,
		threadID:  threadID,
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 16),
	}
}

// codexClient is the internal JSON-RPC/stdio client.
type codexClient struct {
	w       io.WriteCloser
	r       io.Reader
	closeFn func() error

	writeCh chan []byte
	done    chan struct{}
	once    sync.Once

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcResponse
	dead    bool
	deadErr error

	dispatchEvent    func(Event)
	dispatchApproval func(ApprovalRequest)
}

type rpcResponse struct {
	Result json.RawMessage
	Err    *rpcError
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("codex: rpc error %d: %s", e.Code, e.Message)
}

func newCodexClient(w io.WriteCloser, r io.Reader, closeFn func() error,
	onEvent func(Event), onApproval func(ApprovalRequest)) *codexClient {
	c := &codexClient{
		w:                w,
		r:                r,
		closeFn:          closeFn,
		writeCh:          make(chan []byte),
		done:             make(chan struct{}),
		pending:          map[int]chan rpcResponse{},
		dispatchEvent:    onEvent,
		dispatchApproval: onApproval,
		// Request ids MUST start at 1: id 0 is dropped by the `id,omitempty` JSON
		// tag, so codex would see the request as an id-less notification and never
		// reply — the initialize call would then hang until the ctx deadline.
		nextID: 1,
	}
	go c.writeLoop()
	go c.readLoop()
	return c
}

func (c *codexClient) initialize(ctx context.Context) (json.RawMessage, error) {
	params := map[string]any{
		"clientInfo": map[string]any{
			"name":    "pactify",
			"version": "0",
			"title":   "pactify",
		},
	}
	return c.call(ctx, "initialize", params)
}

func (c *codexClient) initialized(ctx context.Context) error {
	return c.notify("initialized", nil)
}

func (c *codexClient) threadStart(ctx context.Context, cwd string) (string, error) {
	params := map[string]any{"cwd": cwd}
	raw, err := c.call(ctx, "thread/start", params)
	if err != nil {
		return "", err
	}
	var res struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("parse thread/start result: %w", err)
	}
	return res.Thread.ID, nil
}

func (c *codexClient) threadResume(ctx context.Context, threadID string) error {
	return c.notify("thread/resume", map[string]any{"threadId": threadID})
}

func (c *codexClient) turnStart(ctx context.Context, threadID, text string) error {
	params := map[string]any{
		"threadId": threadID,
		"input":    []map[string]any{{"type": "text", "text": text}},
	}
	_, err := c.call(ctx, "turn/start", params)
	return err
}

func (c *codexClient) turnInterrupt(ctx context.Context, threadID string) error {
	params := map[string]any{"threadId": threadID}
	return c.notify("turn/interrupt", params)
}

func (c *codexClient) Close() error {
	c.terminate(errors.New("codex: client closed"))
	return nil
}

func (c *codexClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
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

	b, err := json.Marshal(request{Jsonrpc: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		c.dropPending(id)
		return nil, fmt.Errorf("codex: marshal %s: %w", method, err)
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

func (c *codexClient) notify(method string, params any) error {
	b, err := json.Marshal(request{Jsonrpc: "2.0", Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("codex: marshal %s: %w", method, err)
	}
	return c.write(b)
}

func (c *codexClient) dropPending(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *codexClient) write(b []byte) error {
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

func (c *codexClient) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case b := <-c.writeCh:
			frame := append(b, '\n')
			if _, err := c.w.Write(frame); err != nil {
				c.terminate(fmt.Errorf("codex: write: %w", err))
				return
			}
		}
	}
}

func (c *codexClient) readLoop() {
	br := bufio.NewReader(c.r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			c.dispatch(line)
		}
		if err != nil {
			c.terminate(fmt.Errorf("codex: connection closed: %w", err))
			return
		}
	}
}

type request struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func (c *codexClient) dispatch(line []byte) {
	var m struct {
		ID     *json.RawMessage `json:"id"`
		Method string           `json:"method"`
		Params json.RawMessage  `json:"params"`
		Result json.RawMessage  `json:"result"`
		Error  *rpcError        `json:"error"`
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}

	switch {
	case m.Method != "" && m.ID != nil:
		c.handleServerRequest(*m.ID, m.Method, m.Params)
	case m.Method != "":
		c.handleNotification(m.Method, m.Params)
	case m.ID != nil:
		var id int
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

func (c *codexClient) handleServerRequest(id json.RawMessage, method string, params json.RawMessage) {
	switch method {
	case "item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"item/permissions/requestApproval":
		c.handleApprovalRequest(id, method, params)
	}
}

func (c *codexClient) handleApprovalRequest(id json.RawMessage, method string, params json.RawMessage) {
	kind := approvalKindFromMethod(method)

	var p struct {
		Command string `json:"command"`
		Tool    string `json:"tool"`
		Name    string `json:"name"`
	}
	_ = json.Unmarshal(params, &p)

	toolName := p.Command
	if toolName == "" {
		toolName = p.Tool
	}
	if toolName == "" {
		toolName = p.Name
	}

	var once sync.Once
	var innerErr error
	req := ApprovalRequest{
		Kind:     kind,
		ToolName: toolName,
		RawInput: params,
		Respond: func(d Decision) error {
			triggered := false
			once.Do(func() {
				triggered = true
				innerErr = c.replyApproval(id, d)
			})
			if !triggered {
				return errors.New("approval already responded")
			}
			return innerErr
		},
	}

	if c.dispatchApproval != nil {
		c.dispatchApproval(req)
	}
}

func (c *codexClient) replyApproval(id json.RawMessage, d Decision) error {
	decision := decisionString(d)
	if decision == "" {
		return fmt.Errorf("codex: unsupported decision %q", d)
	}
	b, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{"decision": decision},
	})
	if err != nil {
		return err
	}
	return c.write(b)
}

func approvalKindFromMethod(method string) string {
	switch {
	case strings.HasSuffix(method, "commandExecution/requestApproval"):
		return "command"
	case strings.HasSuffix(method, "fileChange/requestApproval"):
		return "file_change"
	case strings.HasSuffix(method, "permissions/requestApproval"):
		return "permission"
	}
	return ""
}

func decisionString(d Decision) string {
	switch d {
	case DecisionAllow:
		return "accept"
	case DecisionAllowForSession:
		return "acceptForSession"
	case DecisionDeny:
		return "decline"
	}
	return ""
}

func (c *codexClient) handleNotification(method string, params json.RawMessage) {
	if c.dispatchEvent == nil {
		return
	}

	raw := cloneRaw(params)

	switch method {
	case "item/agentMessage/delta":
		text := jsonStringAt(params, "delta", "text")
		c.dispatchEvent(Event{Kind: EventMessage, Text: text, Final: false, Raw: raw})

	case "item/completed":
		itemType := jsonStringAt(params, "item", "type")
		switch itemType {
		case "agentMessage":
			c.dispatchEvent(Event{Kind: EventMessage, Final: true, Raw: raw})
		case "commandExecution", "fileChange", "mcpTool":
			name := jsonStringAt(params, "item", "command")
			if name == "" {
				name = jsonStringAt(params, "item", "name")
			}
			if name == "" {
				name = jsonStringAt(params, "item", "path")
			}
			c.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: name}, Raw: raw})
		}

	case "item/commandExecution/outputDelta":
		text := jsonStringAt(params, "delta", "text")
		c.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "output", Text: text}, Raw: raw})

	case "item/started":
		itemType := jsonStringAt(params, "item", "type")
		if itemType == "commandExecution" || itemType == "fileChange" || itemType == "mcpTool" {
			name := jsonStringAt(params, "item", "command")
			if name == "" {
				name = jsonStringAt(params, "item", "name")
			}
			if name == "" {
				name = jsonStringAt(params, "item", "path")
			}
			c.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: name}, Raw: raw})
		}

	case "thread/tokenUsage/updated":
		if u := parseUsageFromParams(params); u != nil {
			c.dispatchEvent(Event{Kind: EventUsage, Usage: u, Raw: raw})
		}

	case "turn/started":
		c.dispatchEvent(Event{Kind: EventState, State: "turn_started", Raw: raw})

	case "turn/completed":
		c.dispatchEvent(Event{Kind: EventState, State: "turn_completed", Raw: raw})
		if u := parseUsageFromParams(params); u != nil {
			c.dispatchEvent(Event{Kind: EventUsage, Usage: u, Raw: raw})
		}

	case "turn/failed", "error":
		summary := jsonStringAt(params, "error", "message")
		if summary == "" {
			summary = jsonStringAt(params, "message")
		}
		c.dispatchEvent(Event{Kind: EventError, Err: summary, Raw: raw})

	case "turn/diff/updated":
		c.dispatchEvent(Event{Kind: EventDiff, Raw: raw})

	case "turn/plan/updated":
		c.dispatchEvent(Event{Kind: EventPlan, Raw: raw})
	}
}

func cloneRaw(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(r))
	copy(out, r)
	return out
}

func jsonStringAt(params json.RawMessage, keys ...string) string {
	if len(params) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(params, &v); err != nil {
		return ""
	}
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		child, ok := m[k]
		if !ok {
			return ""
		}
		switch x := child.(type) {
		case string:
			return x
		case map[string]any:
			v = x
		default:
			return ""
		}
	}
	return ""
}

func parseUsageFromParams(params json.RawMessage) *Usage {
	if len(params) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(params, &m); err != nil {
		return nil
	}
	u := &Usage{
		InputTokens:  firstInt(m, "inputTokens", "input_tokens", "promptTokens", "prompt_tokens"),
		OutputTokens: firstInt(m, "outputTokens", "output_tokens", "completionTokens", "completion_tokens"),
		TotalTokens:  firstInt(m, "totalTokens", "total_tokens"),
		CostUSD:      firstFloat(m, "costUsd", "cost_usd", "cost", "totalCost", "total_cost"),
	}
	if u.InputTokens == 0 && u.OutputTokens == 0 && u.TotalTokens == 0 && u.CostUSD == 0 {
		return nil
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

func (c *codexClient) terminate(err error) {
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

// filteredEnviron returns the current environment with pactify/relay secrets
// removed. A denylist is used so that vendor authentication (PATH, HOME, shell
// config, etc.) still reaches the child process.
func filteredEnviron() []string {
	var out []string
	for _, e := range os.Environ() {
		if key, _, ok := strings.Cut(e, "="); ok {
			if key == "PACT_RELAY_TOKEN" || strings.HasPrefix(key, "PACTIFY_") {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return
	}
	_ = cmd.Process.Kill()
}
