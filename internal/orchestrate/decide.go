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

	// 2. RunOwner: any task assigned/changes_requested/in_progress whose deps are
	//    all accepted. in_progress is included because `pactify join` flips EVERY
	//    of a seat's assigned tasks to in_progress at once (projection.go), so a
	//    task the worker hasn't checkpointed yet sits in in_progress — the driver
	//    must (re-)launch the owner to work it (this also gives crash-retry: a
	//    worker that died mid-task is relaunched until the Fails threshold trips).
	for _, f := range st.Features {
		if f.Status == "shipped" {
			continue
		}
		for _, t := range f.Tasks {
			if (t.Status == "assigned" || t.Status == "changes_requested" || t.Status == "in_progress") && depsSatisfied(f, t) {
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

	// 5. Unfinished work but nothing actionable. With in_progress now a runnable
	//    state (section 2), every non-accepted task IS actionable, so this point is
	//    only reached defensively. Thresholds/escalation are NOT decided here: a
	//    churning task always has an action (relaunch its owner), so a threshold
	//    check in this pure function would never fire. The loop owns History and
	//    enforces the rework/fail/iteration bounds via tripped() BEFORE dispatching
	//    an action, then escalates. Here we just report "nothing to do" and let the
	//    loop treat it defensively.
	return Action{Kind: ActIdle}
}

// featureAction returns the next runnable action for a SINGLE feature, using the
// same priority as nextAction (RunReviewer > RunOwner > Merge). ok=false when the
// feature is shipped or has no runnable work. This is the per-feature decision
// core shared by the parallel driver: a feature's tasks share a branch/worktree,
// so a feature advances by one action at a time.
func featureAction(f projection.Feature, h History, th Thresholds) (Action, bool) {
	if f.Status == "shipped" {
		return Action{}, false
	}
	for _, t := range f.Tasks {
		if t.Status == "awaiting_review" {
			return Action{Kind: ActRunReviewer, Feature: f.ID, Task: t.ID, Seat: t.Reviewer}, true
		}
	}
	for _, t := range f.Tasks {
		if (t.Status == "assigned" || t.Status == "changes_requested" || t.Status == "in_progress") && depsSatisfied(f, t) {
			return Action{Kind: ActRunOwner, Feature: f.ID, Task: t.ID, Seat: t.Owner}, true
		}
	}
	if len(f.Tasks) > 0 && allAccepted(f) {
		return Action{Kind: ActMerge, Feature: f.ID}, true
	}
	return Action{}, false
}

// nextActions returns one runnable action per feature that has work — the
// parallel analogue of nextAction. Features are independent (separate
// branches/worktrees) so the driver can run these concurrently; within a feature
// the action follows featureAction's priority. Merges are included but the driver
// serializes them onto the shared base branch. Threshold/escalation handling is
// the driver's job (it owns History), applied per action before dispatch — just
// like the serial loop's tripped() guard.
func nextActions(st projection.State, h History, th Thresholds) []Action {
	var acts []Action
	for _, f := range st.Features {
		if act, ok := featureAction(f, h, th); ok {
			acts = append(acts, act)
		}
	}
	return acts
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
