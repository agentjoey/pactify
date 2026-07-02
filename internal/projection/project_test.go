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

// cancel excludes a single task from the projection; withdraw excludes an entire
// feature — the structured way to retire work without hand-editing the log.
func TestProjectCancelAndWithdraw(t *testing.T) {
	asg := func(task, feat, branch string) event.Event {
		return event.Event{EventType: "assign", TaskID: task, Feature: feat, Payload: map[string]any{
			"owner": "opencode", "reviewer": "claude-opus", "branch": branch, "spec": "s"}}
	}
	evs := []event.Event{
		initEv(),
		asg("T1", "F", "feat/x"),
		asg("T2", "F", "feat/x"),
		asg("G1", "G", "feat/g"),
		{EventType: "cancel", TaskID: "T2", Feature: "F"},
		{EventType: "withdraw", Feature: "G"},
	}
	st := Project(evs)
	if len(st.Features) != 1 || st.Features[0].ID != "F" {
		t.Fatalf("withdraw should drop feature G: %+v", featureIDs(st))
	}
	if len(st.Features[0].Tasks) != 1 || st.Features[0].Tasks[0].ID != "T1" {
		t.Fatalf("cancel should drop only T2: %+v", st.Features[0].Tasks)
	}
}

func featureIDs(st State) []string {
	ids := []string{}
	for _, f := range st.Features {
		ids = append(ids, f.ID)
	}
	return ids
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

// start is the driver-recorded, task-scoped "working" fact: it lifts exactly
// the named task out of `assigned` (unlike join, which is seat-scoped) and
// never rewinds a task that has already progressed past assigned.
func TestProjectStartLiftsOnlyTheNamedAssignedTask(t *testing.T) {
	asg := func(task string) event.Event {
		return event.Event{EventType: "assign", TaskID: task, Feature: "F", Payload: map[string]any{
			"owner": "opencode", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}}
	}
	evs := []event.Event{
		initEv(),
		asg("T1"),
		asg("T2"), // same owner, also assigned — must NOT be lifted
		{EventType: "start", TaskID: "T1", Feature: "F", AgentID: "claude-opus", Payload: map[string]any{"owner": "opencode"}},
	}
	st := Project(evs)
	if got := st.Features[0].Tasks[0].Status; got != "in_progress" {
		t.Fatalf("started task = %q, want in_progress", got)
	}
	if got := st.Features[0].Tasks[1].Status; got != "assigned" {
		t.Fatalf("sibling task = %q, want assigned (start is task-scoped)", got)
	}
}

func TestProjectStartNeverRewindsALaterStatus(t *testing.T) {
	evs := []event.Event{
		initEv(),
		{EventType: "assign", TaskID: "T1", Feature: "F", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "b", "spec": "s"}},
		{EventType: "checkpoint", TaskID: "T1", Feature: "F", Payload: map[string]any{"evidence": "ok"}},
		{EventType: "start", TaskID: "T1", Feature: "F", Payload: map[string]any{"owner": "opencode"}},
	}
	st := Project(evs)
	if got := st.Features[0].Tasks[0].Status; got != "awaiting_review" {
		t.Fatalf("start rewound a checkpointed task to %q, want awaiting_review", got)
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
