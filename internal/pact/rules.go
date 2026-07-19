package pact

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

func taskExists(st projection.State, taskID string) bool {
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if t.ID == taskID {
				return true
			}
		}
	}
	return false
}

func findTask(st projection.State, taskID string) (*projection.Task, *projection.Feature) {
	for fi := range st.Features {
		for ti := range st.Features[fi].Tasks {
			if st.Features[fi].Tasks[ti].ID == taskID {
				return &st.Features[fi].Tasks[ti], &st.Features[fi]
			}
		}
	}
	return nil, nil
}

// checkAssign validates an assignment. reviewers is the reviewer set (a single-
// reviewer assign passes a one-element slice; a quorum assign passes the full
// list); quorum is 0 for the legacy single-reviewer path and ≥1 for a quorum
// assign. The single-reviewer path (quorum==0, one reviewer) produces byte-
// identical error messages to before, keeping the golden path unchanged.
func checkAssign(st projection.State, actingID, taskID, feature, branch, owner string, reviewers []string, quorum int) error {
	if err := requireOrchestrator(st, "assign", actingID); err != nil {
		return err
	}
	if owner == "" || len(reviewers) == 0 {
		return fmt.Errorf("pactify assign: --owner and --reviewer required")
	}
	// Separation of duties generalizes to the whole reviewer set: the owner may not
	// review its own work under any reviewer slot.
	for _, rv := range reviewers {
		if owner == rv {
			return fmt.Errorf("pactify assign: owner (%s) must differ from reviewer (separation of duties)", owner)
		}
	}
	// Quorum-mode sanity (opt-in only): reviewers must be distinct and non-empty and
	// the quorum must be attainable (1 ≤ quorum ≤ #reviewers). Skipped entirely for
	// the legacy single-reviewer path (quorum==0) so its validation is unchanged.
	if quorum > 0 {
		seen := map[string]bool{}
		for _, rv := range reviewers {
			if rv == "" {
				return fmt.Errorf("pactify assign: --reviewers contains an empty seat")
			}
			if seen[rv] {
				return fmt.Errorf("pactify assign: --reviewers contains a duplicate seat %q", rv)
			}
			seen[rv] = true
		}
		if quorum > len(reviewers) {
			return fmt.Errorf("pactify assign: --quorum %d exceeds the %d reviewer(s)", quorum, len(reviewers))
		}
	}
	// Identifier hygiene: taskID and feature flow unquoted into git argv and
	// commit messages later (CheckoutOrCreate, MergeNoFF, AddWorktree), where a
	// crafted value (leading "-", spaces) reads as a git flag. Hold both to the
	// seat-id slug pattern so nothing hostile ever reaches git.
	if !IsSlug(taskID) {
		return fmt.Errorf("pactify assign: task id %q is not a slug (lowercase kebab, e.g. t1-parse-args)", taskID)
	}
	if !IsSlug(feature) {
		return fmt.Errorf("pactify assign: feature %q is not a slug (lowercase kebab)", feature)
	}
	// A branch legitimately contains "/" so it is not a slug; vet it as a git
	// branch name instead. Empty stays allowed — an in-place feature declares no
	// branch.
	if branch != "" && !gitx.ValidBranchName(branch) {
		return fmt.Errorf("pactify assign: branch %q is not a valid git branch name", branch)
	}
	if taskExists(st, taskID) {
		return fmt.Errorf("pactify assign: task %s already exists", taskID)
	}
	return nil
}

// checkAddSeat validates an add-seat: the acting seat must have the orchestrator
// role (roster management is the orchestrator's job), the new seat id must be
// unique across the roster, and its roles must be known.
func checkAddSeat(st projection.State, actingID string, seat Seat) error {
	if !seatHasRole(st, actingID, "orchestrator") {
		return fmt.Errorf("pactify seat add: acting seat %q must have the orchestrator role", actingID)
	}
	for _, a := range st.Agents {
		if a.ID == seat.ID {
			return fmt.Errorf("pactify seat add: seat id %q already exists", seat.ID)
		}
	}
	for _, r := range seat.Roles {
		if r != "orchestrator" && r != "reviewer" && r != "worker" {
			return fmt.Errorf("pactify seat add: invalid role %q (want orchestrator/reviewer/worker)", r)
		}
	}
	return nil
}

// requireOrchestrator gates a coordination verb: only a roster seat carrying the
// orchestrator role may drive it (same construction as checkAddSeat's gate). This
// is what keeps a registered worker seat from fabricating assignments, retiring
// someone's work (cancel/withdraw), rewriting the safety gate or base branch, or
// driving merges — those verbs shape the shared plan, and the roster's role
// declaration is the authority for who may.
func requireOrchestrator(st projection.State, verb, actingID string) error {
	if !seatHasRole(st, actingID, "orchestrator") {
		return fmt.Errorf("pactify %s: acting seat %q must have the orchestrator role", verb, actingID)
	}
	return nil
}

// seatHasRole reports whether the roster seat id carries role.
func seatHasRole(st projection.State, id, role string) bool {
	for _, a := range st.Agents {
		if a.ID != id {
			continue
		}
		for _, r := range a.Roles {
			if r == role {
				return true
			}
		}
	}
	return false
}

// checkDeps validates a new task's deps at assign time (additive v1):
//   - no self-dependency,
//   - every dep already exists in the SAME feature,
//   - adding the new task's edges introduces no cycle (DFS over the feature's
//     existing deps plus the new edge).
func checkDeps(st projection.State, taskID, feature string, deps []string) error {
	if len(deps) == 0 {
		return nil
	}
	// Build the existing dep graph for this feature: task id -> its deps.
	graph := map[string][]string{}
	var feat *projection.Feature
	for fi := range st.Features {
		if st.Features[fi].ID == feature {
			feat = &st.Features[fi]
		}
	}
	if feat != nil {
		for _, t := range feat.Tasks {
			graph[t.ID] = t.Deps
		}
	}
	for _, d := range deps {
		if d == taskID {
			// A self edge is the smallest cycle; report it as such so the
			// cycle guard is the single source of truth for back-edges.
			return fmt.Errorf("pactify assign: task %s introduces a dependency cycle (self-dep)", taskID)
		}
		tk, df := findTask(st, d)
		if tk == nil {
			return fmt.Errorf("pactify assign: unknown dep %s for task %s", d, taskID)
		}
		if df == nil || df.ID != feature {
			return fmt.Errorf("pactify assign: dep %s must be in the same feature as %s (%s)", d, taskID, feature)
		}
	}
	// Add the new node's edges and run a cycle check reachable from taskID.
	graph[taskID] = deps
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var dfs func(n string) bool
	dfs = func(n string) bool {
		color[n] = gray
		for _, m := range graph[n] {
			switch color[m] {
			case gray:
				return true // back-edge -> cycle
			case white:
				if dfs(m) {
					return true
				}
			}
		}
		color[n] = black
		return false
	}
	if dfs(taskID) {
		return fmt.Errorf("pactify assign: task %s introduces a dependency cycle", taskID)
	}
	return nil
}

// checkJoinGate blocks a seat's join ONLY when it has startable work that is all
// dep-blocked — i.e. nothing it could legitimately begin yet. A seat with at least
// one runnable task may join; a future task's unmet dep must not strand a ready
// one (the old gate failed the whole join, leaving the feature branch uncreated).
// The dep gate is then enforced when each task is actually started (orchestrate's
// nextAction checks depsSatisfied), not across the whole roster at join time.
// checkJoinTarget validates a TARGETED join (join --task): the named task must
// exist, be owned by the joining seat, be in a startable/resumable status, and
// have every dep accepted. Unlike the seat-scoped gate below, every refusal
// names the task the worker was actually told to work — the seat-scoped gate's
// error could point at a sibling task the worker wasn't even trying to start.
func checkJoinTarget(st projection.State, seatID, taskID string) error {
	t, _ := findTask(st, taskID)
	if t == nil {
		return fmt.Errorf("pactify join: unknown task %q", taskID)
	}
	if t.Owner != seatID {
		return fmt.Errorf("pactify join: task %s is owned by %s, not %s", taskID, t.Owner, seatID)
	}
	switch t.Status {
	case "assigned", "changes_requested", "in_progress":
		// startable or resumable
	default:
		return fmt.Errorf("pactify join: task %s is %s — not startable", taskID, t.Status)
	}
	for _, d := range t.Deps {
		// Same dep semantics as the seat-scoped gate: a dep absent from the
		// projection was cancelled and is vacuously satisfied.
		dep, _ := findTask(st, d)
		if dep != nil && dep.Status != "accepted" {
			return fmt.Errorf("pactify join: task %s blocked by unaccepted dep %s", taskID, d)
		}
	}
	return nil
}

func checkJoinGate(st projection.State, seatID string) error {
	startable, runnable, resumable := 0, 0, 0
	firstBlocked, firstBlockedDep := "", ""
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if t.Owner != seatID {
				continue
			}
			// A task already in_progress is resumable work: the seat may re-join
			// to pick it back up (cold-start after a crash, a fresh worker run on
			// the same seat), so it must keep the gate open even when every other
			// owned task is dep-blocked.
			if t.Status == "in_progress" {
				resumable++
				continue
			}
			// Only assigned/changes_requested tasks are "startable" at join; an
			// already accepted/shipped task doesn't gate a (re)join.
			if t.Status != "assigned" && t.Status != "changes_requested" {
				continue
			}
			startable++
			blockedDep := ""
			for _, d := range t.Deps {
				// A dep absent from the projection was cancelled: it can never
				// reach accepted, so blocking the dependent forever is strictly
				// worse — the orchestrator retired it deliberately. Vacuously
				// satisfied; only a still-present unaccepted dep blocks.
				dep, _ := findTask(st, d)
				if dep != nil && dep.Status != "accepted" {
					blockedDep = d
					break
				}
			}
			if blockedDep == "" {
				runnable++
			} else if firstBlocked == "" {
				firstBlocked, firstBlockedDep = t.ID, blockedDep
			}
		}
	}
	if startable > 0 && runnable == 0 && resumable == 0 {
		return fmt.Errorf("pactify join: task %s blocked by unaccepted dep %s (no runnable task to start)", firstBlocked, firstBlockedDep)
	}
	return nil
}

func checkCheckpoint(st projection.State, caller, taskID, evidence string) (*projection.Feature, error) {
	if evidence == "" {
		return nil, fmt.Errorf("pactify checkpoint: --evidence required")
	}
	const maxEvidence = 1 << 20
	if len(evidence) > maxEvidence {
		return nil, fmt.Errorf("pactify checkpoint: evidence exceeds %d byte limit; use a shorter summary or link", maxEvidence)
	}
	tk, f := findTask(st, taskID)
	if tk == nil {
		return nil, fmt.Errorf("pactify checkpoint: unknown task %s", taskID)
	}
	if tk.Owner != caller {
		return nil, fmt.Errorf("pactify checkpoint: %s is not the owner of %s (owner: %s)", caller, taskID, tk.Owner)
	}
	// Close the join-gate ordering hole: a seat that joined BEFORE a dep'd assign
	// could otherwise checkpoint a still-blocked task. Re-apply the same dep gate
	// the join uses, but for this single task.
	for _, d := range tk.Deps {
		// A dep absent from the projection was cancelled: it can never reach
		// accepted, so blocking this task forever is strictly worse — the
		// orchestrator retired it deliberately. Vacuously satisfied.
		dep, _ := findTask(st, d)
		if dep != nil && dep.Status != "accepted" {
			return nil, fmt.Errorf("pactify checkpoint: task %s blocked by unaccepted dep %s", taskID, d)
		}
	}
	return f, nil
}

func checkMerge(st projection.State, actingID, feature string) error {
	if err := requireOrchestrator(st, "merge", actingID); err != nil {
		return err
	}
	var feat *projection.Feature
	for fi := range st.Features {
		if st.Features[fi].ID == feature {
			feat = &st.Features[fi]
		}
	}
	if feat == nil || len(feat.Tasks) == 0 {
		return fmt.Errorf("pactify merge: unknown feature %s (or it has no tasks)", feature)
	}
	for _, t := range feat.Tasks {
		if t.Status != "accepted" {
			return fmt.Errorf("pactify merge: cannot merge %s; task %s not accepted", feature, t.ID)
		}
	}
	return nil
}

func featureBranch(st projection.State, feature string) string {
	for _, f := range st.Features {
		if f.ID == feature {
			return f.Branch
		}
	}
	return ""
}

// projectProtocolVersion returns the protocol_version declared by the init event,
// or 0 when none is declared (legacy logs).
func projectProtocolVersion(evs []event.Event) int {
	for _, e := range evs {
		if e.EventType == "init" {
			if pv, ok := e.Payload["protocol_version"].(float64); ok {
				return int(pv)
			}
		}
	}
	return 0
}

func checkReviewerVerdict(st projection.State, verb, caller, taskID string) (*projection.Feature, error) {
	tk, f := findTask(st, taskID)
	if tk == nil {
		return nil, fmt.Errorf("pactify %s: unknown task %s", verb, taskID)
	}
	// The acting seat must be an authorized reviewer. This is the single-reviewer
	// guard generalized to the quorum reviewer set: a quorum task authorizes any
	// seat in tk.Reviewers; a legacy task authorizes only tk.Reviewer (unchanged,
	// byte-identical error message). A worker is never in either set (assign enforces
	// owner ∉ reviewers), so worker self-accept stays rejected — the sacred rule.
	if !reviewerAllowed(tk, caller) {
		if len(tk.Reviewers) > 0 {
			return nil, fmt.Errorf("pactify %s: only a reviewer (%s) may review %s; you are %s", verb, strings.Join(tk.Reviewers, ", "), taskID, caller)
		}
		return nil, fmt.Errorf("pactify %s: only the reviewer (%s) may review %s; you are %s", verb, tk.Reviewer, taskID, caller)
	}
	if tk.Status != "awaiting_review" {
		return nil, fmt.Errorf("pactify %s: %s is not awaiting_review (status: %s)", verb, taskID, tk.Status)
	}
	return f, nil
}

// reviewerAllowed reports whether caller is an authorized reviewer of tk: any seat
// in the quorum reviewer set when one is declared, else the single reviewer.
func reviewerAllowed(tk *projection.Task, caller string) bool {
	if len(tk.Reviewers) > 0 {
		for _, r := range tk.Reviewers {
			if r == caller {
				return true
			}
		}
		return false
	}
	return tk.Reviewer == caller
}

// ValidateLog runs the v1 conformance checks against the cwd's log + STATE.
func ValidateLog() error { return At(".").validateLog() }

// validateLog runs the v1 conformance checks against the log + rendered STATE.
func (p *Project) validateLog() error {
	evs, err := event.ReadAll(paths.LogIn(p.dir))
	if err != nil {
		return err
	}
	st := projection.Project(evs)

	// (a) STATE.yml must match a fresh render of the log. A missing/unreadable
	// STATE.yml is itself drift (matches bash, whose diff fails when STATE is absent).
	if b, err := os.ReadFile(paths.StateIn(p.dir)); err != nil || string(b) != projection.Render(st) {
		return fmt.Errorf("pactify validate: STATE.yml drift vs render(log)")
	}
	declared := map[string]bool{}
	for _, a := range st.Agents {
		declared[a.ID] = true
	}

	if protocolVersion := projectProtocolVersion(evs); protocolVersion > paths.ProtocolVersion {
		return fmt.Errorf("pactify validate: protocol_version %d exceeds supported %d; upgrade pactify", protocolVersion, paths.ProtocolVersion)
	}
	for _, e := range evs {
		if e.EventID == "" {
			return fmt.Errorf("pactify validate: event missing event_id")
		}
		if !declared[e.AgentID] {
			return fmt.Errorf("pactify validate: agent_id %q not in seat roster", e.AgentID)
		}
		if !slugRe.MatchString(e.AgentID) {
			return fmt.Errorf("pactify validate: agent_id %q is not a slug", e.AgentID)
		}
	}
	seen := map[string]bool{}
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if t.Owner == t.Reviewer {
				return fmt.Errorf("pactify validate: rule1 violation (owner==reviewer) in task %s", t.ID)
			}
			if seen[t.ID] {
				return fmt.Errorf("pactify validate: duplicate task id %s", t.ID)
			}
			seen[t.ID] = true
		}
	}
	return nil
}
