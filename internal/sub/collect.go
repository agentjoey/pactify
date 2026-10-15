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
