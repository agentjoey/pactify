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
