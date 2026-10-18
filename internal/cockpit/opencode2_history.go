package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type jsonRaw = json.RawMessage

type opencode2Cursor struct {
	Next string `json:"next"`
}

func (s *opencode2Session) discover2(ctx context.Context, baseline bool) error {
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true
		cursor := ""
		cursors := map[string]bool{}
		for {
			q := url.Values{"parentID": {id}, "limit": {"200"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			var page struct {
				Data []struct {
					ID       string `json:"id"`
					ParentID string `json:"parentID"`
				} `json:"data"`
				Cursor opencode2Cursor `json:"cursor"`
			}
			if err := s.server.call(ctx, "GET", "/api/session?"+q.Encode(), nil, &page); err != nil {
				return err
			}
			for _, child := range page.Data {
				if child.ParentID != id || child.ID == "" {
					continue
				}
				if _, ok := s.children[child.ID]; !ok {
					base := opencodeSummary{Tokens: &opencodeTokens{}}
					if baseline {
						if err := s.server.call2(ctx, "GET", "/api/session/"+url.PathEscape(child.ID), nil, &base); err != nil {
							return err
						}
						if base.Tokens == nil {
							return fmt.Errorf("Opencode child session summary is missing tokens")
						}
					}
					s.children[child.ID] = base
				}
				if err := visit(child.ID); err != nil {
					return err
				}
			}
			cursor = page.Cursor.Next
			if cursor == "" {
				break
			}
			if cursors[cursor] {
				return fmt.Errorf("opencode repeated session cursor")
			}
			cursors[cursor] = true
		}
		return nil
	}
	return visit(s.id)
}
func (s *opencode2Session) usage2(ctx context.Context) (Usage, error) {
	if err := s.discover2(ctx, false); err != nil {
		return Usage{}, err
	}
	bases := map[string]opencodeSummary{s.id: s.base}
	for id, b := range s.children {
		bases[id] = b
	}
	var total Usage
	for id, b := range bases {
		var summary opencodeSummary
		if err := s.server.call2(ctx, "GET", "/api/session/"+url.PathEscape(id), nil, &summary); err != nil {
			return total, err
		}
		if summary.Tokens == nil {
			return total, fmt.Errorf("Opencode session %s summary is missing tokens", id)
		}
		u := summary.usageSince(b)
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.TotalTokens += u.TotalTokens
		total.CostUSD += u.CostUSD
	}
	return total, nil
}

type opencode2Message struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Outcome string  `json:"outcome"`
	Error   jsonRaw `json:"error"`
	Time    struct {
		Completed *float64 `json:"completed"`
	} `json:"time"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (s *opencode2Session) final2(ctx context.Context, prompt string) (string, jsonRaw, string, error) {
	cursor := ""
	cursors := map[string]bool{}
	var messages []opencode2Message
	for {
		q := url.Values{"limit": {"200"}}
		if cursor == "" {
			q.Set("order", "desc")
		} else {
			q.Set("cursor", cursor)
		}
		var page struct {
			Data   []opencode2Message `json:"data"`
			Cursor opencode2Cursor    `json:"cursor"`
		}
		if err := s.server.call(ctx, "GET", s.path()+"/message?"+q.Encode(), nil, &page); err != nil {
			return "", nil, "", err
		}
		for _, m := range page.Data {
			if m.ID == prompt && m.Type == "user" {
				// Descending pages may include later background wakes. Walk forward
				// from this prompt and stop at its first durable terminal marker.
				text := ""
				var failure jsonRaw
				for i := len(messages) - 1; i >= 0; i-- {
					m := messages[i]
					if m.Type == "idle" {
						return text, m.Error, m.Outcome, nil
					}
					if m.Type == "assistant" && m.Time.Completed != nil {
						var b strings.Builder
						for _, c := range m.Content {
							if c.Type == "text" {
								b.WriteString(c.Text)
							}
						}
						text, failure = b.String(), m.Error
					}
				}
				return text, failure, "", nil
			}
			messages = append(messages, m)
		}
		cursor = page.Cursor.Next
		if cursor == "" {
			return "", nil, "", fmt.Errorf("opencode prompt boundary not found in message history")
		}
		if cursors[cursor] {
			return "", nil, "", fmt.Errorf("opencode repeated message cursor")
		}
		cursors[cursor] = true
	}
}
