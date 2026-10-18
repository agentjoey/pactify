package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type opencode2Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func readOpencode2Event(scan *bufio.Scanner) (opencode2Event, error) {
	// The SSE framing is shared; the envelope's payload key changed in v2.
	e, err := readOpencodeEvent(scan)
	return opencode2Event{Type: e.Type, Data: e.Data}, err
}

// activityReader reports every byte from the event stream, including the
// server's 15-second SSE heartbeat comments, so a long silent tool call is not
// mistaken for a stalled stream.
type activityReader struct {
	r     io.Reader
	touch func()
}

func (a activityReader) Read(p []byte) (int, error) {
	n, err := a.r.Read(p)
	if n > 0 && a.touch != nil {
		a.touch()
	}
	return n, err
}

func (s *opencode2Session) open2(ctx context.Context, touch func()) (*http.Response, *bufio.Scanner, error) {
	res, err := s.server.request(ctx, "GET", "/api/event", nil)
	if err != nil {
		return nil, nil, err
	}
	scan := bufio.NewScanner(activityReader{r: res.Body, touch: touch})
	scan.Buffer(make([]byte, 65536), 16*1024*1024)
	e, err := readOpencode2Event(scan)
	if err == nil && e.Type != "server.connected" {
		err = fmt.Errorf("opencode event stream did not start with server.connected")
	}
	if err != nil {
		res.Body.Close()
		return nil, nil, err
	}
	return res, scan, nil
}
func (s *opencode2Session) Prompt(ctx context.Context, msg UserMessage) error {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	idleLimit := s.inactivityTimeout
	if idleLimit == 0 {
		idleLimit = 2 * time.Minute
	}
	var idleExpired atomic.Bool
	idleTimer := time.AfterFunc(idleLimit, func() { idleExpired.Store(true); cancel() })
	defer idleTimer.Stop()
	stop := context.AfterFunc(s.lifeCtx, cancel)
	defer stop()
	touch := func() { idleTimer.Reset(idleLimit) }
	res, scan, err := s.open2(streamCtx, touch)
	if err != nil {
		return s.failPrompt(err)
	}
	defer func() { res.Body.Close() }()
	prompt := opencode2PromptID()
	if err := s.server.call2(streamCtx, "POST", s.path()+"/prompt", map[string]string{"id": prompt, "text": msg.Text, "delivery": "steer"}, nil); err != nil {
		return s.failPrompt(err)
	}
	delivered := false
	warned := false
	reconnects := 0
	type toolState struct {
		Name    string
		Input   string
		Started bool
		Ended   bool
	}
	tools := map[string]*toolState{}
	texts := map[string]string{}
	var textOrder []string
	lastTextMessage := ""
	pending := map[string]bool{}
	parents := map[string]string{}
	shellSessions := map[string]string{}
	var failure json.RawMessage
	var streamErr error
	canceled := false
	terminalSeen := false
	var waited atomic.Bool
	permission := func(sid, id, action string, raw json.RawMessage) {
		if !pending[id] {
			pending[id] = true
			s.dispatch(Event{Kind: EventSystem, State: "permission.asked", Raw: raw})
			s.permission2(sid, id, action, raw)
		}
	}
	for {
		event, readErr := readOpencode2Event(scan)
		if readErr != nil {
			res.Body.Close()
			if waited.Load() {
				goto finished
			}
			if streamCtx.Err() != nil {
				streamErr = streamCtx.Err()
				break
			}
			connected := false
			for reconnects < 5 {
				delay := 100 * time.Millisecond * time.Duration(1<<reconnects)
				reconnects++
				select {
				case <-time.After(delay):
				case <-streamCtx.Done():
				}
				if streamCtx.Err() != nil {
					break
				}
				next, nextScan, err := s.open2(streamCtx, touch)
				if err != nil {
					readErr = err
					continue
				}
				res, scan = next, nextScan
				if err = s.discover2(streamCtx, false); err != nil {
					readErr = err
					res.Body.Close()
					continue
				}
				// The feed cannot replay: reconcile permissions and execution state on each reconnect.
				for sid := range s.owned2() {
					var requests []json.RawMessage
					if err = s.server.call2(streamCtx, "GET", "/api/session/"+url.PathEscape(sid)+"/permission", nil, &requests); err != nil {
						break
					}
					for _, raw := range requests {
						var request struct {
							ID     string `json:"id"`
							Action string `json:"action"`
						}
						if json.Unmarshal(raw, &request) == nil && request.ID != "" {
							permission(sid, request.ID, request.Action, raw)
						}
					}
				}
				if err != nil {
					readErr = err
					res.Body.Close()
					continue
				}
				if _, _, _, historyErr := s.final2(streamCtx, prompt); historyErr == nil {
					delivered = true
				}
				var active map[string]json.RawMessage
				if err = s.server.call2(streamCtx, "GET", "/api/session/active", nil, &active); err != nil {
					readErr = err
					res.Body.Close()
					continue
				}
				if _, ok := active[s.id]; !ok {
					goto finished
				}
				// wait is process-local and unbounded. On settlement close this connection
				// so reconciliation can recover the durable idle/error marker.
				go func(response *http.Response) {
					r, e := s.server.request(streamCtx, "POST", "/api/experimental/session/"+url.PathEscape(s.id)+"/wait", map[string]any{})
					if e == nil {
						r.Body.Close()
						waited.Store(true)
						response.Body.Close()
					}
				}(res)
				connected = true
				break
			}
			if !connected {
				streamErr = fmt.Errorf("SSE reconnect exhausted: %w", readErr)
				break
			}
			continue
		}
		idleTimer.Reset(idleLimit)
		var p struct {
			SessionID          string          `json:"sessionID"`
			ParentID           string          `json:"parentID"`
			AssistantMessageID string          `json:"assistantMessageID"`
			Ordinal            int             `json:"ordinal"`
			Delta              string          `json:"delta"`
			Text               string          `json:"text"`
			ID                 string          `json:"id"`
			Name               string          `json:"name"`
			InboxID            string          `json:"inboxID"`
			Action             string          `json:"action"`
			Reason             string          `json:"reason"`
			Input              json.RawMessage `json:"input"`
			Error              json.RawMessage `json:"error"`
			Metadata           json.RawMessage `json:"metadata"`
			Content            []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Model struct {
				ID         string `json:"id"`
				ProviderID string `json:"providerID"`
				Variant    string `json:"variant"`
			} `json:"model"`
			Info struct {
				ID       string `json:"id"`
				Metadata struct {
					SessionID string `json:"sessionID"`
				} `json:"metadata"`
			} `json:"info"`
			Form struct {
				ID        string `json:"id"`
				SessionID string `json:"sessionID"`
			} `json:"form"`
		}
		if json.Unmarshal(event.Data, &p) != nil {
			continue
		}
		if event.Type == "session.created" {
			parents[p.SessionID] = p.ParentID
			for changed := true; changed; {
				changed = false
				for child, parent := range parents {
					if s.owns(parent) && !s.owns(child) {
						s.children[child] = opencodeSummary{Tokens: &opencodeTokens{}}
						changed = true
					}
				}
			}
		}
		sid := p.SessionID
		if event.Type == "shell.created" {
			sid = p.Info.Metadata.SessionID
			if s.owns(sid) {
				shellSessions[p.Info.ID] = sid
			}
		}
		if event.Type == "shell.exited" {
			sid = shellSessions[p.ID]
		}
		if event.Type == "form.created" {
			sid = p.Form.SessionID
		}
		if !s.owns(sid) {
			continue
		}
		var metadata struct {
			Truncated bool `json:"truncated"`
		}
		if json.Unmarshal(p.Metadata, &metadata) == nil && metadata.Truncated && !warned {
			warned = true
			s.dispatch(Event{Kind: EventSystem, State: "warning", Text: "Opencode truncated tool output"})
		}
		key := p.AssistantMessageID + "/" + fmt.Sprint(p.Ordinal)
		switch event.Type {
		case "session.inbox.delivered":
			if sid == s.id && p.InboxID == prompt {
				delivered = true
				s.interruptMu.Lock()
				pendingInterrupt := s.interruptPending
				s.interruptMu.Unlock()
				if pendingInterrupt {
					if err := s.Interrupt(streamCtx); err != nil {
						streamErr = err
						goto finished
					}
				}
			}
		case "session.text.delta", "session.text.ended":
			if sid != s.id {
				break
			}
			if _, ok := texts[key]; !ok {
				textOrder = append(textOrder, key)
			}
			if event.Type == "session.text.delta" {
				texts[key] += p.Delta
				s.dispatch(Event{Kind: EventMessage, Text: p.Delta, Raw: event.Data})
			} else {
				texts[key] = p.Text
				lastTextMessage = p.AssistantMessageID
			}
		case "session.step.started":
			if sid == s.id {
				s.mu.Lock()
				s.info.Provider = p.Model.ProviderID
				s.info.Model = p.Model.ProviderID + "/" + p.Model.ID
				s.info.Effort = p.Model.Variant
				s.mu.Unlock()
			}
		case "session.tool.input.started", "session.tool.input.delta", "session.tool.input.ended", "session.tool.called", "session.tool.success", "session.tool.failed":
			key := p.AssistantMessageID + "/" + p.ID
			t := tools[key]
			if t == nil {
				t = &toolState{}
				tools[key] = t
			}
			if p.Name != "" {
				t.Name = p.Name
			}
			switch event.Type {
			case "session.tool.input.delta":
				t.Input += p.Delta
			case "session.tool.input.ended":
				t.Input = p.Text
			case "session.tool.called":
				t.Input = string(p.Input)
			}
			if event.Type == "session.tool.input.started" {
				s.dispatch(Event{Kind: EventSystem, State: event.Type, Raw: event.Data})
			}
			if event.Type == "session.tool.called" && !t.Started {
				t.Started = true
				s.dispatch(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: t.Name, Text: t.Input}, Raw: event.Data})
			}
			if (event.Type == "session.tool.success" || event.Type == "session.tool.failed") && !t.Ended {
				t.Ended = true
				var b strings.Builder
				for _, c := range p.Content {
					if c.Type == "text" {
						b.WriteString(c.Text)
					}
				}
				if b.Len() == 0 && hasOpencodeError(p.Error) {
					b.WriteString(opencode2ErrorText(p.Error))
				}
				s.dispatch(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: t.Name, Text: b.String()}, Raw: event.Data})
			}
		case "permission.asked":
			permission(sid, p.ID, p.Action, event.Data)
		case "form.created":
			if err := s.server.call2(streamCtx, "DELETE", "/api/session/"+url.PathEscape(sid)+"/form/"+url.PathEscape(p.Form.ID)+"?"+url.Values{"message": {"Pactify cannot answer forms. Continue without it."}}.Encode(), nil, nil); err != nil {
				s.dispatch(Event{Kind: EventError, Err: err.Error()})
			}
		case "session.execution.failed":
			if sid == s.id {
				terminalSeen = true
				failure = p.Error
				goto finished
			}
		case "session.execution.succeeded":
			if sid == s.id && delivered {
				terminalSeen = true
				goto finished
			}
		case "session.execution.interrupted":
			if sid == s.id && delivered {
				terminalSeen = true
				if p.Reason == "user" {
					canceled = true
				} else {
					streamErr = fmt.Errorf("Opencode execution interrupted: %s", p.Reason)
				}
				goto finished
			}
		case "session.reasoning.delta", "session.reasoning.started", "session.reasoning.ended", "session.tool.progress", "session.usage", "session.usage.updated", "session.step.usage":
			// Final accounting is authoritative; these high-frequency updates add no job state.
		default:
			s.dispatch(Event{Kind: EventSystem, State: event.Type, Raw: event.Data})
		}
	}
finished:
	if idleExpired.Load() {
		streamErr = fmt.Errorf("Opencode event stream inactive for %s", idleLimit)
	}
	cancel()
	finalCtx, stopFinal := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer stopFinal()
	text, historyError, outcome, historyErr := s.final2(finalCtx, prompt)
	if historyErr != nil {
		for _, key := range textOrder {
			if lastTextMessage == "" || strings.HasPrefix(key, lastTextMessage+"/") {
				text += texts[key]
			}
		}
		s.dispatch(Event{Kind: EventSystem, State: "history_partial", Text: historyErr.Error()})
		if !terminalSeen {
			streamErr = fmt.Errorf("cannot confirm opencode prompt completion: %w", historyErr)
		}
	} else {
		if !hasOpencodeError(failure) {
			failure = historyError
		}
		if !terminalSeen {
			switch outcome {
			case "succeeded":
				streamErr = nil
			case "interrupted":
				streamErr = fmt.Errorf("Opencode execution interrupted; reason unavailable in recovered history")
			case "failed":
			default:
				if streamErr == nil {
					streamErr = fmt.Errorf("cannot confirm opencode prompt completion: no terminal outcome in message history")
				}
			}
		}
		if outcome == "failed" && !hasOpencodeError(failure) {
			streamErr = fmt.Errorf("Opencode execution failed")
		}
	}
	if u, err := s.usage2(finalCtx); err == nil {
		s.dispatch(Event{Kind: EventUsage, Usage: &u, Raw: json.RawMessage(`{"accounting":"job_delta"}`)})
	} else {
		s.dispatch(Event{Kind: EventSystem, State: "usage_partial", Text: err.Error()})
	}
	var active map[string]json.RawMessage
	if s.server.call2(finalCtx, "GET", "/api/session/active", nil, &active) == nil {
		for id := range active {
			if id != s.id && s.owns(id) {
				s.dispatch(Event{Kind: EventSystem, State: "warning", Text: "Opencode background child session is still active"})
				break
			}
		}
	}
	if text != "" {
		s.dispatch(Event{Kind: EventMessage, Final: true, Text: text})
	}
	terminal := Event{Kind: EventState, State: StateTurnCompleted}
	if hasOpencodeError(failure) {
		terminal.State = StateTurnFailed
		terminal.Err = opencode2ErrorText(failure)
		terminal.Raw = failure
	} else if canceled {
		terminal.State = StateTurnFailed
		terminal.Err = "Opencode execution canceled by user"
		terminal.Raw = json.RawMessage(`{"type":"pactify.canceled"}`)
	} else if streamErr != nil {
		terminal.State = StateTurnFailed
		terminal.Err = streamErr.Error()
		if ctx.Err() == nil && s.lifeCtx.Err() == nil {
			terminal.Raw = json.RawMessage(`{"type":"provider.transport"}`)
		}
	}
	s.dispatch(terminal)
	return nil
}
func (s *opencode2Session) owned2() map[string]bool {
	owned := map[string]bool{s.id: true}
	for id := range s.children {
		owned[id] = true
	}
	return owned
}
