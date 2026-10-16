package sub

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"

	"github.com/agentjoey/pactify/internal/cockpit"
)

type Collector struct {
	finalText      string
	chunks         strings.Builder
	Usage          cockpit.Usage
	ToolCalls      int
	TurnDone       bool
	TurnErr        string
	RateLimit      json.RawMessage
	FailureRaw     json.RawMessage
	ErrorRaw       json.RawMessage
	FailureMessage string

	usageBase    cockpit.Usage
	perTurnUsage bool
	threadTotals map[string]cockpit.Usage
}

// Recover only newline-committed events after a worker disappears. A truncated
// final record cannot be trusted; stderr and malformed lines carry no usage.
func collectLoggedUsage(path string) Collector {
	var c Collector
	f, err := os.Open(path)
	if err != nil {
		return c
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 65536), 16*1024*1024)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if atEOF && bytes.IndexByte(data, '\n') < 0 {
			return 0, nil, nil
		}
		return bufio.ScanLines(data, atEOF)
	})
	for scanner.Scan() {
		if event, ok := parseLogEvent(scanner.Text()); ok && (event.Kind == cockpit.EventUsage || event.Kind == cockpit.EventTool || event.Kind == cockpit.EventSystem) {
			c.Add(event.Event)
		}
	}
	return c
}

func (c *Collector) Add(e cockpit.Event) {
	if e.Kind == cockpit.EventSystem && e.State == "transient_retry" {
		c.finalText = ""
		c.chunks.Reset()
		c.TurnDone, c.TurnErr, c.FailureMessage = false, "", ""
		c.FailureRaw, c.ErrorRaw = nil, nil
		c.perTurnUsage = e.Text == "kimi"
		if c.perTurnUsage {
			c.usageBase = c.Usage
		}
		return
	}
	if e.Kind == cockpit.EventError && len(e.Raw) > 0 {
		c.ErrorRaw = append(json.RawMessage(nil), e.Raw...)
	}
	if e.Kind == cockpit.EventError || (e.Kind == cockpit.EventState && e.State == cockpit.StateTurnFailed) {
		if len(e.Raw) > 0 {
			c.FailureRaw = append(json.RawMessage(nil), e.Raw...)
		}
		if e.Err != "" {
			c.FailureMessage = e.Err
		} else {
			var p struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
				Turn struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				} `json:"turn"`
			}
			if json.Unmarshal(e.Raw, &p) == nil {
				if p.Turn.Error.Message != "" {
					c.FailureMessage = p.Turn.Error.Message
				} else if p.Error.Message != "" {
					c.FailureMessage = p.Error.Message
				}
			}
		}
	}
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
			if c.perTurnUsage {
				c.Usage.InputTokens += c.usageBase.InputTokens
				c.Usage.OutputTokens += c.usageBase.OutputTokens
				c.Usage.TotalTokens += c.usageBase.TotalTokens
				c.Usage.CostUSD += c.usageBase.CostUSD
			}
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
