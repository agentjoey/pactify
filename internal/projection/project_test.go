package projection

import (
	"testing"

	"github.com/agentjoey/pactify/internal/event"
)

func initEv() event.Event {
	return event.Event{EventType: "init", AgentID: "claude-opus", Payload: map[string]any{
		"project": "p", "protocol_version": 1.0,
		"seats": []any{
			map[string]any{"id": "claude-opus", "roles": []any{"orchestrator", "reviewer"}, "entry": "CLAUDE.md"},
			map[string]any{"id": "opencode", "roles": []any{"worker"}, "entry": "AGENTS.md"},
		},
	}}
}

func TestProjectSeatsAndProject(t *testing.T) {
	st := Project([]event.Event{initEv()})
	if st.Project != "p" || len(st.Agents) != 2 || st.Agents[1].ID != "opencode" {
		t.Fatalf("bad state: %+v", st)
	}
}

func TestProjectStateMachine(t *testing.T) {
	evs := []event.Event{
		initEv(),
		{EventType: "assign", TaskID: "T1", Feature: "F", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}},
		{EventType: "join", AgentID: "opencode", Payload: map[string]any{"roles": []any{"worker"}}},
		{EventType: "checkpoint", TaskID: "T1", Feature: "F", Payload: map[string]any{"evidence": "ok"}},
	}
	st := Project(evs)
	tk := st.Features[0].Tasks[0]
	if tk.Status != "awaiting_review" || tk.Evidence == nil || *tk.Evidence != "ok" || tk.Owner != "opencode" {
		t.Fatalf("bad task: %+v", tk)
	}
	if st.Features[0].Branch != "feat/x" {
		t.Fatalf("bad branch: %q", st.Features[0].Branch)
	}
}

func TestProjectUnknownEventTypeIgnored(t *testing.T) {
	evs := []event.Event{
		initEv(),
		{EventType: "assign", TaskID: "T1", Feature: "F", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "b", "spec": "s"}},
		{EventType: "nudge", TaskID: "T1", Feature: "F", Payload: map[string]any{}},
	}
	st := Project(evs)
	if st.Features[0].Tasks[0].Status != "assigned" {
		t.Fatalf("unknown event must not change state: %+v", st.Features[0].Tasks[0])
	}
}

func TestProjectFirstSeenOrdering(t *testing.T) {
	evs := []event.Event{
		initEv(),
		{EventType: "assign", TaskID: "T2", Feature: "B", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "b2", "spec": "s"}},
		{EventType: "assign", TaskID: "T1", Feature: "A", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "b1", "spec": "s"}},
	}
	st := Project(evs)
	if st.Features[0].ID != "B" || st.Features[1].ID != "A" {
		t.Fatalf("features not in first-assign order: %+v", st.Features)
	}
}
