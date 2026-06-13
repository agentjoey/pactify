// Package orchestrate drives the pact state machine: it folds the event log into
// a projection.State, decides the next action (this file), and launches the
// corresponding agent. nextAction is the pure decision core — no IO — so every
// transition rule is unit-testable.
package orchestrate

import "github.com/agentjoey/pactify/internal/projection"

// ActionKind enumerates the decisions the driver can make for a given state.
type ActionKind int

const (
	ActIdle        ActionKind = iota // no action available and no threshold tripped (should not normally occur)
	ActRunOwner                      // launch the task owner (worker) to implement the task
	ActRunReviewer                   // launch the task reviewer to review the task
	ActMerge                         // every task in the feature is accepted → merge (hard gate enforced by the loop layer)
	ActStuck                         // a threshold was exceeded with unfinished work → escalate
	ActDone                          // all features shipped (or none) → finished
)

// Action is the result of nextAction. Feature/Task/Seat are populated for the
// kinds that reference them; Reason carries the explanation for ActStuck.
type Action struct {
	Kind    ActionKind
	Feature string // ActMerge / ActRunOwner / ActRunReviewer
	Task    string // ActRunOwner / ActRunReviewer / ActStuck (when task-scoped)
	Seat    string // ActRunOwner (owner) / ActRunReviewer (reviewer)
	Reason  string // ActStuck explanation
}

// History is the driver-maintained mutable state (NOT protocol state): per-task
// rework and consecutive-failure counts plus a global iteration count.
type History struct {
	Rework map[string]int // taskID -> observed changes_requested rounds
	Fails  map[string]int // taskID -> consecutive "no expected transition" failures after exec
	Iters  int            // total actions executed
}

// Thresholds bound the driver before it escalates to a human.
type Thresholds struct{ MaxRework, MaxFails, MaxIters int }

// nextAction is a pure function: it reads only state + history + thresholds and
// returns the next action. No IO.
//
// Priority across all features (deterministic, first candidate wins in
// feature-then-task order): RunReviewer > RunOwner > Merge. Falling through with
// unfinished work checks thresholds → Stuck, else Idle. When no feature has
// unfinished work, the result is Done.
func nextAction(st projection.State, h History, th Thresholds) Action {
	// 1. RunReviewer: any task awaiting_review (highest priority — drain in-flight reviews first).
	for _, f := range st.Features {
		if f.Status == "shipped" {
			continue
		}
		for _, t := range f.Tasks {
			if t.Status == "awaiting_review" {
				return Action{Kind: ActRunReviewer, Feature: f.ID, Task: t.ID, Seat: t.Reviewer}
			}
		}
	}

	// 2. RunOwner: any task assigned/changes_requested whose deps are all accepted.
	for _, f := range st.Features {
		if f.Status == "shipped" {
			continue
		}
		for _, t := range f.Tasks {
			if (t.Status == "assigned" || t.Status == "changes_requested") && depsSatisfied(f, t) {
				return Action{Kind: ActRunOwner, Feature: f.ID, Task: t.ID, Seat: t.Owner}
			}
		}
	}

	// 3. Merge: a non-shipped feature with at least one task, all accepted.
	for _, f := range st.Features {
		if f.Status == "shipped" {
			continue
		}
		if len(f.Tasks) > 0 && allAccepted(f) {
			return Action{Kind: ActMerge, Feature: f.ID}
		}
	}

	// 4. No action available. If everything is shipped (or there are no features
	//    at all), we're done.
	if allShipped(st) {
		return Action{Kind: ActDone}
	}

	// 5. Unfinished work but nothing actionable → threshold checks → Stuck, else Idle.
	//    Per-task thresholds first (so the offending task id is reported).
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if th.MaxRework > 0 && h.Rework[t.ID] >= th.MaxRework {
				return Action{Kind: ActStuck, Feature: f.ID, Task: t.ID, Reason: "rework limit exceeded"}
			}
			if th.MaxFails > 0 && h.Fails[t.ID] >= th.MaxFails {
				return Action{Kind: ActStuck, Feature: f.ID, Task: t.ID, Reason: "failure limit exceeded"}
			}
		}
	}
	if th.MaxIters > 0 && h.Iters >= th.MaxIters {
		return Action{Kind: ActStuck, Reason: "iteration limit exceeded"}
	}

	// 6. Theoretically unreachable: unfinished work, no action, no threshold.
	return Action{Kind: ActIdle}
}

// depsSatisfied reports whether every dependency of t is an accepted task within
// the SAME feature. Dep ids are scoped to the feature; a dep id not present in f
// is treated as unsatisfied.
func depsSatisfied(f projection.Feature, t projection.Task) bool {
	for _, dep := range t.Deps {
		if !acceptedInFeature(f, dep) {
			return false
		}
	}
	return true
}

func acceptedInFeature(f projection.Feature, taskID string) bool {
	for _, t := range f.Tasks {
		if t.ID == taskID {
			return t.Status == "accepted"
		}
	}
	return false
}

func allAccepted(f projection.Feature) bool {
	for _, t := range f.Tasks {
		if t.Status != "accepted" {
			return false
		}
	}
	return true
}

func allShipped(st projection.State) bool {
	for _, f := range st.Features {
		if f.Status != "shipped" {
			return false
		}
	}
	return true
}
