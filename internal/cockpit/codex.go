package cockpit

import (
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
		s.client.rpc.terminate(errors.New("codex: app-server process exited"))
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

// codexClient is the thin, codex-specific wrapper around the shared rpcConn.
type codexClient struct {
	rpc *rpcConn

	dispatchEvent    func(Event)
	dispatchApproval func(ApprovalRequest)
}

func newCodexClient(w io.WriteCloser, r io.Reader, closeFn func() error,
	onEvent func(Event), onApproval func(ApprovalRequest)) *codexClient {
	c := &codexClient{
		dispatchEvent:    onEvent,
		dispatchApproval: onApproval,
	}
	c.rpc = newRPCConn(w, r, closeFn, c.handleServerRequest, c.handleNotification)
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
	return c.rpc.call(ctx, "initialize", params)
}

func (c *codexClient) initialized(ctx context.Context) error {
	return c.rpc.notify("initialized", nil)
}

func (c *codexClient) threadStart(ctx context.Context, cwd string) (string, error) {
	params := map[string]any{"cwd": cwd}
	raw, err := c.rpc.call(ctx, "thread/start", params)
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
	return c.rpc.notify("thread/resume", map[string]any{"threadId": threadID})
}

func (c *codexClient) turnStart(ctx context.Context, threadID, text string) error {
	params := map[string]any{
		"threadId": threadID,
		"input":    []map[string]any{{"type": "text", "text": text}},
	}
	_, err := c.rpc.call(ctx, "turn/start", params)
	return err
}

func (c *codexClient) turnInterrupt(ctx context.Context, threadID string) error {
	params := map[string]any{"threadId": threadID}
	return c.rpc.notify("turn/interrupt", params)
}

func (c *codexClient) Close() error {
	return c.rpc.Close()
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
	return c.rpc.reply(id, map[string]any{"decision": decision})
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
