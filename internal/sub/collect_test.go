package sub

import (
	"github.com/agentjoey/pactify/internal/cockpit"
	"testing"
)

func TestCollectorPrefersCodexFinal(t *testing.T) {
	var c Collector
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "WRO"})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "TE"})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "WROTE"})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Final: true})
	c.Add(cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted})
	if c.FinalText() != "WROTE" || !c.TurnDone {
		t.Fatalf("final=%q done=%v", c.FinalText(), c.TurnDone)
	}
}

func TestCollectorTakesLastCodexFinal(t *testing.T) {
	var c Collector
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "我会尝试在当前工作区创建 probe.txt。"})
	c.Add(cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "shell"}})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "BLOCKED"})
	if c.FinalText() != "BLOCKED" {
		t.Fatalf("final=%q", c.FinalText())
	}
}

func TestCollectorACPChunksAfterLastTool(t *testing.T) {
	var c Collector
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "thinking out loud"})
	c.Add(cockpit.Event{Kind: cockpit.EventTool})
	c.Add(cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "Write"}})
	c.Add(cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "end", Name: "Write"}})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "WRO"})
	c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "TE"})
	if c.FinalText() != "WROTE" || c.ToolCalls != 1 {
		t.Fatalf("final=%q tools=%d", c.FinalText(), c.ToolCalls)
	}
}

func TestCollectorUsageKeepsLatestTotals(t *testing.T) {
	var c Collector
	c.Add(cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 10, OutputTokens: 1}})
	c.Add(cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 40, OutputTokens: 7, CostUSD: 0.01}})
	c.Add(cockpit.Event{Kind: cockpit.EventUsage})
	if c.Usage.InputTokens != 40 || c.Usage.OutputTokens != 7 || c.Usage.CostUSD != 0.01 {
		t.Fatalf("usage=%+v", c.Usage)
	}
}

func TestCollectorFailureAndRateLimits(t *testing.T) {
	var c Collector
	c.Add(cockpit.Event{Kind: cockpit.EventState, State: "turn_started"})
	if c.TurnDone {
		t.Fatal("start ended turn")
	}
	c.Add(cockpit.Event{Kind: cockpit.EventSystem, State: "rate_limits", Raw: []byte(`{"remaining":5}`)})
	c.Add(cockpit.Event{Kind: cockpit.EventSystem, State: "other", Raw: []byte(`{}`)})
	c.Add(cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "boom"})
	if !c.TurnDone || c.TurnErr != "boom" || string(c.RateLimit) != `{"remaining":5}` {
		t.Fatalf("collector=%+v", c)
	}
}
