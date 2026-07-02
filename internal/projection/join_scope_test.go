package projection

import (
	"testing"

	"github.com/agentjoey/pactify/internal/event"
)

// Helpers local to the join-scope tests: an assign for owner "opencode" with
// optional deps, and the seat's join event.
func joinAsg(task string, deps ...string) event.Event {
	p := map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}
	if len(deps) > 0 {
		ds := make([]any, len(deps))
		for i, d := range deps {
			ds[i] = d
		}
		p["deps"] = ds
	}
	return event.Event{EventType: "assign", TaskID: task, Feature: "F", Payload: p}
}

func joinEv() event.Event {
	return event.Event{EventType: "join", AgentID: "opencode", Payload: map[string]any{"roles": []any{"worker"}}}
}

func taskStatus(t *testing.T, st State, taskID string) string {
	t.Helper()
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			if tk.ID == taskID {
				return tk.Status
			}
		}
	}
	t.Fatalf("task %s not in projection", taskID)
	return ""
}

// join is the seat's interactive cold-start signal: it lifts ONLY the first
// actionable owned task — not every assigned task the seat owns. A second
// owned task nobody started must stay `assigned`.
func TestProjectJoinLiftsOnlyFirstActionableOwnedTask(t *testing.T) {
	st := Project([]event.Event{initEv(), joinAsg("T1"), joinAsg("T2"), joinEv()})
	if got := taskStatus(t, st, "T1"); got != "in_progress" {
		t.Fatalf("T1 = %q, want in_progress", got)
	}
	if got := taskStatus(t, st, "T2"); got != "assigned" {
		t.Fatalf("T2 = %q, want assigned (join lifts one task, not all)", got)
	}
}

// A dep-blocked owned task is not actionable: the join skips it and lifts the
// first owned task whose deps are all accepted.
func TestProjectJoinSkipsDepBlockedTask(t *testing.T) {
	dep := event.Event{EventType: "assign", TaskID: "D1", Feature: "F", Payload: map[string]any{
		"owner": "other", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}}
	st := Project([]event.Event{initEv(), dep, joinAsg("T1", "D1"), joinAsg("T2"), joinEv()})
	if got := taskStatus(t, st, "T1"); got != "assigned" {
		t.Fatalf("dep-blocked T1 = %q, want assigned", got)
	}
	if got := taskStatus(t, st, "T2"); got != "in_progress" {
		t.Fatalf("runnable T2 = %q, want in_progress", got)
	}
}

// When every owned task is dep-blocked the join lifts nothing.
func TestProjectJoinLiftsNothingWhenAllOwnedTasksBlocked(t *testing.T) {
	dep := event.Event{EventType: "assign", TaskID: "D1", Feature: "F", Payload: map[string]any{
		"owner": "other", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}}
	st := Project([]event.Event{initEv(), dep, joinAsg("T1", "D1"), joinEv()})
	if got := taskStatus(t, st, "T1"); got != "assigned" {
		t.Fatalf("blocked T1 = %q, want assigned", got)
	}
}

// An accepted dep makes the dependent actionable for the join.
func TestProjectJoinLiftsTaskWithAcceptedDep(t *testing.T) {
	dep := event.Event{EventType: "assign", TaskID: "D1", Feature: "F", Payload: map[string]any{
		"owner": "other", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}}
	evs := []event.Event{
		initEv(), dep,
		{EventType: "checkpoint", TaskID: "D1", Feature: "F", Payload: map[string]any{"evidence": "ok"}},
		{EventType: "accept", TaskID: "D1", Feature: "F", Payload: map[string]any{}},
		joinAsg("T1", "D1"),
		joinEv(),
	}
	if got := taskStatus(t, Project(evs), "T1"); got != "in_progress" {
		t.Fatalf("T1 = %q, want in_progress (dep accepted)", got)
	}
}

// A cancelled dep can never reach accepted; it must not hold the dependent
// back, and the cancelled task itself is never the one lifted.
func TestProjectJoinTreatsCancelledDepAsSatisfied(t *testing.T) {
	dep := event.Event{EventType: "assign", TaskID: "D1", Feature: "F", Payload: map[string]any{
		"owner": "opencode", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}}
	evs := []event.Event{
		initEv(), dep,
		joinAsg("T1", "D1"),
		{EventType: "cancel", TaskID: "D1", Feature: "F", Payload: map[string]any{}},
		joinEv(),
	}
	st := Project(evs)
	if got := taskStatus(t, st, "T1"); got != "in_progress" {
		t.Fatalf("T1 = %q, want in_progress (cancelled dep is vacuously satisfied)", got)
	}
	// D1 is retired: it must be gone from the projection, not lifted.
	if len(st.Features[0].Tasks) != 1 {
		t.Fatalf("cancelled D1 must be excluded: %+v", st.Features[0].Tasks)
	}
}

// join never rewinds a task that has progressed past assigned, and does not
// touch tasks owned by other seats.
func TestProjectJoinNeverRewindsOrTouchesOthers(t *testing.T) {
	other := event.Event{EventType: "assign", TaskID: "O1", Feature: "F", Payload: map[string]any{
		"owner": "other", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}}
	evs := []event.Event{
		initEv(),
		joinAsg("T1"),
		{EventType: "checkpoint", TaskID: "T1", Feature: "F", Payload: map[string]any{"evidence": "ok"}},
		other,
		joinAsg("T2"),
		joinEv(),
	}
	st := Project(evs)
	if got := taskStatus(t, st, "T1"); got != "awaiting_review" {
		t.Fatalf("T1 = %q, want awaiting_review (join must not rewind)", got)
	}
	if got := taskStatus(t, st, "O1"); got != "assigned" {
		t.Fatalf("O1 = %q, want assigned (other seat's task untouched)", got)
	}
	if got := taskStatus(t, st, "T2"); got != "in_progress" {
		t.Fatalf("T2 = %q, want in_progress (first actionable after checkpointed T1)", got)
	}
}
