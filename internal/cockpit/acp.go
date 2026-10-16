package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/agent"
)

// NewACPBackend returns a cockpit.Backend that drives an ACP-compatible agent.
// Kimi uses ACP; opencode is routed to its v1 HTTP/SSE backend.
func NewACPBackend(kind string) Backend {
	return newACPBackend(kind)
}

func newACPBackend(kind string) Backend {
	if kind == "opencode" {
		return NewOpencodeBackend()
	}
	return &acpBackend{kind: kind}
}

type acpBackend struct {
	kind string
}

func (b *acpBackend) Start(ctx context.Context, opts StartOpts) (Session, error) {
	cmd, args, err := acpCommandFor(b.kind)
	if err != nil {
		return nil, err
	}

	var observe func(string, []byte)
	if opts.Transcript != nil {
		observe = opts.Transcript.Observe
	}
	client, err := acp.SpawnObserved(ctx, cmd, args, opts.Env, opts.RepoDir, observe, opts.Stderr)
	if err != nil {
		return nil, fmt.Errorf("acp spawn %q: %w", b.kind, err)
	}

	if _, err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("acp initialize %q: %w", b.kind, err)
	}

	res, err := client.NewSessionDetailed(ctx, opts.RepoDir)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("acp new session %q: %w", b.kind, err)
	}

	var info SessionInfo
	info, err = applyACPOpts(ctx, client, res.SessionID, res, opts)
	if err != nil {
		_ = client.Close()
		return nil, &StartError{ID: string(res.SessionID), Err: fmt.Errorf("acp configure %q: %w", b.kind, err)}
	}
	sess := newACPSession(client, res.SessionID)
	sess.info = info
	return sess, nil
}

func (b *acpBackend) Resume(ctx context.Context, threadID string) (Session, error) {
	return b.ResumeWith(ctx, threadID, StartOpts{})
}

func (b *acpBackend) ResumeWith(ctx context.Context, threadID string, opts StartOpts) (Session, error) {
	cmd, args, err := acpCommandFor(b.kind)
	if err != nil {
		return nil, err
	}

	var observe func(string, []byte)
	if opts.Transcript != nil {
		observe = opts.Transcript.Observe
	}
	client, err := acp.SpawnObserved(ctx, cmd, args, opts.Env, opts.RepoDir, observe, opts.Stderr)
	if err != nil {
		return nil, fmt.Errorf("acp spawn %q: %w", b.kind, err)
	}

	if _, err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("acp initialize %q: %w", b.kind, err)
	}

	sid := acp.SessionID(threadID)
	var info SessionInfo
	if loadErr := client.LoadSession(ctx, sid); loadErr != nil {
		_ = client.Close()
		return nil, fmt.Errorf("acp load session %q: %w", b.kind, loadErr)
	}
	info, err = applyACPResumeOpts(ctx, client, sid, opts)

	if err != nil {
		_ = client.Close()
		return nil, &StartError{ID: threadID, Err: fmt.Errorf("acp configure resumed %q: %w", b.kind, err)}
	}
	sess := newACPSession(client, sid)
	sess.info = info
	return sess, nil
}

// acpCommandFor maps a cockpit seat kind to its native ACP command.
func acpCommandFor(kind string) (command string, args []string, err error) {
	if err := agent.RemovedKindError(kind); err != nil {
		return "", nil, err
	}
	switch kind {
	case "kimi-cli":
		return "kimi", []string{"acp"}, nil
	default:
		return "", nil, fmt.Errorf("acp: unsupported kind %q", kind)
	}
}

// acpSession wraps an acp.Client for one ACP session.
type acpSession struct {
	client *acp.Client
	sid    acp.SessionID

	events    chan Event
	approvals chan ApprovalRequest
	done      chan struct{}

	closeOnce  sync.Once
	closeErr   error
	dispatchMu sync.RWMutex
	closed     bool
	info       SessionInfo
	textMu     sync.Mutex
	text       strings.Builder
	promptFn   func(context.Context, string) (acp.StopReason, error)
	cancelFn   func() error
}

func newACPSession(client *acp.Client, sid acp.SessionID) *acpSession {

	s := &acpSession{
		client:    client,
		sid:       sid,
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 16),
		done:      make(chan struct{}),
	}

	if client != nil {
		s.promptFn = func(ctx context.Context, text string) (acp.StopReason, error) { return client.Prompt(ctx, sid, text) }
		s.cancelFn = func() error { return client.Cancel(sid) }
		client.OnSessionUpdateFor(sid, func(u acp.SessionUpdate) {
			if e, ok := MapACPUpdateToEvent(u); ok {
				s.dispatchEvent(e)
			}
		})
		handler := newPermissionHandler(s.approvals, s.done)
		client.OnPermissionRequestFor(sid, func(req acp.PermissionRequest) acp.PermissionOutcome {
			s.dispatchMu.RLock()
			defer s.dispatchMu.RUnlock()
			if s.closed {
				return acp.PermissionOutcome{Cancelled: true}
			}
			return handler(req)
		})
	}

	return s
}

func (s *acpSession) Prompt(ctx context.Context, msg UserMessage) error {
	s.textMu.Lock()
	s.text.Reset()
	s.textMu.Unlock()
	stop, err := s.promptFn(ctx, msg.Text)
	e := Event{Kind: EventState, State: StateTurnCompleted, Text: string(stop)}
	if err != nil {
		e = Event{Kind: EventState, State: StateTurnFailed, Err: err.Error()}
		var coded interface{ RPCCode() int }
		if errors.As(err, &coded) {
			e.Raw, _ = json.Marshal(map[string]any{"code": coded.RPCCode(), "data": rpcErrorData(err)})
		}
	}
	if err == nil && stop != "end_turn" {
		e = Event{Kind: EventState, State: StateTurnFailed, Err: "ACP stopped: " + string(stop)}
		e.Raw, _ = json.Marshal(map[string]any{"stopReason": stop})
	}
	s.textMu.Lock()
	text := s.text.String()
	s.textMu.Unlock()
	if text != "" {
		e.Text, e.Final = text, true
	}
	s.dispatchEvent(e)
	return err
}

func (s *acpSession) Interrupt(ctx context.Context) error { return s.cancelFn() }
func (s *acpSession) Events() <-chan Event                { return s.events }
func (s *acpSession) Approvals() <-chan ApprovalRequest   { return s.approvals }
func (s *acpSession) ThreadID() string                    { return string(s.sid) }
func (s *acpSession) Info() SessionInfo                   { return s.info }

func (s *acpSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.client != nil {
			s.client.ClearSession(s.sid)
			s.closeErr = s.client.Close()
		}
		s.dispatchMu.Lock()
		s.closed = true
		close(s.events)
		close(s.approvals)
		s.dispatchMu.Unlock()
	})
	return s.closeErr
}
func (s *acpSession) dispatchEvent(e Event) {
	s.dispatchMu.RLock()
	defer s.dispatchMu.RUnlock()
	if s.closed {
		return
	}
	if e.Kind == EventTool && e.Tool != nil && e.Tool.Phase == "start" {
		s.textMu.Lock()
		s.text.Reset()
		s.textMu.Unlock()
	}
	if e.Kind == EventMessage && !e.Final {
		s.textMu.Lock()
		s.text.WriteString(e.Text)
		s.textMu.Unlock()
	}
	select {
	case s.events <- e:
	case <-s.done:
	}
}

// MapACPUpdateToEvent converts an ACP session/update into a cockpit Event.
// The second result is false when the update should be ignored.
func MapACPUpdateToEvent(u acp.SessionUpdate) (Event, bool) {
	raw := cloneRaw(u.Raw)

	if u.Usage != nil {
		return Event{
			Kind: EventUsage,
			Usage: &Usage{
				InputTokens:  u.Usage.InputTokens,
				OutputTokens: u.Usage.OutputTokens,
				TotalTokens:  u.Usage.InputTokens + u.Usage.OutputTokens,
				CostUSD:      u.Usage.Cost,
			},
			Raw: raw,
		}, true
	}

	switch u.Kind {
	case "agent_message_chunk":
		return Event{Kind: EventMessage, Text: parseACPMessageText(u.Raw), Raw: raw}, true
	case "tool_call":
		return Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: parseACPToolName(u.Raw)}, Raw: raw}, true
	case "tool_call_update":
		return Event{Kind: EventTool, Tool: &ToolEvent{Phase: "output", Name: parseACPToolName(u.Raw)}, Raw: raw}, true
	case "plan":
		return Event{Kind: EventPlan, Raw: raw}, true
	default:
		return Event{Kind: EventState, State: u.Kind, Raw: raw}, true
	}
}

func parseACPMessageText(raw json.RawMessage) string {
	var p struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ""
	}
	if p.Content.Type != "" && p.Content.Type != "text" {
		return ""
	}
	return p.Content.Text
}

func parseACPToolName(raw json.RawMessage) string {
	var p struct {
		Title string `json:"title"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ""
	}
	if p.Title != "" {
		return p.Title
	}
	return p.Kind
}

// newPermissionHandler builds the ACP OnPermissionRequest callback that bridges
// ACP's synchronous outcome model to cockpit's async ApprovalRequest/Respond.
// The returned handler runs in the acp reader goroutine; it blocks only on the
// approvals channel (buffered) and on the response channel, so the reader loop
// keeps draining.
func newPermissionHandler(approvals chan<- ApprovalRequest, done <-chan struct{}) func(acp.PermissionRequest) acp.PermissionOutcome {
	return func(req acp.PermissionRequest) acp.PermissionOutcome {
		resCh := make(chan Decision, 1)
		var once sync.Once

		ar := ApprovalRequest{
			Kind:     "permission",
			ToolName: req.ToolCall.Title,
			RawInput: cloneRaw(req.Raw),
			Respond: func(d Decision) error {
				once.Do(func() { resCh <- d })
				return nil
			},
		}

		select {
		case approvals <- ar:
		case <-done:
			return acp.PermissionOutcome{Cancelled: true}
		}

		select {
		case d := <-resCh:
			return MapDecisionToOutcome(d, req.Options)
		case <-done:
			return acp.PermissionOutcome{Cancelled: true}
		}
	}
}

// MapDecisionToOutcome picks the ACP PermissionOption that best matches a
// cockpit Decision.
func MapDecisionToOutcome(d Decision, opts []acp.PermissionOption) acp.PermissionOutcome {
	switch d {
	case DecisionAllow, DecisionAllowForSession:
		if d == DecisionAllowForSession {
			for _, o := range opts {
				k := strings.ToLower(o.Kind)
				if strings.Contains(k, "always") || strings.Contains(k, "session") {
					return acp.PermissionOutcome{OptionID: o.OptionID}
				}
			}
		}
		for _, o := range opts {
			if strings.HasPrefix(strings.ToLower(o.Kind), "allow") {
				return acp.PermissionOutcome{OptionID: o.OptionID}
			}
		}
		return acp.PermissionOutcome{Cancelled: true}
	case DecisionDeny:
		for _, o := range opts {
			k := strings.ToLower(o.Kind)
			if strings.Contains(k, "reject") || strings.Contains(k, "deny") {
				return acp.PermissionOutcome{OptionID: o.OptionID}
			}
		}
		return acp.PermissionOutcome{Cancelled: true}
	default:
		return acp.PermissionOutcome{Cancelled: true}
	}
}

var ErrModelUnavailable = errors.New("requested model is not offered by this agent")
var ErrModelUnsupported = errors.New("agent exposes no model option over ACP")

type acpConfigurer interface {
	SetConfigOption(context.Context, acp.SessionID, string, string) ([]acp.ConfigOption, error)
	SetMode(context.Context, acp.SessionID, string) error
}

// applyACPOpts applies only explicit overrides and reports confirmed session settings.
func applyACPOpts(ctx context.Context, c acpConfigurer, sid acp.SessionID, res acp.NewSessionResult, opts StartOpts) (SessionInfo, error) {

	info := SessionInfo{Access: res.CurrentModeID}
	if m, ok := acp.FindConfig(res.ConfigOptions, "model"); ok {
		info.Model = m.CurrentValue
	}
	if e, ok := acp.FindConfig(res.ConfigOptions, "thought_level"); ok {
		info.Effort = e.CurrentValue
	}
	if opts.Model != "" {
		m, ok := acp.FindConfig(res.ConfigOptions, "model")
		if !ok {
			return info, fmt.Errorf("%w (asked for %q); available models: none advertised", ErrModelUnsupported, opts.Model)
		}
		if !offers(m, opts.Model) {
			return info, fmt.Errorf("%w: %q; available: %s", ErrModelUnavailable, opts.Model, modelChoices("kimi", opts.Model, values(m)))
		}
		updated, err := c.SetConfigOption(ctx, sid, m.ID, opts.Model)
		if err != nil {
			return info, fmt.Errorf("set model %q: %w; available: %s", opts.Model, err, modelChoices("kimi", opts.Model, values(m)))
		}
		actual, ok := acp.FindConfig(updated, m.ID)
		if !ok || actual.CurrentValue != opts.Model {
			return info, fmt.Errorf("%w: asked for %q, reported %q; available: %s", ErrModelUnavailable, opts.Model, actual.CurrentValue, modelChoices("kimi", opts.Model, values(m)))
		}
		info.Model = actual.CurrentValue
		// Model selection can change the available thought levels.
		for _, o := range updated {
			for i := range res.ConfigOptions {
				if res.ConfigOptions[i].ID == o.ID {
					res.ConfigOptions[i] = o
					break
				}
			}
		}
		if e, ok := acp.FindConfig(res.ConfigOptions, "thought_level"); ok {
			info.Effort = e.CurrentValue
		}
	}
	if opts.Effort != "" {
		e, ok := acp.FindConfig(res.ConfigOptions, "thought_level")
		if !ok || !offers(e, opts.Effort) {
			return info, fmt.Errorf("effort %q is not offered by this agent; available: %s", opts.Effort, strings.Join(values(e), ", "))
		}
		updated, err := c.SetConfigOption(ctx, sid, e.ID, opts.Effort)
		if err != nil {
			return info, fmt.Errorf("set effort: %w", err)
		}
		actual, ok := acp.FindConfig(updated, e.ID)
		if !ok || actual.CurrentValue != opts.Effort {
			return info, fmt.Errorf("set effort: asked for %q, reported %q", opts.Effort, actual.CurrentValue)
		}
		info.Effort = actual.CurrentValue
		if m, ok := acp.FindConfig(updated, "model"); ok {
			info.Model = m.CurrentValue
			if opts.Model != "" && info.Model != opts.Model {
				return info, fmt.Errorf("%w: asked for %q, reported %q after setting effort; available: %s", ErrModelUnavailable, opts.Model, info.Model, modelChoices("kimi", opts.Model, values(m)))
			}
		}
	}
	if opts.Access == AccessReadOnly && hasMode(res.Modes, "plan") {
		if err := c.SetMode(ctx, sid, "plan"); err != nil {
			return info, fmt.Errorf("set plan mode: %w", err)
		}
		info.Access = "plan"
	}
	return info, nil
}
func values(o acp.ConfigOption) []string {
	v := make([]string, 0, len(o.Options))
	for _, option := range o.Options {
		v = append(v, option.Value)
	}
	return v
}
func offers(o acp.ConfigOption, value string) bool {
	for _, option := range o.Options {
		if option.Value == value {
			return true
		}
	}
	return false
}
func hasMode(modes []acp.SessionMode, id string) bool {
	for _, mode := range modes {
		if mode.ID == id {
			return true
		}
	}
	return false
}

// ACP load does not report settings, so explicit overrides use the standard IDs.
func applyACPResumeOpts(ctx context.Context, c acpConfigurer, sid acp.SessionID, opts StartOpts) (SessionInfo, error) {
	info := SessionInfo{}
	for _, setting := range []struct{ id, value string }{{"model", opts.Model}, {"thinking", opts.Effort}} {
		if setting.value == "" {
			continue
		}
		updated, err := c.SetConfigOption(ctx, sid, setting.id, setting.value)
		if err != nil {
			return info, fmt.Errorf("set %s %q: %w; available values not reported by session/load", setting.id, setting.value, err)
		}
		option, ok := acp.FindConfig(updated, setting.id)
		if !ok || option.CurrentValue != setting.value {
			if setting.id == "model" {
				return info, fmt.Errorf("%w: asked for %q, reported %q; available: %s", ErrModelUnavailable, setting.value, option.CurrentValue, modelChoices("kimi", setting.value, values(option)))
			}
			return info, fmt.Errorf("set %s: asked for %q, reported %q", setting.id, setting.value, option.CurrentValue)
		}
		if m, ok := acp.FindConfig(updated, "model"); ok {
			info.Model = m.CurrentValue
			if opts.Model != "" && info.Model != opts.Model {
				return info, fmt.Errorf("%w: asked for %q, reported %q; available: %s", ErrModelUnavailable, opts.Model, info.Model, modelChoices("kimi", opts.Model, values(m)))
			}
		}
		if setting.id == "thinking" {
			info.Effort = option.CurrentValue
		} else if e, ok := acp.FindConfig(updated, "thought_level"); ok {
			info.Effort = e.CurrentValue
		}
	}
	if opts.Access != "" {
		mode := "default"
		if opts.Access == AccessReadOnly {
			mode = "plan"
		}
		if err := c.SetMode(ctx, sid, mode); err != nil {
			return info, fmt.Errorf("set %s mode: %w", mode, err)
		}
		info.Access = mode
	}
	return info, nil
}

// Kimi documents AUTH_REQUIRED (-32000):
// https://github.com/MoonshotAI/kimi-cli/blob/main/docs/en/reference/kimi-acp.md
var kimiFailureRules = []failureRule{
	{"run kimi login first", "auth"},
	{"acp: rpc error -32000:", "auth"},
	{"requested model is not offered by this agent", "model"},
}

func acpFailure(message string, raw json.RawMessage, rules []failureRule) Failure {
	var stop struct {
		StopReason string `json:"stopReason"`
	}
	if json.Unmarshal(raw, &stop) == nil {
		switch stop.StopReason {
		case "cancelled":
			return Failure{Class: "canceled"}
		case "refusal", "max_tokens", "max_turn_requests":
			return Failure{Class: stop.StopReason}
		}
	}
	var rpc struct {
		Code int `json:"code"`
	}
	if json.Unmarshal(raw, &rpc) == nil && rpc.Code == -32000 {
		return Failure{Class: "auth"}
	}
	// -32603 is generic Internal Error: its code alone does not prove transient.
	return matchFailure(message, rules)
}
