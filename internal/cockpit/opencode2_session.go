package cockpit

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agentjoey/pactify/internal/procx"
	"net/url"
	"strings"
	"sync"
	"time"
)

func NewOpencode2Backend() Backend { return &opencode2Backend{} }

type opencode2Backend struct{}

func (*opencode2Backend) Start(ctx context.Context, o StartOpts) (Session, error) {
	return newOpencode2Session(ctx, o, "")
}
func (b *opencode2Backend) Resume(ctx context.Context, id string) (Session, error) {
	return b.ResumeWith(ctx, id, StartOpts{})
}
func (*opencode2Backend) ResumeWith(ctx context.Context, id string, o StartOpts) (Session, error) {
	return newOpencode2Session(ctx, o, id)
}

// Lifecycle, redaction, channels and accounting arithmetic share the v1 primitives.
type opencode2Session struct {
	*opencodeSession
	interruptMu       sync.Mutex
	interruptPending  bool
	inactivityTimeout time.Duration
}

func newOpencode2Session(ctx context.Context, opts StartOpts, id string) (*opencode2Session, error) {
	server, err := startOpencode2Server(ctx, opts)
	if err != nil {
		return nil, err
	}
	life, stop := context.WithCancel(context.Background())
	s := &opencode2Session{opencodeSession: &opencodeSession{server: server, id: id, opts: opts, lifeCtx: life, stop: stop, done: life.Done(), events: make(chan Event, 64), approvals: make(chan ApprovalRequest, 16), children: map[string]opencodeSummary{}}}
	fail := func(err error) (*opencode2Session, error) {
		_ = s.Close()
		if s.id != "" {
			return nil, &StartError{ID: s.id, Err: err}
		}
		return nil, err
	}
	s.info.Access = "build"
	if opts.Access == AccessReadOnly {
		s.info.Access = "plan"
	}
	if opts.Model == "" && opts.Effort != "" {
		var selected *opencode2Model
		if err := server.call2(ctx, "GET", "/api/model/default", nil, &selected); err != nil {
			return fail(err)
		}
		if selected == nil || !selected.Enabled {
			return fail(fmt.Errorf("%w: no enabled default opencode model for effort selection", ErrModelUnavailable))
		}
		opts.Model = selected.ProviderID + "/" + selected.ID
		s.opts.Model = opts.Model
	}
	model := map[string]string{}
	if opts.Model != "" || opts.Effort != "" {
		models, err := server.models2(ctx)
		if err != nil {
			return fail(err)
		}
		available := []string{}
		found := false
		for _, m := range models {
			if !m.Enabled {
				continue
			}
			key := m.ProviderID + "/" + m.ID
			available = append(available, key)
			if key != opts.Model {
				continue
			}
			found = true
			model = map[string]string{"id": m.ID, "providerID": m.ProviderID}
			if opts.Effort != "" {
				valid := false
				for _, v := range m.Variants {
					if v.ID == opts.Effort {
						valid = true
					}
				}
				if !valid {
					return fail(fmt.Errorf("%w: unsupported opencode effort %q for %s", ErrModelUnsupported, opts.Effort, opts.Model))
				}
				model["variant"] = opts.Effort
			}
		}
		if !found {
			err := validateOpencodeModel(opts.Model, available)
			if provider, _, ok := strings.Cut(opts.Model, "/"); ok && opencode2NeedsLogin(ctx, server, provider, available) {
				return fail(fmt.Errorf("opencode 2.x has no pactify login for provider %s; run pactify sub login opencode (%v)", provider, err))
			}
			return fail(err)
		}
	}
	permissions := opencode2Permissions(opts.Access)
	if id == "" {
		body := map[string]any{"title": "Pactify delegated task", "agent": s.info.Access, "location": map[string]string{"directory": opts.RepoDir}, "permissions": permissions}
		if len(model) > 0 {
			body["model"] = model
		}
		var summary opencodeSummary
		if err := server.call2(ctx, "POST", "/api/session", body, &summary); err != nil {
			return fail(err)
		}
		s.id = summary.ID
		if !strings.HasPrefix(s.id, "ses") {
			return fail(fmt.Errorf("opencode returned an invalid v2 session id"))
		}
	}
	if err := server.call2(ctx, "GET", s.path(), nil, &s.base); err != nil {
		var httpError *opencodeHTTPError
		if id != "" && errors.As(err, &httpError) && httpError.status == 404 {
			err = fmt.Errorf("opencode 2.x session not found; sessions cannot be resumed across opencode major versions: %w", err)
		}
		return fail(err)
	}
	if id != "" {
		if err := server.call2(ctx, "PATCH", s.path(), map[string]any{"permissions": permissions}, nil); err != nil {
			return fail(err)
		}
		if err := server.call2(ctx, "POST", s.path()+"/agent", map[string]string{"agent": s.info.Access}, nil); err != nil {
			return fail(err)
		}
		if len(model) > 0 {
			if err := server.call2(ctx, "POST", s.path()+"/model", map[string]any{"model": model}, nil); err != nil {
				return fail(err)
			}
		}
	}
	if opts.SystemPrompt != "" { // InstructionEntry.Key: ^[a-z0-9][a-z0-9._-]*$.
		if err := server.call2(ctx, "PUT", "/api/experimental/session/"+url.PathEscape(s.id)+"/instructions/entries/pactify-system", map[string]string{"value": opts.SystemPrompt}, nil); err != nil {
			return fail(err)
		}
	}
	if s.base.Tokens == nil {
		return fail(fmt.Errorf("Opencode session summary is missing tokens"))
	}
	if err := s.discover2(ctx, true); err != nil {
		return fail(err)
	}
	if id != "" {
		for child := range s.children {
			if err := server.call2(ctx, "PATCH", "/api/session/"+url.PathEscape(child), map[string]any{"permissions": permissions}, nil); err != nil {
				return fail(err)
			}
		}
	}
	return s, nil
}
func (s *opencode2Session) path() string { return "/api/session/" + url.PathEscape(s.id) }
func (s *opencode2Session) Interrupt(ctx context.Context) error {
	s.server.snapshot()
	s.interruptMu.Lock()
	var reply struct {
		Interrupted bool `json:"interrupted"`
	}
	// SessionInterruptResponse is the one consumed v2 response without a data envelope.
	err := s.server.call(ctx, "POST", s.path()+"/interrupt", map[string]any{}, &reply)
	s.interruptPending = err == nil && !reply.Interrupted
	s.interruptMu.Unlock()
	s.server.mu.Lock()
	children := make([]procx.Identity, 0, len(s.server.descendants))
	for child := range s.server.descendants {
		children = append(children, child)
	}
	s.server.mu.Unlock()
	return errors.Join(err, procx.TerminateDescendants("opencode tools", children, 250*time.Millisecond))
}
func (s *opencode2Session) permission2(ctx context.Context, sid, id, name string, raw json.RawMessage) {
	var once sync.Once
	var replyErr error
	respond := func(d Decision) error {
		once.Do(func() {
			body := map[string]string{"decision": "reject", "message": "Pactify denied this action. Continue without it."}
			if s.opts.Access != AccessReadOnly && (d == DecisionAllow || d == DecisionAllowForSession) {
				body = map[string]string{"decision": "once"}
			}
			replyErr = s.server.call2(ctx, "POST", "/api/session/"+url.PathEscape(sid)+"/permission/"+url.PathEscape(id)+"/reply", body, nil)
			b, _ := json.Marshal(body)
			s.dispatch(Event{Kind: EventSystem, State: "permission.replied", Raw: b})
			if replyErr != nil {
				s.dispatch(Event{Kind: EventError, Err: replyErr.Error()})
			}
		})
		return replyErr
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
func opencode2PromptID() string {
	b := make([]byte, 14)
	_, _ = rand.Read(b)
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	// Same 6-byte timestamp/counter shape as Identifier.ascending.
	return fmt.Sprintf("msg_%012x%s", (uint64(time.Now().UnixMilli())*0x1000+1)&0xffffffffffff, string(b))
}

// opencode2NeedsLogin is true only for a provider opencode can connect that
// offers no available model and has no connection in the private home. A typo or an
// unknown provider stays a model error instead of prompting a login.
func opencode2NeedsLogin(ctx context.Context, server *opencodeServer, provider string, available []string) bool {
	for _, model := range available {
		if strings.HasPrefix(model, provider+"/") {
			return false
		}
	}
	// /api/provider lists only connected providers; the integration catalog
	// lists every connectable one with connection ids/status, never secrets.
	var integrations []struct {
		ID          string            `json:"id"`
		Connections []json.RawMessage `json:"connections"`
	}
	if server.call2(ctx, "GET", "/api/integration", nil, &integrations) != nil {
		return false
	}
	for _, i := range integrations {
		if i.ID == provider {
			return len(i.Connections) == 0
		}
	}
	return false
}
