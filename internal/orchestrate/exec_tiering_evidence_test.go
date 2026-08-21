package orchestrate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/projection"
)

// The cap record must name the unfinished task and carry its recorded failure
// verbatim, so the operator diagnoses without re-reading project state.
func TestCapEvidenceIncludesLastFail(t *testing.T) {
	st := projection.State{Features: []projection.Feature{
		{ID: "f1", Status: "open", Tasks: []projection.Task{
			{ID: "legacy-backfill", Status: "in_progress"},
			{ID: "done-task", Status: "accepted"},
		}},
	}}
	h := History{
		LastFail:  map[string]string{"legacy-backfill": "worker run failed: exit status 1"},
		LastClass: map[string]FailClass{"legacy-backfill": FailLogic},
		Rework:    map[string]int{"legacy-backfill": 2},
	}
	got := capEvidence(st, h)
	if !strings.Contains(got, "legacy-backfill") {
		t.Fatalf("missing task id: %q", got)
	}
	if !strings.Contains(got, "worker run failed: exit status 1") {
		t.Fatalf("missing recorded failure: %q", got)
	}
	if !strings.Contains(got, "class=logic") || !strings.Contains(got, "rework=2") {
		t.Fatalf("missing class/rework: %q", got)
	}
	if strings.Contains(got, "done-task") {
		t.Fatalf("accepted task must be skipped: %q", got)
	}
}

// Regression (tradelinks shape): consecutive-failure counters were reset by
// progress (Fails all 0), so tripped() never fired and the run hit the global
// cap. The evidence must still surface the recorded failure, not "(global cap)".
func TestCapEvidenceFailsResetButLastFailKept(t *testing.T) {
	st := projection.State{Features: []projection.Feature{
		{ID: "f1", Status: "open", Tasks: []projection.Task{
			{ID: "legacy-backfill", Status: "in_progress"},
		}},
	}}
	h := History{
		Fails:    map[string]int{"legacy-backfill": 0},
		LastFail: map[string]string{"legacy-backfill": "worker run failed: exit status 1"},
	}
	got := capEvidence(st, h)
	if !strings.Contains(got, "worker run failed: exit status 1") {
		t.Fatalf("progress-reset history must still diagnose: %q", got)
	}
}

// Nothing unfinished (or only shipped features) → the record stays as before.
func TestCapEvidenceNothingUnfinished(t *testing.T) {
	h := History{LastFail: map[string]string{"t1": "boom"}}
	allAccepted := projection.State{Features: []projection.Feature{
		{ID: "f1", Status: "open", Tasks: []projection.Task{{ID: "t1", Status: "accepted"}}},
	}}
	if got := capEvidence(allAccepted, h); got != "(global cap)" {
		t.Fatalf("all accepted: got %q, want %q", got, "(global cap)")
	}
	shipped := projection.State{Features: []projection.Feature{
		{ID: "f1", Status: "shipped", Tasks: []projection.Task{{ID: "t1", Status: "in_progress"}}},
	}}
	if got := capEvidence(shipped, h); got != "(global cap)" {
		t.Fatalf("shipped feature must be skipped: got %q, want %q", got, "(global cap)")
	}
}

// Bounded output: more than 20 unfinished tasks truncate with a (+N more)
// marker; an overlong LastFail is cut so the record cannot bloat the ledger.
func TestCapEvidenceBounded(t *testing.T) {
	var tasks []projection.Task
	for i := 0; i < 25; i++ {
		tasks = append(tasks, projection.Task{ID: fmt.Sprintf("t%02d", i), Status: "assigned"})
	}
	st := projection.State{Features: []projection.Feature{{ID: "f1", Status: "open", Tasks: tasks}}}
	h := History{LastFail: map[string]string{"t00": strings.Repeat("e", 300)}}
	got := capEvidence(st, h)
	if !strings.Contains(got, "(+5 more)") {
		t.Fatalf("missing overflow marker: %q", got)
	}
	if strings.Contains(got, strings.Repeat("e", 201)) {
		t.Fatalf("LastFail not truncated to 200 runes")
	}
	if !strings.Contains(got, strings.Repeat("e", 200)) {
		t.Fatalf("truncated LastFail must keep the first 200 runes")
	}
	if n := strings.Count(got, "\n"); n > 20 {
		t.Fatalf("too many lines: %d", n+1)
	}
}
