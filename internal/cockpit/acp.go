package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/agentjoey/pactify/internal/acp"
)

// NewACPBackend returns a cockpit.Backend that drives an ACP-compatible agent.
// Supported kinds are "kimi-cli", "gemini-cli" and "opencode".
func NewACPBackend(kind string) Backend {
	return newACPBackend(kind)
}

func newACPBackend(kind string) Backend {
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

	client, err := acp.Spawn(ctx, cmd, args, nil, opts.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("acp spawn %q: %w", b.kind, err)
	}

	if _, err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("acp initialize %q: %w", b.kind, err)
	}

	sid, err := client.NewSession(ctx, opts.RepoDir)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("acp new session %q: %w", b.kind, err)
	}

	sess := newACPSession(client, sid)
	return sess, nil
}

func (b *acpBackend) Resume(ctx context.Context, threadID string) (Session, error) {
	cmd, args, err := acpCommandFor(b.kind)
	if err != nil {
		return nil, err
	}

	client, err := acp.Spawn(ctx, cmd, args, nil, "")
	if err != nil {
		return nil, fmt.Errorf("acp spawn %q: %w", b.kind, err)
	}

	if _, err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("acp initialize %q: %w", b.kind, err)
	}

	sid := acp.SessionID(threadID)
	if err := client.LoadSession(ctx, sid); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("acp load session %q: %w", b.kind, err)
	}

	sess := newACPSession(client, sid)
	return sess, nil
}

// acpCommandFor maps a cockpit seat kind to its native ACP command.
func acpCommandFor(kind string) (command string, args []string, err error) {
	switch kind {
	case "kimi-cli":
		return "kimi", []string{"acp"}, nil
	case "gemini-cli":
		return "gemini", []string{"--acp"}, nil
	case "opencode":
		return "opencode", []string{"acp"}, nil
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

	closeOnce sync.Once
	closeErr  error
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
		client.OnSessionUpdateFor(sid, func(u acp.SessionUpdate) {
			if e, ok := MapACPUpdateToEvent(u); ok {
				s.dispatchEvent(e)
			}
		})
		client.OnPermissionRequestFor(sid, newPermissionHandler(s.approvals, s.done))
	}

	return s
}

func (s *acpSession) Prompt(ctx context.Context, msg UserMessage) error {
	_, err := s.client.Prompt(ctx, s.sid, msg.Text)
	return err
}

func (s *acpSession) Interrupt(ctx context.Context) error {
	// ACP does not yet expose a session/cancel method; do not fake one.
	return nil
}

func (s *acpSession) Events() <-chan Event              { return s.events }
func (s *acpSession) Approvals() <-chan ApprovalRequest { return s.approvals }
func (s *acpSession) ThreadID() string                  { return string(s.sid) }

func (s *acpSession) Close() error {
	s.closeOnce.Do(func() {
		if s.client != nil {
			s.client.ClearSession(s.sid)
			s.closeErr = s.client.Close()
		}
		close(s.done)
		close(s.events)
		close(s.approvals)
	})
	return s.closeErr
}

func (s *acpSession) dispatchEvent(e Event) {
	select {
	case s.events <- e:
	default:
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
