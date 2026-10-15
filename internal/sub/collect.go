package sub

import (
	"encoding/json"
	"strings"

	"github.com/agentjoey/pactify/internal/cockpit"
)

type Collector struct {
	finalText string
	chunks    strings.Builder
	Usage     cockpit.Usage
	ToolCalls int
	TurnDone  bool
	TurnErr   string
	RateLimit json.RawMessage

	threadTotals map[string]cockpit.Usage
}

func (c *Collector) Add(e cockpit.Event) {
	switch e.Kind {
	case cockpit.EventMessage:
		if e.Final {
			if e.Text != "" {
				c.finalText = e.Text
			}
		} else {
			c.chunks.WriteString(e.Text)
		}
	case cockpit.EventTool:
		if e.Tool != nil && e.Tool.Phase == "start" {
			c.ToolCalls++
			c.chunks.Reset()
		}
	case cockpit.EventUsage:
		if e.Usage != nil {
			c.Usage = *e.Usage
		}
		// codex reports the last request as Usage; the job's cost is the sum of each
		// thread's running total (the main agent and any sub-agent threads).
		var raw struct {
			ThreadID   string `json:"threadId"`
			TokenUsage struct {
				Total *cockpit.Usage `json:"total"`
			} `json:"tokenUsage"`
		}
		if json.Unmarshal(e.Raw, &raw) == nil && raw.TokenUsage.Total != nil {
			if c.threadTotals == nil {
				c.threadTotals = map[string]cockpit.Usage{}
			}
			c.threadTotals[raw.ThreadID] = *raw.TokenUsage.Total
			var sum cockpit.Usage
			for _, u := range c.threadTotals {
				sum.InputTokens += u.InputTokens
				sum.OutputTokens += u.OutputTokens
				sum.TotalTokens += u.TotalTokens
			}
			c.Usage = sum
		}
	case cockpit.EventState:
		if e.State == cockpit.StateTurnCompleted || e.State == cockpit.StateTurnFailed {
			c.TurnDone = true
		}
		if e.State == cockpit.StateTurnFailed {
			c.TurnErr = e.Err
		}
	case cockpit.EventSystem:
		if e.State == "rate_limits" {
			c.RateLimit = append(json.RawMessage(nil), e.Raw...)
		}
	}
}

func (c *Collector) FinalText() string {
	if c.finalText != "" {
		return c.finalText
	}
	return c.chunks.String()
}
