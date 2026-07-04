package stats

import (
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/event"
)

// A task in changes_requested is being reworked — the clock must keep running
// past the last checkpoint (end = now), unlike awaiting_review which freezes at
// the last checkpoint.
func TestCompute_ChangesRequestedClockKeepsRunning(t *testing.T) {
	base := []event.Event{
		ev("init", "", "", "2026-06-14T11:59:00Z", map[string]any{
			"project": "p", "protocol_version": 1,
			"seats":       []any{map[string]any{"id": "w", "roles": []any{"worker"}, "entry": "AGENTS.md"}},
			"base_branch": "main",
		}),
		// assign 12:00 → checkpoint 12:10 → changes_requested 12:12
		ev("assign", "t1", "f", "2026-06-14T12:00:00Z", map[string]any{"owner": "w", "reviewer": "r", "branch": "b"}),
		ev("checkpoint", "t1", "f", "2026-06-14T12:10:00Z", map[string]any{"evidence": "ok"}),
		ev("changes_requested", "t1", "f", "2026-06-14T12:12:00Z", map[string]any{"notes": "fix it"}),
	}

	find := func(s Stats) TaskStat {
		for _, ts := range s.Tasks {
			if ts.TaskID == "t1" {
				return ts
			}
		}
		t.Fatal("t1 not in stats")
		return TaskStat{}
	}

	// 30 min into rework: duration = assign→now, well past the 10-min checkpoint.
	now := time.Date(2026, 6, 14, 12, 40, 0, 0, time.UTC)
	got := find(Compute(base, now))
	if got.Status != "changes_requested" {
		t.Fatalf("status = %q, want changes_requested", got.Status)
	}
	if got.DurationSec != 2400 {
		t.Fatalf("duration = %ds, want 2400 (assign→now during rework)", got.DurationSec)
	}

	// The clock keeps growing while the task sits in changes_requested.
	later := now.Add(5 * time.Minute)
	if d := find(Compute(base, later)).DurationSec; d != 2700 {
		t.Fatalf("duration at now+5m = %ds, want 2700 (still ticking)", d)
	}

	// Contrast: awaiting_review stays frozen at the last checkpoint.
	awaiting := base[:3]
	if d := find(Compute(awaiting, later)).DurationSec; d != 600 {
		t.Fatalf("awaiting_review duration = %ds, want 600 (assign→last checkpoint)", d)
	}
}

// initEvt is a minimal init event registering seat w (owner) — shared setup for
// the reliability-rollup tests.
func initEvt() event.Event {
	return ev("init", "", "", "2026-06-14T11:59:00Z", map[string]any{
		"project": "p", "protocol_version": 1,
		"seats":       []any{map[string]any{"id": "w", "roles": []any{"worker"}, "entry": "AGENTS.md"}},
		"base_branch": "main",
	})
}

func agentStat(s Stats, seat string) (AgentStat, bool) {
	for _, a := range s.Agents {
		if a.Seat == seat {
			return a, true
		}
	}
	return AgentStat{}, false
}

// Accepted/Reworked are per-seat reliability signals derived purely from the
// event stream: an `accept` on an owned task bumps Accepted; every
// `changes_requested` on an owned task bumps Reworked.
func TestCompute_AcceptedReworked(t *testing.T) {
	now := time.Date(2026, 6, 14, 13, 0, 0, 0, time.UTC)

	cases := []struct {
		name         string
		events       []event.Event
		wantAccepted int
		wantReworked int
	}{
		{
			name: "directly accepted",
			events: []event.Event{
				initEvt(),
				ev("assign", "t1", "f", "2026-06-14T12:00:00Z", map[string]any{"owner": "w", "reviewer": "r", "branch": "b"}),
				ev("checkpoint", "t1", "f", "2026-06-14T12:10:00Z", map[string]any{"evidence": "ok"}),
				ev("accept", "t1", "f", "2026-06-14T12:15:00Z", map[string]any{}),
			},
			wantAccepted: 1,
			wantReworked: 0,
		},
		{
			name: "two changes then accept",
			events: []event.Event{
				initEvt(),
				ev("assign", "t1", "f", "2026-06-14T12:00:00Z", map[string]any{"owner": "w", "reviewer": "r", "branch": "b"}),
				ev("checkpoint", "t1", "f", "2026-06-14T12:05:00Z", map[string]any{"evidence": "ok"}),
				ev("changes_requested", "t1", "f", "2026-06-14T12:06:00Z", map[string]any{"notes": "fix a"}),
				ev("checkpoint", "t1", "f", "2026-06-14T12:10:00Z", map[string]any{"evidence": "ok"}),
				ev("changes_requested", "t1", "f", "2026-06-14T12:11:00Z", map[string]any{"notes": "fix b"}),
				ev("checkpoint", "t1", "f", "2026-06-14T12:15:00Z", map[string]any{"evidence": "ok"}),
				ev("accept", "t1", "f", "2026-06-14T12:20:00Z", map[string]any{}),
			},
			wantAccepted: 1,
			wantReworked: 2,
		},
		{
			name: "never accepted with N changes",
			events: []event.Event{
				initEvt(),
				ev("assign", "t1", "f", "2026-06-14T12:00:00Z", map[string]any{"owner": "w", "reviewer": "r", "branch": "b"}),
				ev("checkpoint", "t1", "f", "2026-06-14T12:05:00Z", map[string]any{"evidence": "ok"}),
				ev("changes_requested", "t1", "f", "2026-06-14T12:06:00Z", map[string]any{"notes": "fix a"}),
				ev("checkpoint", "t1", "f", "2026-06-14T12:10:00Z", map[string]any{"evidence": "ok"}),
				ev("changes_requested", "t1", "f", "2026-06-14T12:11:00Z", map[string]any{"notes": "fix b"}),
				ev("checkpoint", "t1", "f", "2026-06-14T12:15:00Z", map[string]any{"evidence": "ok"}),
				ev("changes_requested", "t1", "f", "2026-06-14T12:16:00Z", map[string]any{"notes": "fix c"}),
			},
			wantAccepted: 0,
			wantReworked: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := agentStat(Compute(tc.events, now), "w")
			if !ok {
				t.Fatalf("seat w missing from agent rollup")
			}
			if a.Accepted != tc.wantAccepted {
				t.Errorf("Accepted = %d, want %d", a.Accepted, tc.wantAccepted)
			}
			if a.Reworked != tc.wantReworked {
				t.Errorf("Reworked = %d, want %d", a.Reworked, tc.wantReworked)
			}
		})
	}
}

// Accepted/Reworked survive the WithTaskLOC and WithTokens chained rebuilds of
// the agent rollup (they copy the whole struct, only zeroing their own fields).
func TestCompute_AcceptedReworkedSurvivesChaining(t *testing.T) {
	now := time.Date(2026, 6, 14, 13, 0, 0, 0, time.UTC)
	events := []event.Event{
		initEvt(),
		ev("assign", "t1", "f", "2026-06-14T12:00:00Z", map[string]any{"owner": "w", "reviewer": "r", "branch": "b"}),
		ev("checkpoint", "t1", "f", "2026-06-14T12:05:00Z", map[string]any{"evidence": "ok"}),
		ev("changes_requested", "t1", "f", "2026-06-14T12:06:00Z", map[string]any{"notes": "fix"}),
		ev("checkpoint", "t1", "f", "2026-06-14T12:10:00Z", map[string]any{"evidence": "ok"}),
		ev("accept", "t1", "f", "2026-06-14T12:15:00Z", map[string]any{}),
	}
	s := Compute(events, now).
		WithTaskLOC(func(string) (int, int) { return 5, 2 }).
		WithTokens(func(string) int { return 99 })
	a, ok := agentStat(s, "w")
	if !ok {
		t.Fatal("seat w missing")
	}
	if a.Accepted != 1 || a.Reworked != 1 {
		t.Fatalf("Accepted/Reworked = %d/%d, want 1/1 after chaining", a.Accepted, a.Reworked)
	}
	if a.Added != 5 || a.Tokens != 99 {
		t.Fatalf("chained LOC/tokens lost: Added=%d Tokens=%d", a.Added, a.Tokens)
	}
}
