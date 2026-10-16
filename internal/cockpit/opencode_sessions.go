package cockpit

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
)

func (s *opencodeSession) owns(id string) bool {
	if id == s.id {
		return true
	}
	_, ok := s.children[id]
	return ok
}
func (s *opencodeSession) discoverChildren(ctx context.Context, baseline bool) error {
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true
		var children []struct {
			ID       string `json:"id"`
			ParentID string `json:"parentID"`
		}
		if err := s.server.call(ctx, "GET", "/session/"+url.PathEscape(id)+"/children", nil, &children); err != nil {
			return err
		}
		for _, child := range children {
			if child.ParentID != id || child.ID == "" {
				continue
			}
			if _, exists := s.children[child.ID]; !exists {
				base := opencodeSummary{Tokens: &opencodeTokens{}}
				if baseline {
					if err := s.server.call(ctx, "GET", "/session/"+url.PathEscape(child.ID), nil, &base); err != nil {
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
		return nil
	}
	return visit(s.id)
}
func (s *opencodeSession) totalUsage(ctx context.Context) (Usage, error) {
	if err := s.discoverChildren(ctx, false); err != nil {
		return Usage{}, err
	}
	bases := map[string]opencodeSummary{s.id: s.base}
	for id, base := range s.children {
		bases[id] = base
	}
	var total Usage
	for id, base := range bases {
		var summary opencodeSummary
		if err := s.server.call(ctx, "GET", "/session/"+url.PathEscape(id), nil, &summary); err != nil {
			return total, err
		}
		if summary.Tokens == nil {
			return total, fmt.Errorf("Opencode session %s summary is missing tokens", id)
		}
		delta := summary.usageSince(base)
		total.InputTokens += delta.InputTokens
		total.OutputTokens += delta.OutputTokens
		total.TotalTokens += delta.TotalTokens
		total.CostUSD += delta.CostUSD
	}
	return total, nil
}
func (s *opencodeSession) openEvents(ctx context.Context) (*http.Response, *bufio.Scanner, error) {
	res, err := s.server.request(ctx, "GET", "/event", nil)
	if err != nil {
		return nil, nil, err
	}
	scan := bufio.NewScanner(res.Body)
	scan.Buffer(make([]byte, 65536), 16*1024*1024)
	if _, err := readOpencodeEvent(scan); err != nil {
		_ = res.Body.Close()
		return nil, nil, err
	}
	return res, scan, nil
}
