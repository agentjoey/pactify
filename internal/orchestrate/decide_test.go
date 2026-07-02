package orchestrate

import (
	"testing"

	"github.com/agentjoey/pactify/internal/projection"
)

// defaultTh is a permissive threshold so the Stuck branch is not triggered
// unless a test deliberately exercises it.
func defaultTh() Thresholds { return Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50} }

func emptyHistory() History {
	return History{Rework: map[string]int{}, Fails: map[string]int{}}
}

// a) task assigned + deps empty → RunOwner(task, seat=owner)
func TestNextAction_AssignedNoDeps_RunOwner(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "assigned"},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunOwner {
		t.Fatalf("kind = %v, want ActRunOwner", act.Kind)
	}
	if act.Feature != "f1" || act.Task != "t1" || act.Seat != "w1" {
		t.Fatalf("got feature=%q task=%q seat=%q", act.Feature, act.Task, act.Seat)
	}
}

// b) task assigned + dep NOT accepted → that task is not actionable.
// Here the only blocked task's dep is in_progress; the dep itself (assigned) is
// the actionable one, so we must get RunOwner for the dep, never the blocked task.
func TestNextAction_AssignedDepNotAccepted_BlockedTaskSkipped(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "assigned"},
				{ID: "t2", Owner: "w2", Reviewer: "r1", Status: "assigned", Deps: []string{"t1"}},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunOwner || act.Task != "t1" {
		t.Fatalf("got kind=%v task=%q, want RunOwner t1 (t2 blocked by unaccepted dep)", act.Kind, act.Task)
	}
}

// b') a single task blocked by an unaccepted dep with no other action → not Done,
// not Merge; falls through to Idle (no threshold tripped).
func TestNextAction_DepBlockedTask_DrivesDepFirst(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				// t0 is in_progress (runnable — `join` flipped it); t1 depends on the
				// not-yet-accepted t0 so t1 stays blocked. The driver advances the
				// dependency first → RunOwner(t0); t1 never jumps its dep.
				{ID: "t0", Owner: "w0", Reviewer: "r1", Status: "in_progress"},
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "assigned", Deps: []string{"t0"}},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunOwner || act.Task != "t0" {
		t.Fatalf("got %+v, want RunOwner(t0) (drive the dep first; t1 stays blocked)", act)
	}
}

// c) task assigned + dep already accepted → RunOwner
func TestNextAction_AssignedDepAccepted_RunOwner(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "accepted"},
				{ID: "t2", Owner: "w2", Reviewer: "r1", Status: "assigned", Deps: []string{"t1"}},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunOwner || act.Task != "t2" || act.Seat != "w2" {
		t.Fatalf("got kind=%v task=%q seat=%q, want RunOwner t2 w2", act.Kind, act.Task, act.Seat)
	}
}

// c') changes_requested + deps accepted → RunOwner
func TestNextAction_ChangesRequested_RunOwner(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "changes_requested"},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunOwner || act.Task != "t1" || act.Seat != "w1" {
		t.Fatalf("got kind=%v task=%q seat=%q, want RunOwner t1 w1", act.Kind, act.Task, act.Seat)
	}
}

// A changes_requested task blocked by an unaccepted dep does not run; the driver
// advances the runnable dependency (t0) instead.
func TestNextAction_ChangesRequestedDepNotAccepted_DrivesDepFirst(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t0", Owner: "w0", Reviewer: "r1", Status: "in_progress"},
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "changes_requested", Deps: []string{"t0"}},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunOwner || act.Task != "t0" {
		t.Fatalf("got %+v, want RunOwner(t0) (t1 changes_requested stays dep-blocked)", act)
	}
}

// d) task awaiting_review → RunReviewer(task, seat=reviewer)
func TestNextAction_AwaitingReview_RunReviewer(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "awaiting_review"},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunReviewer || act.Task != "t1" || act.Seat != "r1" {
		t.Fatalf("got kind=%v task=%q seat=%q, want RunReviewer t1 r1", act.Kind, act.Task, act.Seat)
	}
}

// e) feature all tasks accepted (not shipped) → Merge(feature)
func TestNextAction_AllAccepted_Merge(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "accepted"},
				{ID: "t2", Owner: "w2", Reviewer: "r1", Status: "accepted"},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActMerge || act.Feature != "f1" {
		t.Fatalf("got kind=%v feature=%q, want Merge f1", act.Kind, act.Feature)
	}
}

// A feature with zero tasks is not mergeable (vacuous all-accepted must not fire).
func TestNextAction_FeatureNoTasks_NotMerged(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{ID: "f1", Status: "in_progress"}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind == ActMerge {
		t.Fatalf("kind = %v, must not Merge a feature with no tasks", act.Kind)
	}
}

// f) all features shipped → Done
func TestNextAction_AllShipped_Done(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "shipped",
			Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "accepted"},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActDone {
		t.Fatalf("kind = %v, want ActDone", act.Kind)
	}
}

// f') no features at all → Done
func TestNextAction_NoFeatures_Done(t *testing.T) {
	act := nextAction(projection.State{}, emptyHistory(), defaultTh())
	if act.Kind != ActDone {
		t.Fatalf("kind = %v, want ActDone (no features)", act.Kind)
	}
}

// Thresholds are enforced by the LOOP (it owns History), not by nextAction:
// with in_progress runnable, a churning task always has an action, so the pure
// decision function never reaches a threshold branch. The bounds live in
// tripped(), tested here directly (rework + fail). The iteration bound is a
// separate loop-level guard, covered by the loop's escalation integration test.
func TestTripped_ReworkExceeded(t *testing.T) {
	h := emptyHistory()
	h.Rework["t1"] = 3
	if reason, ok := tripped("t1", h, Thresholds{MaxRework: 3, MaxFails: 3}); !ok || reason == "" {
		t.Fatalf("rework=3 MaxRework=3: got (%q,%v), want tripped", reason, ok)
	}
}

func TestTripped_FailsExceeded(t *testing.T) {
	h := emptyHistory()
	h.Fails["t1"] = 3
	if reason, ok := tripped("t1", h, Thresholds{MaxRework: 3, MaxFails: 3}); !ok || reason == "" {
		t.Fatalf("fails=3 MaxFails=3: got (%q,%v), want tripped", reason, ok)
	}
}

func TestTripped_WithinBounds(t *testing.T) {
	h := emptyHistory()
	h.Rework["t1"] = 1
	h.Fails["t1"] = 1
	if reason, ok := tripped("t1", h, Thresholds{MaxRework: 3, MaxFails: 3}); ok {
		t.Fatalf("rework=1 fails=1 under bounds: got (%q,%v), want not tripped", reason, ok)
	}
}

// Thresholds must not fire when an action IS available: a runnable assigned task
// trumps a tripped Rework counter (Stuck is the "no action" fallback only).
func TestNextAction_ThresholdDoesNotPreemptAction(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "assigned"},
			},
		}},
	}
	h := emptyHistory()
	h.Rework["t1"] = 99
	h.Iters = 999
	act := nextAction(st, h, Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50})
	if act.Kind != ActRunOwner {
		t.Fatalf("kind = %v, want ActRunOwner (action available, thresholds must not preempt)", act.Kind)
	}
}

// Priority: RunReviewer > RunOwner. An awaiting_review task in a later feature
// still beats a runnable assigned task in an earlier feature.
func TestNextAction_PriorityReviewerOverOwner(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{
			{ID: "f1", Status: "in_progress", Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "assigned"},
			}},
			{ID: "f2", Status: "in_progress", Tasks: []projection.Task{
				{ID: "t2", Owner: "w2", Reviewer: "r2", Status: "awaiting_review"},
			}},
		},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunReviewer || act.Task != "t2" {
		t.Fatalf("got kind=%v task=%q, want RunReviewer t2 (reviewer beats owner across features)", act.Kind, act.Task)
	}
}

// Priority: RunOwner > Merge. A feature fully accepted (mergeable) loses to a
// runnable assigned task in another feature.
func TestNextAction_PriorityOwnerOverMerge(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{
			{ID: "f1", Status: "in_progress", Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "accepted"},
			}},
			{ID: "f2", Status: "in_progress", Tasks: []projection.Task{
				{ID: "t2", Owner: "w2", Reviewer: "r2", Status: "assigned"},
			}},
		},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunOwner || act.Task != "t2" {
		t.Fatalf("got kind=%v task=%q, want RunOwner t2 (owner beats merge across features)", act.Kind, act.Task)
	}
}

// Priority: RunReviewer > Merge as well (transitive sanity).
func TestNextAction_PriorityReviewerOverMerge(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{
			{ID: "f1", Status: "in_progress", Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "accepted"},
			}},
			{ID: "f2", Status: "in_progress", Tasks: []projection.Task{
				{ID: "t2", Owner: "w2", Reviewer: "r2", Status: "awaiting_review"},
			}},
		},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunReviewer || act.Task != "t2" {
		t.Fatalf("got kind=%v task=%q, want RunReviewer t2 (reviewer beats merge)", act.Kind, act.Task)
	}
}

// Determinism: with multiple awaiting_review candidates, the first in
// feature-then-task order wins.
func TestNextAction_FirstReviewerCandidate(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "awaiting_review"},
				{ID: "t2", Owner: "w2", Reviewer: "r2", Status: "awaiting_review"},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActRunReviewer || act.Task != "t1" {
		t.Fatalf("got kind=%v task=%q, want RunReviewer t1 (first candidate)", act.Kind, act.Task)
	}
}

// A shipped feature is skipped for Merge even if (stale) all its tasks accepted;
// the other in_progress feature with all accepted is the merge target.
func TestNextAction_ShippedFeatureNotRemerged(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{
			{ID: "f1", Status: "shipped", Tasks: []projection.Task{
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "accepted"},
			}},
			{ID: "f2", Status: "in_progress", Tasks: []projection.Task{
				{ID: "t2", Owner: "w2", Reviewer: "r2", Status: "accepted"},
			}},
		},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActMerge || act.Feature != "f2" {
		t.Fatalf("got kind=%v feature=%q, want Merge f2 (f1 already shipped)", act.Kind, act.Feature)
	}
}

// Cross-feature deps: dep ids only resolve within the same feature. A task whose
// dep id matches an accepted task in ANOTHER feature is still blocked (dep not
// found in own feature → treated as unsatisfied).
func TestNextAction_DepsScopedToOwnFeature(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{
			{ID: "f1", Status: "in_progress", Tasks: []projection.Task{
				{ID: "shared", Owner: "w1", Reviewer: "r1", Status: "accepted"},
			}},
			{ID: "f2", Status: "in_progress", Tasks: []projection.Task{
				{ID: "t2", Owner: "w2", Reviewer: "r2", Status: "assigned", Deps: []string{"shared"}},
			}},
		},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	// Deps are validated same-feature at assign, so a dep id absent from t2's own
	// feature was cancelled — it can never reach accepted, and blocking t2 forever
	// would deadlock the run (depsSatisfied treats it as vacuously satisfied). t2
	// is runnable, and RunOwner outranks Merge.
	if act.Kind != ActRunOwner || act.Feature != "f2" || act.Task != "t2" {
		t.Fatalf("got kind=%v feature=%q task=%q, want RunOwner f2/t2 (dep absent from own feature → vacuously satisfied)", act.Kind, act.Feature, act.Task)
	}
}

// TestNextAction_ShippedTaskDoesNotTripStuck (review #1+#3): a shipped feature's
// task with a stale rework count must not preempt ActDone for a non-shipped
// feature that simply has no action available this tick (and accepted tasks in a
// live feature must not be named as the stuck cause).
func TestNextAction_ShippedTaskDoesNotTripStuck(t *testing.T) {
	st := projection.State{Features: []projection.Feature{
		{ID: "f1", Status: "shipped", Tasks: []projection.Task{
			{ID: "t1", Status: "accepted", Owner: "w", Reviewer: "r"},
		}},
	}}
	h := History{Rework: map[string]int{"t1": 5}, Fails: map[string]int{}}
	th := Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50}
	got := nextAction(st, h, th)
	if got.Kind != ActDone {
		t.Fatalf("shipped feature with stale rework count: got %v, want ActDone", got)
	}
}

// TestNextAction_InProgress_RunOwner (review, real prod bug): `pactify join` flips
// all of a seat's assigned tasks to in_progress; an un-checkpointed in_progress
// task (deps met) must be (re)dispatched to its owner, else multi-task features
// strand after join.
func TestNextAction_InProgress_RunOwner(t *testing.T) {
	st := projection.State{Features: []projection.Feature{
		{ID: "f1", Status: "in_progress", Tasks: []projection.Task{
			{ID: "t1", Status: "in_progress", Owner: "w", Reviewer: "r"},
		}},
	}}
	got := nextAction(st, emptyHistory(), defaultTh())
	if got.Kind != ActRunOwner || got.Task != "t1" || got.Seat != "w" {
		t.Fatalf("in_progress task: got %+v, want RunOwner t1/w", got)
	}
}

// In-progress dep gates a dependent in-progress task the same as assigned.
func TestNextAction_InProgressDepNotAccepted_Blocked(t *testing.T) {
	st := projection.State{Features: []projection.Feature{
		{ID: "f1", Status: "in_progress", Tasks: []projection.Task{
			{ID: "t1", Status: "in_progress", Owner: "w", Reviewer: "r"},
			{ID: "t2", Status: "in_progress", Owner: "w", Reviewer: "r", Deps: []string{"t1"}},
		}},
	}}
	// t1 is runnable (no deps); t2 blocked by unaccepted t1. First candidate = t1.
	got := nextAction(st, emptyHistory(), defaultTh())
	if got.Task != "t1" {
		t.Fatalf("got %+v, want t1 first (t2 dep-blocked)", got)
	}
}
