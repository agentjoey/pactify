package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
)

// NewClaudeBackend returns a Backend that spawns the local
// `bridge/claude-host/host.mjs` Node bridge and speaks JSON-RPC 2.0 over stdio.
func NewClaudeBackend() Backend {
	return &claudeBackend{}
}

type claudeBackend struct{}

// Start spawns the Claude host bridge, initializes it, and starts a new thread.
func (b *claudeBackend) Start(ctx context.Context, opts StartOpts) (Session, error) {
	hostPath, err := findClaudeHost()
	if err != nil {
		return nil, err
	}
	return spawnClaudeSession(ctx, hostPath, opts)
}

// Resume spawns the Claude host bridge and resumes an existing thread.
func (b *claudeBackend) Resume(ctx context.Context, threadID string) (Session, error) {
	hostPath, err := findClaudeHost()
	if err != nil {
		return nil, err
	}
	return spawnClaudeSession(ctx, hostPath, StartOpts{RepoDir: ""}, threadID)
}

// claudeSession owns one Claude host process and its JSON-RPC connection.
type claudeSession struct {
	rpc *rpcConn
	cmd *exec.Cmd

	mu       sync.RWMutex
	threadID string

	events    chan Event
	approvals chan ApprovalRequest

	closeOnce sync.Once
	closeErr  error
}

func (s *claudeSession) Prompt(ctx context.Context, msg UserMessage) error {
	if s.rpc == nil {
		return errors.New("claude: session closed")
	}
	params := map[string]any{
		"threadId": s.ThreadID(),
		"input":    []map[string]any{{"type": "text", "text": msg.Text}},
	}
	_, err := s.rpc.call(ctx, "turn/start", params)
	return err
}

func (s *claudeSession) Interrupt(ctx context.Context) error {
	if s.rpc == nil {
		return errors.New("claude: session closed")
	}
	return s.rpc.notify("turn/interrupt", map[string]any{"threadId": s.ThreadID()})
}

func (s *claudeSession) Events() <-chan Event { return s.events }

func (s *claudeSession) Approvals() <-chan ApprovalRequest { return s.approvals }

func (s *claudeSession) ThreadID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.threadID
}

func (s *claudeSession) Close() error {
	s.closeOnce.Do(func() {
		if s.rpc != nil {
			s.closeErr = s.rpc.Close()
		}
		if s.cmd != nil {
			killGroup(s.cmd)
		}
		close(s.events)
		close(s.approvals)
	})
	return s.closeErr
}

func (s *claudeSession) dispatchEvent(e Event) {
	select {
	case s.events <- e:
	default:
	}
}

func (s *claudeSession) dispatchApproval(a ApprovalRequest) {
	select {
	case s.approvals <- a:
	default:
	}
}

func (s *claudeSession) setThreadID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threadID = id
}

// handleRequest processes server-to-client requests from the host.
func (s *claudeSession) handleRequest(id json.RawMessage, method string, params json.RawMessage) {
	switch method {
	case "approval/request":
		s.handleApprovalRequest(id, params)
	}
}

func (s *claudeSession) handleApprovalRequest(id json.RawMessage, params json.RawMessage) {
	var p struct {
		ToolName string `json:"toolName"`
	}
	_ = json.Unmarshal(params, &p)

	var once sync.Once
	var innerErr error
	req := ApprovalRequest{
		Kind:     "tool",
		ToolName: p.ToolName,
		RawInput: cloneRaw(params),
		Respond: func(d Decision) error {
			triggered := false
			once.Do(func() {
				triggered = true
				decision := "deny"
				if d == DecisionAllow {
					decision = "allow"
				}
				innerErr = s.rpc.reply(id, map[string]any{"decision": decision})
			})
			if !triggered {
				return errors.New("approval already responded")
			}
			return innerErr
		},
	}

	s.dispatchApproval(req)
}

// handleNotification maps host notifications to cockpit events.
func (s *claudeSession) handleNotification(method string, params json.RawMessage) {
	raw := cloneRaw(params)

	switch method {
	case "cockpit/session":
		var p struct {
			ThreadID string `json:"threadId"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			s.setThreadID(p.ThreadID)
		}

	case "cockpit/message":
		var p struct {
			Text  string `json:"text"`
			Final bool   `json:"final"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			s.dispatchEvent(Event{Kind: EventMessage, Text: p.Text, Final: p.Final, Raw: raw})
		}

	case "cockpit/tool":
		var p struct {
			Phase string `json:"phase"`
			Name  string `json:"name"`
			Text  string `json:"text"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			s.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: p.Phase, Name: p.Name, Text: p.Text}, Raw: raw})
		}

	case "cockpit/usage":
		var p struct {
			InputTokens  int     `json:"inputTokens"`
			OutputTokens int     `json:"outputTokens"`
			TotalTokens  int     `json:"totalTokens"`
			CostUSD      float64 `json:"costUSD"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			s.dispatchEvent(Event{Kind: EventUsage, Usage: &Usage{
				InputTokens:  p.InputTokens,
				OutputTokens: p.OutputTokens,
				TotalTokens:  p.TotalTokens,
				CostUSD:      p.CostUSD,
			}, Raw: raw})
		}

	case "cockpit/state":
		var p struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			s.dispatchEvent(Event{Kind: EventState, State: p.State, Raw: raw})
		}

	case "cockpit/error":
		var p struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			s.dispatchEvent(Event{Kind: EventError, Err: p.Error, Raw: raw})
		}
	}
}

func findClaudeHost() (string, error) {
	// Explicit override wins (tests, non-standard layouts).
	if envPath := os.Getenv("PACT_CLAUDE_HOST"); envPath != "" {
		if info, err := os.Stat(envPath); err == nil && !info.IsDir() {
			return envPath, nil
		}
		return "", fmt.Errorf("PACT_CLAUDE_HOST points to missing file %q; run `pactify doctor --setup-bridge`", envPath)
	}

	// Walk upward from the executable looking for the bundled bridge. Terminate at
	// the filesystem root: filepath.Dir("/") == "/", so the loop must stop when the
	// parent stops changing — otherwise it spins forever statting the same path.
	if exe, err := os.Executable(); err == nil {
		for dir := filepath.Dir(exe); ; {
			candidate := filepath.Join(dir, "bridge", "claude-host", "host.mjs")
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return "", errors.New("claude host bridge not found (bridge/claude-host/host.mjs); run `pactify doctor --setup-bridge`")
}

func spawnClaudeSession(ctx context.Context, hostPath string, opts StartOpts, resumeThreadID ...string) (*claudeSession, error) {
	cmd := exec.CommandContext(ctx, "node", hostPath)
	cmd.Dir = opts.RepoDir
	cmd.Env = filteredEnviron()
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { killGroup(cmd); return nil }

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("claude: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude: start host: %w", err)
	}

	s := &claudeSession{
		cmd:       cmd,
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 16),
	}

	closeFn := func() error {
		_ = stdin.Close()
		killGroup(cmd)
		return nil
	}
	s.rpc = newRPCConn(stdin, stdout, closeFn, s.handleRequest, s.handleNotification)

	go func() {
		_ = cmd.Wait()
		s.rpc.terminate(errors.New("claude: host process exited"))
	}()

	var resume string
	if len(resumeThreadID) > 0 {
		resume = resumeThreadID[0]
	}
	if err := s.handshake(ctx, opts.RepoDir, opts.Model, opts.SystemPrompt, resume); err != nil {
		_ = s.Close()
		return nil, err
	}

	return s, nil
}

func (s *claudeSession) handshake(ctx context.Context, repoDir, model, systemPrompt, resumeThreadID string) error {
	if _, err := s.rpc.call(ctx, "initialize", nil); err != nil {
		return fmt.Errorf("claude: initialize: %w", err)
	}

	if resumeThreadID != "" {
		raw, err := s.rpc.call(ctx, "thread/resume", map[string]any{"threadId": resumeThreadID})
		if err != nil {
			return fmt.Errorf("claude: thread/resume: %w", err)
		}
		var res struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return fmt.Errorf("claude: parse thread/resume result: %w", err)
		}
		s.setThreadID(res.Thread.ID)
		return nil
	}

	params := map[string]any{"cwd": repoDir}
	if model != "" {
		params["model"] = model
	}
	if systemPrompt != "" {
		params["systemPrompt"] = systemPrompt
	}
	if _, err := s.rpc.call(ctx, "thread/start", params); err != nil {
		return fmt.Errorf("claude: thread/start: %w", err)
	}
	return nil
}

// newClaudeSessionForTest dials a fake host through the provided pipes and is
// only used by tests.
func newClaudeSessionForTest(ctx context.Context, w io.WriteCloser, r io.Reader, closeFn func() error, repoDir string) (*claudeSession, error) {
	s := &claudeSession{
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 16),
	}
	s.rpc = newRPCConn(w, r, closeFn, s.handleRequest, s.handleNotification)
	if err := s.handshake(ctx, repoDir, "", "", ""); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}
