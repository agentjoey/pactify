package projection

import (
	"strings"
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

func TestProjectDepsFoldedAndRendered(t *testing.T) {
	evs := []event.Event{
		initEv(),
		{EventType: "assign", TaskID: "T1", Feature: "F", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}},
		{EventType: "assign", TaskID: "T2", Feature: "F", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s", "deps": []any{"T1"}}},
	}
	st := Project(evs)
	if got := st.Features[0].Tasks[1].Deps; len(got) != 1 || got[0] != "T1" {
		t.Fatalf("T2 deps not folded: %+v", got)
	}
	if st.Features[0].Tasks[0].Deps != nil {
		t.Fatalf("T1 (deps-free) must have nil Deps, got %+v", st.Features[0].Tasks[0].Deps)
	}
	out := Render(st)
	if !strings.Contains(out, "        deps: [T1]\n") {
		t.Fatalf("render missing deps line:\n%s", out)
	}
	if n := strings.Count(out, "deps:"); n != 1 {
		t.Fatalf("expected exactly one deps line, got %d:\n%s", n, out)
	}
}

func TestProjectEmptyDepsArrayOmitted(t *testing.T) {
	// An assign carrying an explicit empty deps array must render deps-free
	// (byte-parity with the bash reference, which has no deps concept).
	evs := []event.Event{
		initEv(),
		{EventType: "assign", TaskID: "T1", Feature: "F", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "b", "spec": "s", "deps": []any{}}},
	}
	st := Project(evs)
	if st.Features[0].Tasks[0].Deps != nil {
		t.Fatalf("empty deps array must fold to nil, got %+v", st.Features[0].Tasks[0].Deps)
	}
	if strings.Contains(Render(st), "deps:") {
		t.Fatal("empty deps must not render a deps line")
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
