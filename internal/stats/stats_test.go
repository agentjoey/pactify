package stats

import (
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/event"
)

func ev(typ, task, feature, ts string, payload map[string]any) event.Event {
	return event.Event{EventID: typ + task + ts, TS: ts, AgentID: "x", EventType: typ, TaskID: task, Feature: feature, Payload: payload}
}

func TestCompute_Duration(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 20, 0, 0, time.UTC)
	events := []event.Event{
		ev("init", "", "", "2026-06-14T11:59:00Z", map[string]any{
			"project": "p", "protocol_version": 1,
			"seats":       []any{map[string]any{"id": "w", "roles": []any{"worker"}, "entry": "AGENTS.md"}},
			"base_branch": "main",
		}),
		// t1: assigned 12:00 → checkpoint 12:10 → accept 12:15 (accepted) → 15min
		ev("assign", "t1", "f", "2026-06-14T12:00:00Z", map[string]any{"owner": "w", "reviewer": "r", "branch": "b"}),
		ev("checkpoint", "t1", "f", "2026-06-14T12:10:00Z", map[string]any{"evidence": "ok"}),
		ev("accept", "t1", "f", "2026-06-14T12:15:00Z", map[string]any{}),
		// t2: assigned 12:00 → join (in_progress), no checkpoint → up to now (12:20) → 20min
		ev("assign", "t2", "f", "2026-06-14T12:00:00Z", map[string]any{"owner": "w", "reviewer": "r", "branch": "b"}),
		ev("join", "", "", "2026-06-14T12:05:00Z", map[string]any{"roles": []any{"worker"}}),
	}
	// the join must be AgentID "w" to flip w's assigned tasks → in_progress.
	events[5].AgentID = "w"

	s := Compute(events, now)

	byID := map[string]TaskStat{}
	for _, ts := range s.Tasks {
		byID[ts.TaskID] = ts
	}
	if got := byID["t1"].DurationSec; got != 900 {
		t.Fatalf("t1 duration = %ds, want 900 (assign→accept)", got)
	}
	if byID["t1"].Status != "accepted" {
		t.Fatalf("t1 status = %q, want accepted", byID["t1"].Status)
	}
	if got := byID["t2"].DurationSec; got != 1200 {
		t.Fatalf("t2 duration = %ds, want 1200 (assign→now)", got)
	}
	if byID["t2"].Status != "in_progress" {
		t.Fatalf("t2 status = %q, want in_progress", byID["t2"].Status)
	}

	// agent rollup: w owns both → 2 tasks, 2100s total.
	if len(s.Agents) != 1 || s.Agents[0].Seat != "w" {
		t.Fatalf("agents = %+v, want one seat w", s.Agents)
	}
	if s.Agents[0].Tasks != 2 || s.Agents[0].DurationSec != 2100 {
		t.Fatalf("agent w = %+v, want 2 tasks / 2100s", s.Agents[0])
	}

	// WithTaskLOC: each task is attributed ONLY its own commits' diff, so t1 and
	// t2 differ; the agent rollup sums across the seat's owned tasks.
	loc := func(taskID string) (int, int) {
		switch taskID {
		case "t1":
			return 10, 2
		case "t2":
			return 5, 1
		}
		return 0, 0
	}
	withLOC := s.WithTaskLOC(loc)
	byID2 := map[string]TaskStat{}
	for _, ts := range withLOC.Tasks {
		byID2[ts.TaskID] = ts
	}
	if byID2["t1"].Added != 10 || byID2["t2"].Added != 5 {
		t.Fatalf("per-task LOC = t1:+%d t2:+%d, want 10 and 5", byID2["t1"].Added, byID2["t2"].Added)
	}
	if withLOC.Agents[0].Added != 15 || withLOC.Agents[0].Deleted != 3 {
		t.Fatalf("agent LOC = +%d/-%d, want +15/-3 (sum of owned tasks)", withLOC.Agents[0].Added, withLOC.Agents[0].Deleted)
	}
}
