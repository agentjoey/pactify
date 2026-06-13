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
func TestNextAction_OnlyBlockedTask_Idle(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				// t0 is in_progress (not actionable, not accepted); t1 depends on
				// t0 so it cannot run either → no action, no threshold → Idle.
				{ID: "t0", Owner: "w0", Reviewer: "r1", Status: "in_progress"},
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "assigned", Deps: []string{"t0"}},
			},
		}},
	}
	act := nextAction(st, emptyHistory(), defaultTh())
	if act.Kind != ActIdle {
		t.Fatalf("kind = %v, want ActIdle (t1 blocked, t0 in_progress not actionable)", act.Kind)
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

// changes_requested blocked by an unaccepted dep is NOT actionable.
func TestNextAction_ChangesRequestedDepNotAccepted_Idle(t *testing.T) {
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
	if act.Kind != ActIdle {
		t.Fatalf("kind = %v, want ActIdle (changes_requested blocked by dep)", act.Kind)
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

// g) no actionable task but unfinished work + Rework[t] >= MaxRework → Stuck(task)
func TestNextAction_ReworkExceeded_Stuck(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				// blocked by an unaccepted dep so no normal action fires
				{ID: "t0", Owner: "w0", Reviewer: "r1", Status: "in_progress"},
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "assigned", Deps: []string{"t0"}},
			},
		}},
	}
	h := emptyHistory()
	h.Rework["t1"] = 3
	act := nextAction(st, h, Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50})
	if act.Kind != ActStuck || act.Task != "t1" {
		t.Fatalf("got kind=%v task=%q, want Stuck t1", act.Kind, act.Task)
	}
}

// h) Fails[t] >= MaxFails → Stuck
func TestNextAction_FailsExceeded_Stuck(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t0", Owner: "w0", Reviewer: "r1", Status: "in_progress"},
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "assigned", Deps: []string{"t0"}},
			},
		}},
	}
	h := emptyHistory()
	h.Fails["t1"] = 3
	act := nextAction(st, h, Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50})
	if act.Kind != ActStuck || act.Task != "t1" {
		t.Fatalf("got kind=%v task=%q, want Stuck t1", act.Kind, act.Task)
	}
}

// i) Iters >= MaxIters → Stuck
func TestNextAction_ItersExceeded_Stuck(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{{
			ID: "f1", Status: "in_progress",
			Tasks: []projection.Task{
				{ID: "t0", Owner: "w0", Reviewer: "r1", Status: "in_progress"},
				{ID: "t1", Owner: "w1", Reviewer: "r1", Status: "assigned", Deps: []string{"t0"}},
			},
		}},
	}
	h := emptyHistory()
	h.Iters = 50
	act := nextAction(st, h, Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50})
	if act.Kind != ActStuck {
		t.Fatalf("kind = %v, want ActStuck (iters exceeded)", act.Kind)
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
	// f1 has all tasks accepted → mergeable. f2.t2 is blocked (dep "shared" not in
	// f2). So the only available action is Merge f1.
	if act.Kind != ActMerge || act.Feature != "f1" {
		t.Fatalf("got kind=%v feature=%q task=%q, want Merge f1 (t2 dep is cross-feature → blocked)", act.Kind, act.Feature, act.Task)
	}
}
