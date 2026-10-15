package cockpit

import (
	"encoding/json"
	"testing"
)

// codex can run sub-agents in child threads that share the app-server connection.
// Only the session's own thread may end the turn, supply the final reply or own the
// in-flight turn id (2026-10-15: a child reviewer's turn/completed ended a pactify
// sub job while the main agent was still fixing what the reviewer found).
func childThreadClient(t *testing.T) (*codexClient, *[]Event) {
	t.Helper()
	var got []Event
	c := &codexClient{itemNames: map[string]string{}}
	c.dispatchEvent = func(e Event) { got = append(got, e) }
	c.setMainThread("th-main")
	return c, &got
}

func notify(t *testing.T, c *codexClient, method string, params map[string]any) {
	t.Helper()
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	c.handleNotification(method, b)
}

func TestCodexChildThreadTurnEndIsNotTerminal(t *testing.T) {
	c, got := childThreadClient(t)
	notify(t, c, "turn/completed", map[string]any{"threadId": "th-child", "turn": map[string]any{"id": "tu-c", "status": "completed"}})
	for _, e := range *got {
		if e.Kind == EventState && (e.State == StateTurnCompleted || e.State == StateTurnFailed) {
			t.Fatalf("child turn end surfaced as session turn end: %+v", e)
		}
	}
	notify(t, c, "turn/completed", map[string]any{"threadId": "th-main", "turn": map[string]any{"id": "tu-m", "status": "completed"}})
	last := (*got)[len(*got)-1]
	if last.Kind != EventState || last.State != StateTurnCompleted {
		t.Fatalf("main turn end missing: %+v", last)
	}
}

func TestCodexChildThreadMessageIsNotTheFinalReply(t *testing.T) {
	c, got := childThreadClient(t)
	notify(t, c, "item/completed", map[string]any{"threadId": "th-child", "turnId": "tu-c", "item": map[string]any{"id": "i1", "type": "agentMessage", "text": "reviewer verdict"}})
	notify(t, c, "item/agentMessage/delta", map[string]any{"threadId": "th-child", "turnId": "tu-c", "itemId": "i1", "delta": "rev"})
	for _, e := range *got {
		if e.Kind == EventMessage {
			t.Fatalf("child message surfaced as session message: %+v", e)
		}
	}
	if len(*got) == 0 || (*got)[0].Kind != EventSystem || (*got)[0].State != "subagent_message" {
		t.Fatalf("child message must still reach the log as a system event: %+v", *got)
	}
	notify(t, c, "item/completed", map[string]any{"threadId": "th-main", "turnId": "tu-m", "item": map[string]any{"id": "i2", "type": "agentMessage", "text": "done"}})
	last := (*got)[len(*got)-1]
	if last.Kind != EventMessage || !last.Final || last.Text != "done" {
		t.Fatalf("main final reply missing: %+v", last)
	}
}

func TestCodexChildTurnStartDoesNotStealTheTurnID(t *testing.T) {
	c, _ := childThreadClient(t)
	notify(t, c, "turn/started", map[string]any{"threadId": "th-main", "turn": map[string]any{"id": "tu-m"}})
	notify(t, c, "turn/started", map[string]any{"threadId": "th-child", "turn": map[string]any{"id": "tu-c"}})
	notify(t, c, "turn/completed", map[string]any{"threadId": "th-child", "turn": map[string]any{"id": "tu-c", "status": "completed"}})
	if got := c.currentTurnID(); got != "tu-m" {
		t.Fatalf("turn id = %q, want the main thread's tu-m (Interrupt must target the main turn)", got)
	}
}

// Child threads still do the job's work: their tool calls and token usage count.
func TestCodexChildThreadToolsAndUsageStillCount(t *testing.T) {
	c, got := childThreadClient(t)
	notify(t, c, "item/started", map[string]any{"threadId": "th-child", "turnId": "tu-c", "item": map[string]any{"id": "x", "type": "commandExecution", "command": "go test ./..."}})
	notify(t, c, "thread/tokenUsage/updated", map[string]any{"threadId": "th-child", "turnId": "tu-c", "tokenUsage": map[string]any{
		"last":  map[string]any{"inputTokens": 5, "outputTokens": 1, "totalTokens": 6},
		"total": map[string]any{"inputTokens": 50, "outputTokens": 10, "totalTokens": 60}}})
	var tool, usage bool
	for _, e := range *got {
		tool = tool || (e.Kind == EventTool && e.Tool != nil && e.Tool.Phase == "start")
		usage = usage || e.Kind == EventUsage
	}
	if !tool || !usage {
		t.Fatalf("tool=%v usage=%v events=%+v", tool, usage, *got)
	}
}
