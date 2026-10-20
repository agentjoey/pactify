package sub

import (
	"fmt"
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

// codex's thread/tokenUsage/updated carries both the last request ("last") and the
// thread's running total ("total"); cockpit maps "last" into Usage. A job summary must
// report the running total so multi-request jobs are not undercounted.
func TestCollectorUsesCodexThreadTotal(t *testing.T) {
	var c Collector
	raw := []byte(`{"tokenUsage":{"total":{"inputTokens":1067257,"cachedInputTokens":1014784,"outputTokens":3932},"last":{"inputTokens":62506,"outputTokens":258}}}`)
	c.Add(cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 62506, OutputTokens: 258}, Raw: raw})
	if c.Usage.InputTokens != 1067257 || c.Usage.OutputTokens != 3932 {
		t.Fatalf("usage=%+v, want thread total 1067257/3932", c.Usage)
	}
	// Usage events without a total (other harnesses) keep the mapped values.
	var d Collector
	d.Add(cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 10, OutputTokens: 2}, Raw: []byte(`{"x":1}`)})
	if d.Usage.InputTokens != 10 || d.Usage.OutputTokens != 2 {
		t.Fatalf("usage=%+v", d.Usage)
	}
}

// A job's cost spans every codex thread it ran (the main agent and its sub-agents):
// each thread reports its own running total, so the job total is their sum.
func TestCollectorSumsUsageAcrossCodexThreads(t *testing.T) {
	var c Collector
	ev := func(thread string, in, out int) cockpit.Event {
		raw := []byte(fmt.Sprintf(`{"threadId":%q,"tokenUsage":{"total":{"inputTokens":%d,"outputTokens":%d,"totalTokens":%d}}}`, thread, in, out, in+out))
		return cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{}, Raw: raw}
	}
	c.Add(ev("main", 100, 10))
	c.Add(ev("child", 40, 4))
	c.Add(ev("main", 300, 30)) // running total: replaces main's earlier 100/10
	if c.Usage.InputTokens != 340 || c.Usage.OutputTokens != 34 {
		t.Fatalf("usage=%+v, want 340/34", c.Usage)
	}
}
