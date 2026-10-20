package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agentjoey/pactify/internal/procx"
	"io"
	"math"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

func NewOpencodeBackend() Backend { return &opencodeBackend{} }

type opencodeBackend struct{}

func (*opencodeBackend) Start(ctx context.Context, opts StartOpts) (Session, error) {
	return newOpencodeSession(ctx, opts, "")
}
func (b *opencodeBackend) Resume(ctx context.Context, id string) (Session, error) {
	return b.ResumeWith(ctx, id, StartOpts{})
}
func (*opencodeBackend) ResumeWith(ctx context.Context, id string, opts StartOpts) (Session, error) {
	return newOpencodeSession(ctx, opts, id)
}

type opencodeTokens struct {
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	Reasoning float64 `json:"reasoning"`
	Cache     struct {
		Read  float64 `json:"read"`
		Write float64 `json:"write"`
	} `json:"cache"`
}
type opencodeSummary struct {
	ID     string          `json:"id"`
	Tokens *opencodeTokens `json:"tokens"`
	Cost   float64         `json:"cost"`
}

func (s opencodeSummary) usageSince(base opencodeSummary) Usage {
	b := opencodeTokens{}
	if base.Tokens != nil {
		b = *base.Tokens
	}
	t := s.Tokens
	in := max(0, t.Input-b.Input) + max(0, t.Cache.Read-b.Cache.Read) + max(0, t.Cache.Write-b.Cache.Write)
	out := max(0, t.Output-b.Output) + max(0, t.Reasoning-b.Reasoning)
	return Usage{InputTokens: int(math.Ceil(in)), OutputTokens: int(math.Ceil(out)), TotalTokens: int(math.Ceil(in)) + int(math.Ceil(out)), CostUSD: max(0, s.Cost-base.Cost)}
}

type opencodeMessageInfo struct {
	ID         string          `json:"id"`
	ParentID   string          `json:"parentID"`
	SessionID  string          `json:"sessionID"`
	Role       string          `json:"role"`
	ProviderID string          `json:"providerID"`
	ModelID    string          `json:"modelID"`
	Error      json.RawMessage `json:"error"`
	Finish     string          `json:"finish"`
}
type opencodePart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	Tool      string `json:"tool"`
	State     struct {
		Status   string          `json:"status"`
		Output   string          `json:"output"`
		Metadata json.RawMessage `json:"metadata,omitempty"`
	} `json:"state"`
}
type opencodeMessage struct {
	Info  opencodeMessageInfo `json:"info"`
	Parts []opencodePart      `json:"parts"`
}
type opencodeEvent struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type opencodeSession struct {
	server     *opencodeServer
	id         string
	opts       StartOpts
	base       opencodeSummary
	children   map[string]opencodeSummary
	mu         sync.Mutex
	info       SessionInfo
	promptMu   sync.Mutex
	events     chan Event
	approvals  chan ApprovalRequest
	done       <-chan struct{}
	lifeCtx    context.Context
	stop       context.CancelFunc
	closeOnce  sync.Once
	dispatchMu sync.RWMutex
	closed     bool
	closeErr   error
}

func newOpencodeSession(ctx context.Context, opts StartOpts, id string) (*opencodeSession, error) {
	server, err := startOpencodeServer(ctx, opts)
	if err != nil {
		return nil, err
	}
	lifeCtx, stop := context.WithCancel(context.Background())
	s := &opencodeSession{lifeCtx: lifeCtx, stop: stop, server: server, id: id, opts: opts, events: make(chan Event, 64), approvals: make(chan ApprovalRequest, 16), done: lifeCtx.Done()}
	fail := func(err error) (*opencodeSession, error) {
		_ = s.Close()
		if s.id != "" {
			return nil, &StartError{ID: s.id, Err: err}
		}
		return nil, err
	}
	if opts.Model == "" && opts.Effort != "" {
		// Resume retains the session's last model unless explicitly overridden.
		if id != "" {
			var history []opencodeMessage
			if err := server.call(ctx, "GET", s.path()+"/message", nil, &history); err != nil {
				return fail(err)
			}
			for _, message := range history {
				if info := message.Info; info.Role == "assistant" && info.ProviderID != "" && info.ModelID != "" {
					opts.Model = info.ProviderID + "/" + info.ModelID
				}
			}
		}
		if opts.Model == "" {
			var config struct{ Model string }
			if err := server.call(ctx, "GET", "/config", nil, &config); err != nil {
				return fail(err)
			}
			opts.Model = config.Model
		}
		if opts.Model == "" {
			return fail(fmt.Errorf("%w: cannot determine the opencode model for --effort; specify --model provider/model", ErrModelUnavailable))
		}
		s.opts.Model = opts.Model
	}
	if opts.Model != "" {
		details, err := server.modelDetails(ctx)
		if err != nil {
			return fail(err)
		}
		if err := validateOpencodeModel(opts.Model, details.Items); err != nil {
			return fail(err)
		}
		if variants := details.ModelEfforts[opts.Model]; opts.Effort != "" && !slices.Contains(variants, opts.Effort) {
			choices := strings.Join(variants, ", ")
			if len(variants) == 0 {
				choices = "none (omit --effort for this model)"
			}
			return fail(fmt.Errorf("%w: --effort %q for %s; valid values: %s", ErrEffortUnavailable, opts.Effort, opts.Model, choices))
		}
	}
	if s.id == "" {
		var session struct {
			ID string `json:"id"`
		}
		permissions := []opencodePermission{{"edit", "*", "ask"}, {"bash", "*", "ask"}}
		// Session rules are evaluated after agent/user permissions in 1.19.
		if opts.Access == AccessReadOnly {
			permissions = opencodeReadonlyPermissions()
		}
		body := map[string]any{"title": "Pactify delegated task"}
		if permissions != nil {
			body["permission"] = permissions
		}
		if err := server.call(ctx, "POST", "/session", body, &session); err != nil {
			return fail(err)
		}
		s.id = session.ID
	}
	if id != "" && opts.Access == AccessReadOnly {
		// PATCH appends these ordered rules after the session's old policy.
		if err := server.call(ctx, "PATCH", s.path(), map[string]any{"permission": opencodeReadonlyPermissions()}, nil); err != nil {
			return fail(err)
		}
	}

	if s.id == "" {
		return fail(fmt.Errorf("opencode returned an empty session id"))
	}
	if err := server.call(ctx, "GET", s.path(), nil, &s.base); err != nil {
		var httpError *opencodeHTTPError
		if id != "" && errors.As(err, &httpError) && httpError.status == 404 {
			err = fmt.Errorf("opencode 1.x session not found; sessions cannot be resumed across opencode major versions: %w", err)
		}
		return fail(err)
	}
	if s.base.Tokens == nil {
		if id != "" {
			var history []opencodeMessage
			if err := server.call(ctx, "GET", s.path()+"/message", nil, &history); err != nil {
				return fail(err)
			}
			for _, message := range history {
				if message.Info.Role == "assistant" {
					return fail(fmt.Errorf("opencode resumed session summary is missing tokens"))
				}
			}
		}
		s.base.Tokens = &opencodeTokens{}
	}
	s.children = map[string]opencodeSummary{}
	if err := s.discoverChildren(ctx, true); err != nil {
		return fail(err)
	}
	s.info.Access = "build"
	if opts.Access == AccessReadOnly {
		s.info.Access = "plan"
	}
	return s, nil
}
func (s *opencodeSession) path() string                      { return "/session/" + url.PathEscape(s.id) }
func (s *opencodeSession) ThreadID() string                  { return s.id }
func (s *opencodeSession) Events() <-chan Event              { return s.events }
func (s *opencodeSession) Approvals() <-chan ApprovalRequest { return s.approvals }
func (s *opencodeSession) Info() SessionInfo                 { s.mu.Lock(); defer s.mu.Unlock(); return s.info }
func (s *opencodeSession) dispatch(e Event) {
	s.dispatchMu.RLock()
	defer s.dispatchMu.RUnlock()
	if s.closed {
		return
	}
	e.Text = s.server.redact(e.Text)
	e.Err = s.server.redact(e.Err)
	if len(e.Raw) > 0 {
		e.Raw = json.RawMessage(s.server.redact(string(e.Raw)))
	}
	if e.Tool != nil {
		tool := *e.Tool
		tool.Text = s.server.redact(tool.Text)
		e.Tool = &tool
	}
	select {
	case s.events <- e:
	case <-s.done:
	}
}
func (s *opencodeSession) Close() error {
	s.closeOnce.Do(func() {
		s.stop()
		s.closeErr = s.server.close()
		s.dispatchMu.Lock()
		s.closed = true
		close(s.events)
		close(s.approvals)
		s.dispatchMu.Unlock()
	})
	return s.closeErr
}
func (s *opencodeSession) Interrupt(ctx context.Context) error {
	s.server.snapshot() // Capture detached tools before abort can reparent them.
	err := s.server.call(ctx, "POST", s.path()+"/abort", map[string]any{}, nil)
	s.server.mu.Lock()
	children := make([]procx.Identity, 0, len(s.server.descendants))
	for child := range s.server.descendants {
		children = append(children, child)
	}
	s.server.mu.Unlock()
	return errors.Join(err, procx.TerminateDescendants("opencode tools", children, 250*time.Millisecond))
}
func (s *opencodeSession) updateInfo(info opencodeMessageInfo) {
	if info.Role != "assistant" || info.ProviderID == "" || info.ModelID == "" {
		return
	}
	s.mu.Lock()
	s.info.Provider = info.ProviderID
	s.info.Model = info.ProviderID + "/" + info.ModelID
	// The 1.x assistant message reports the model but no applied variant.
	// A validated request alone does not establish the effort actually used.
	s.mu.Unlock()
}
func opencodeErrorText(raw json.RawMessage) string {
	var e struct {
		Name string `json:"name"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &e) == nil {
		if e.Data.Message != "" {
			return e.Data.Message
		}
		if e.Name != "" {
			return e.Name
		}
	}
	return "Opencode session failed"
}
func hasOpencodeError(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }

// Prompt subscribes before submitting. A terminal event is published only after
// authoritative messages and the session summary have been read.
func (s *opencodeSession) Prompt(ctx context.Context, msg UserMessage) error {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	old := []opencodeMessage{}
	if err := s.server.call(ctx, "GET", s.path()+"/message", nil, &old); err != nil {
		return s.failPrompt(err)
	}
	seen := map[string]bool{}
	for _, m := range old {
		seen[m.Info.ID] = true
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopClose := context.AfterFunc(s.lifeCtx, cancel)
	defer stopClose()
	res, scan, err := s.openEvents(streamCtx)
	if err != nil {
		return s.failPrompt(err)
	}
	defer func() { _ = res.Body.Close() }()

	body := map[string]any{"agent": s.Info().Access, "parts": []map[string]string{{"type": "text", "text": msg.Text}}}
	if s.opts.Model != "" {
		provider, model, _ := strings.Cut(s.opts.Model, "/")
		body["model"] = map[string]string{"providerID": provider, "modelID": model}
	}
	if s.opts.SystemPrompt != "" {
		body["system"] = s.opts.SystemPrompt
	}
	if s.opts.Effort != "" {
		body["variant"] = s.opts.Effort
	}
	if err := s.server.call(ctx, "POST", s.path()+"/prompt_async", body, nil); err != nil {
		return s.failPrompt(err)
	}
	var failure json.RawMessage
	parts := map[string]string{}
	roles := map[string]string{}
	textParts := map[string]bool{}
	var order []string
	tools := map[string]bool{}

	warnedTruncation := false
	noteTruncation := func(part opencodePart) {
		// Tool metadata is an open extension bag. An unknown flag type must
		// not discard the tool event or alter its lifecycle.
		var metadata struct {
			Truncated bool `json:"truncated"`
		}
		if part.Type == "tool" && !warnedTruncation && json.Unmarshal(part.State.Metadata, &metadata) == nil && metadata.Truncated {
			warnedTruncation = true
			s.dispatch(Event{Kind: EventSystem, State: "warning", Text: "Opencode truncated tool output"})
		}
	}
	permissions := map[string]bool{}
	var streamErr error
	reconnects := 0
	if s.children == nil {
		s.children = map[string]opencodeSummary{}
	}
	for {
		event, err := readOpencodeEvent(scan)
		if err != nil {
			_ = res.Body.Close()
			if streamCtx.Err() != nil {
				streamErr = streamCtx.Err()
				break
			}
			reconnected := false
			for reconnects < 5 {
				delay := 100 * time.Millisecond * time.Duration(1<<reconnects)
				reconnects++
				timer := time.NewTimer(delay)
				select {
				case <-streamCtx.Done():
					timer.Stop()
				case <-timer.C:
				}
				if streamCtx.Err() != nil {
					break
				}
				next, nextScan, openErr := s.openEvents(streamCtx)
				if openErr != nil {
					err = openErr
					continue
				}
				res, scan = next, nextScan
				var statuses map[string]struct {
					Type string `json:"type"`
				}
				if statusErr := s.server.call(streamCtx, "GET", "/session/status", nil, &statuses); statusErr != nil {
					err = statusErr
					_ = res.Body.Close()
					continue
				}
				if childErr := s.discoverChildren(streamCtx, false); childErr != nil {
					err = childErr
					_ = res.Body.Close()
					continue
				}
				reconnected = true
				if status, exists := statuses[s.id]; !exists || status.Type == "idle" {
					streamErr = nil
					goto finishedStream
				}
				// Recover permission requests missed while disconnected.
				var pending []struct {
					ID         string `json:"id"`
					SessionID  string `json:"sessionID"`
					Permission string `json:"permission"`
				}
				if pendingErr := s.server.call(streamCtx, "GET", "/permission", nil, &pending); pendingErr != nil {
					err = pendingErr
					_ = res.Body.Close()
					reconnected = false
					continue
				}
				for _, request := range pending {
					if s.owns(request.SessionID) && !permissions[request.ID] {
						permissions[request.ID] = true
						raw, _ := json.Marshal(request)
						s.dispatch(Event{Kind: EventSystem, State: "permission.asked", Raw: raw})
						s.permission(request.ID, request.Permission, raw)
					}
				}
				break
			}
			if !reconnected {
				streamErr = fmt.Errorf("SSE reconnect exhausted: %w", err)
				break
			}
			continue
		}
		var p struct {
			SessionID  string              `json:"sessionID"`
			Info       opencodeMessageInfo `json:"info"`
			Part       opencodePart        `json:"part"`
			PartID     string              `json:"partID"`
			Field      string              `json:"field"`
			Delta      string              `json:"delta"`
			Error      json.RawMessage     `json:"error"`
			ID         string              `json:"id"`
			Permission string              `json:"permission"`
			Status     struct {
				Type string `json:"type"`
			} `json:"status"`
		}
		if json.Unmarshal(event.Properties, &p) != nil {
			continue
		}
		sid := p.SessionID
		if sid == "" {
			sid = p.Info.SessionID
		}
		if sid == "" {
			sid = p.Part.SessionID
		}
		if event.Type == "session.created" || event.Type == "session.updated" {
			if s.owns(p.Info.ParentID) && p.Info.ID != s.id {
				if _, exists := s.children[p.Info.ID]; !exists {
					s.children[p.Info.ID] = opencodeSummary{Tokens: &opencodeTokens{}}
				}
			}
		}
		if sid == "" && (event.Type == "session.created" || event.Type == "session.updated") {
			sid = p.Info.ID
		}
		if !s.owns(sid) && !(event.Type == "session.error" && sid == "") {
			continue
		}
		switch event.Type {
		case "session.error":
			if hasOpencodeError(p.Error) {
				failure = cloneRaw(p.Error)
				s.dispatch(Event{Kind: EventError, Err: opencodeErrorText(failure), Raw: failure})
			}
		case "message.updated":
			roles[p.Info.ID] = p.Info.Role
			if sid == s.id {
				s.updateInfo(p.Info)
			}
			if hasOpencodeError(p.Info.Error) {
				failure = cloneRaw(p.Info.Error)
			}
		case "message.part.updated":
			if sid == s.id && p.Part.Type == "text" && roles[p.Part.MessageID] == "assistant" {
				textParts[p.Part.ID] = true
				if _, ok := parts[p.Part.ID]; !ok {
					order = append(order, p.Part.ID)
				}
				parts[p.Part.ID] = p.Part.Text
			}
			if p.Part.Type == "tool" {
				noteTruncation(p.Part)
				if !tools[p.Part.ID] {
					tools[p.Part.ID] = true
					s.dispatch(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: p.Part.Tool}, Raw: event.Properties})
				}
				if p.Part.State.Status == "completed" || p.Part.State.Status == "error" {
					s.dispatch(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: p.Part.Tool, Text: p.Part.State.Output}, Raw: event.Properties})
				}
			}
		case "message.part.delta":
			if sid == s.id && p.Field == "text" && textParts[p.PartID] {
				if _, ok := parts[p.PartID]; !ok {
					order = append(order, p.PartID)
				}
				parts[p.PartID] += p.Delta
				s.dispatch(Event{Kind: EventMessage, Text: p.Delta, Raw: event.Properties})
			}
		case "permission.asked":
			if !permissions[p.ID] {
				permissions[p.ID] = true
				s.dispatch(Event{Kind: EventSystem, State: event.Type, Raw: event.Properties})
				s.permission(p.ID, p.Permission, event.Properties)
			}
		default:
			s.dispatch(Event{Kind: EventSystem, State: event.Type, Raw: event.Properties})
		}
		if sid == s.id && (event.Type == "session.idle" || event.Type == "session.status" && p.Status.Type == "idle") {
			break
		}
	}
finishedStream:
	cancel()
	finalCtx, stopFinal := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer stopFinal()
	var messages []opencodeMessage
	finalText := ""
	if err := s.server.call(finalCtx, "GET", s.path()+"/message", nil, &messages); err == nil {
		for _, m := range messages {
			if seen[m.Info.ID] || m.Info.Role != "assistant" {
				continue
			}
			s.updateInfo(m.Info)
			if hasOpencodeError(m.Info.Error) {
				failure = cloneRaw(m.Info.Error)
			}
			var text strings.Builder
			for _, part := range m.Parts {
				noteTruncation(part)
				if part.Type == "text" {
					text.WriteString(part.Text)
				}
			}
			if text.Len() > 0 {
				finalText = text.String()
			}
		}
	} else if streamErr == nil {
		streamErr = err
	}
	if finalText == "" {
		for _, id := range order {
			finalText += parts[id]
		}
	}
	if usage, err := s.totalUsage(finalCtx); err == nil {
		s.dispatch(Event{Kind: EventUsage, Usage: &usage, Raw: json.RawMessage(`{"accounting":"job_delta"}`)})
	} else {
		s.dispatch(Event{Kind: EventSystem, State: "usage_partial", Text: err.Error()})
	}

	if finalText != "" {
		s.dispatch(Event{Kind: EventMessage, Final: true, Text: finalText})
	}
	terminal := Event{Kind: EventState, State: StateTurnCompleted}
	if hasOpencodeError(failure) {
		terminal.State = StateTurnFailed
		terminal.Err = opencodeErrorText(failure)
		terminal.Raw = failure
	} else if streamErr != nil {
		terminal.State = StateTurnFailed
		terminal.Err = "Opencode event stream ended: " + streamErr.Error()
		if ctx.Err() == nil && s.lifeCtx.Err() == nil {
			terminal.Raw = json.RawMessage(`{"name":"APIError","data":{"isRetryable":true}}`)
		}
	}
	s.dispatch(terminal)
	return nil
}
func (s *opencodeSession) failPrompt(err error) error {
	e := Event{Kind: EventState, State: StateTurnFailed, Err: s.server.redact(err.Error())}
	var httpError *opencodeHTTPError
	if errors.As(err, &httpError) {
		e.Raw, _ = json.Marshal(map[string]any{"name": "APIError", "data": map[string]any{"statusCode": httpError.status, "message": e.Err}})
	}
	s.dispatch(e)
	return err
}

// replyContext bounds a permission reply by the session, not by the event
// stream that saw the request: approvers answer asynchronously, after that
// stream may have ended or reconnected. The server call adds its own timeout.
func (s *opencodeSession) replyContext() context.Context {
	if s.lifeCtx == nil {
		return context.Background()
	}
	return s.lifeCtx
}

func (s *opencodeSession) permission(id, name string, raw json.RawMessage) {
	var once sync.Once
	respond := func(d Decision) (err error) {
		once.Do(func() {
			reply := "reject"
			if s.opts.Access != AccessReadOnly && (d == DecisionAllow || d == DecisionAllowForSession) {
				reply = "once"
			}
			err = s.server.call(s.replyContext(), "POST", "/permission/"+url.PathEscape(id)+"/reply", map[string]string{"reply": reply}, nil)
			raw, _ := json.Marshal(map[string]string{"id": id, "reply": reply})
			s.dispatch(Event{Kind: EventSystem, State: "permission.replied", Raw: raw})
			if err != nil {
				s.dispatch(Event{Kind: EventError, Err: err.Error()})
			}
		})
		return err
	}
	if s.opts.Access == AccessReadOnly {
		_ = respond(DecisionDeny)
		return
	}
	s.dispatchMu.RLock()
	defer s.dispatchMu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.approvals <- ApprovalRequest{Kind: "permission", ToolName: name, RawInput: json.RawMessage(s.server.redact(string(raw))), Respond: respond}:
	case <-s.done:
	}
}
func readOpencodeEvent(scan *bufio.Scanner) (opencodeEvent, error) {
	var data strings.Builder
	for scan.Scan() {
		line := scan.Text()
		if line == "" && data.Len() > 0 {
			var event opencodeEvent
			if err := json.Unmarshal([]byte(data.String()), &event); err != nil {
				return event, fmt.Errorf("invalid opencode event: %w", err)
			}
			return event, nil
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scan.Err(); err != nil {
		return opencodeEvent{}, err
	}
	return opencodeEvent{}, io.ErrUnexpectedEOF
}

func validateOpencodeModel(model string, models []string) error {
	for _, offered := range models {
		if offered == model {
			return nil
		}
	}
	return fmt.Errorf("%w: %q; available: %s", ErrModelUnavailable, model, modelChoices("opencode", model, models))
}
