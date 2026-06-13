package orchestrate

import (
	"sort"
	"testing"

	"github.com/agentjoey/pactify/internal/projection"
)

// nextActions returns one runnable action per feature that has work, so N
// independent features yield N actions the driver can run concurrently (each in
// its own worktree). Within a feature the priority matches nextAction
// (reviewer > owner > merge); across features the order is by feature id.
func TestNextActions_OneActionPerFeature(t *testing.T) {
	st := projection.State{Features: []projection.Feature{
		{ID: "fa", Status: "in_progress", Tasks: []projection.Task{
			{ID: "a1", Owner: "w1", Reviewer: "r1", Status: "assigned"},
		}},
		{ID: "fb", Status: "in_progress", Tasks: []projection.Task{
			{ID: "b1", Owner: "w2", Reviewer: "r2", Status: "awaiting_review"},
		}},
	}}
	acts := nextActions(st, emptyHistory(), defaultTh())
	if len(acts) != 2 {
		t.Fatalf("want 2 actions, got %d: %+v", len(acts), acts)
	}
	sort.Slice(acts, func(i, j int) bool { return acts[i].Feature < acts[j].Feature })
	if acts[0].Feature != "fa" || acts[0].Kind != ActRunOwner {
		t.Errorf("fa action = %+v, want RunOwner", acts[0])
	}
	if acts[1].Feature != "fb" || acts[1].Kind != ActRunReviewer {
		t.Errorf("fb action = %+v, want RunReviewer", acts[1])
	}
}

// A feature contributes at most ONE action even when it has several runnable
// tasks — its tasks share a branch/worktree and must advance serially.
func TestNextActions_AtMostOnePerFeature(t *testing.T) {
	st := projection.State{Features: []projection.Feature{
		{ID: "fa", Status: "in_progress", Tasks: []projection.Task{
			{ID: "a1", Owner: "w1", Reviewer: "r1", Status: "assigned"},
			{ID: "a2", Owner: "w1", Reviewer: "r1", Status: "assigned"},
		}},
	}}
	acts := nextActions(st, emptyHistory(), defaultTh())
	if len(acts) != 1 {
		t.Fatalf("want 1 action for one feature, got %d", len(acts))
	}
	// Within-feature priority: reviewer beats owner.
	st.Features[0].Tasks[1].Status = "awaiting_review"
	acts = nextActions(st, emptyHistory(), defaultTh())
	if len(acts) != 1 || acts[0].Kind != ActRunReviewer || acts[0].Task != "a2" {
		t.Fatalf("want single RunReviewer(a2), got %+v", acts)
	}
}

// Shipped features and features with no runnable work contribute nothing.
func TestNextActions_SkipsShippedAndIdle(t *testing.T) {
	st := projection.State{Features: []projection.Feature{
		{ID: "done", Status: "shipped", Tasks: []projection.Task{
			{ID: "d1", Status: "accepted"},
		}},
		{ID: "blocked", Status: "in_progress", Tasks: []projection.Task{
			{ID: "x1", Owner: "w", Reviewer: "r", Status: "accepted"},
			{ID: "x2", Owner: "w", Reviewer: "r", Status: "assigned", Deps: []string{"x9-missing"}},
		}},
	}}
	acts := nextActions(st, emptyHistory(), defaultTh())
	// 'done' is shipped → skip. 'blocked': x1 accepted, x2's dep missing → not
	// runnable, and not all-accepted → no merge. So no actions.
	if len(acts) != 0 {
		t.Fatalf("want 0 actions, got %d: %+v", len(acts), acts)
	}
}

// An all-accepted feature yields a Merge action.
func TestNextActions_AllAcceptedMerges(t *testing.T) {
	st := projection.State{Features: []projection.Feature{
		{ID: "fa", Status: "in_progress", Tasks: []projection.Task{
			{ID: "a1", Owner: "w", Reviewer: "r", Status: "accepted"},
		}},
	}}
	acts := nextActions(st, emptyHistory(), defaultTh())
	if len(acts) != 1 || acts[0].Kind != ActMerge || acts[0].Feature != "fa" {
		t.Fatalf("want Merge(fa), got %+v", acts)
	}
}

// featureAction agrees with the serial nextAction for a single-feature state, so
// the parallel decomposition can't silently diverge from the proven core.
func TestFeatureAction_MatchesNextActionSingleFeature(t *testing.T) {
	mk := func(status string) projection.State {
		return projection.State{Features: []projection.Feature{
			{ID: "f", Status: "in_progress", Tasks: []projection.Task{
				{ID: "t", Owner: "w", Reviewer: "r", Status: status},
			}},
		}}
	}
	for _, status := range []string{"assigned", "in_progress", "changes_requested", "awaiting_review", "accepted"} {
		st := mk(status)
		serial := nextAction(st, emptyHistory(), defaultTh())
		par := nextActions(st, emptyHistory(), defaultTh())
		if serial.Kind == ActDone {
			continue
		}
		if len(par) != 1 {
			t.Fatalf("status %s: want 1 parallel action, got %d", status, len(par))
		}
		if par[0].Kind != serial.Kind || par[0].Task != serial.Task {
			t.Errorf("status %s: parallel %+v != serial %+v", status, par[0], serial)
		}
	}
}
