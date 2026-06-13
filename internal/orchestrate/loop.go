package orchestrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

// Run is the package entry point: it drives opts to completion (or pause). See
// Options.run for the loop semantics.
func Run(ctx context.Context, opts Options) error { return opts.run(ctx) }

// fallbackGate is the conservative full command run when a feature's tasks
// declare no machine-readable `verify:` command. It is intentionally broad so
// the hard gate never silently passes on an unverified feature (spec §4).
const fallbackGate = "go build ./... && go test ./..."

// Options carries everything the driver needs, with every external dependency
// injected so the loop is deterministically testable: the agent launcher
// (Run), the gate command executor (Exec), the human-escalation notifier
// (Notify), the timestamp source (Now), and the seat→kind mapping (SeatKind).
type Options struct {
	Dir      string                     // repo root
	Feature  string                     // limit the loop to this feature ("" = all features)
	Th       Thresholds                 // rework / fail / iteration bounds
	DryRun   bool                       // compute + print actions, never exec/merge/escalate
	Run      Runner                     // injected agent launcher (prod CmdRunner, test fake)
	Exec     cmdExec                    // injected gate command executor
	Notify   Notifier                   // injected escalation notifier
	Now      func() string              // injected timestamp for escalation filenames
	SeatKind func(seatID string) string // seat id → agent kind (prod parses roster, test injects)
}

// Run drives the pact state machine for opts.Dir until the targeted work is
// shipped, escalated, or (dry-run) previewed. It is serial: read state →
// nextAction → launch agent → reproject → repeat, with a deterministic hard
// gate ahead of every merge.
//
// On escalation (rework/fail thresholds, an unreachable Idle, or a failed hard
// gate) it writes an escalation record, notifies, and returns nil — paused, not
// failed; a human fixes the cause and reruns to resume. A genuine error
// (unreadable state, a Runner/Merge failure) is returned.
func (opts Options) run(ctx context.Context) error {
	h := History{Rework: map[string]int{}, Fails: map[string]int{}}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		st, err := pact.At(opts.Dir).StateProjection()
		if err != nil {
			return fmt.Errorf("orchestrate: read state: %w", err)
		}
		view := opts.filtered(st)

		act := nextAction(view, h, opts.Th)

		// Threshold guard for the churn loop. nextAction only escalates from its
		// idle path (no available action), but a task stuck in a rework or
		// fail cycle always HAS an action (rerun owner/reviewer), so the idle path
		// is never reached. Enforce per-task bounds here, before dispatching the
		// action that would spin, so the offending task escalates instead.
		if !opts.DryRun && (act.Kind == ActRunOwner || act.Kind == ActRunReviewer) {
			if reason, tripped := tripped(act.Task, h, opts.Th); tripped {
				return opts.escalate(act.Task, reason, evidenceFor(st, act.Task),
					"人工介入后 pactify orchestrate 续跑")
			}
		}
		if !opts.DryRun && opts.Th.MaxIters > 0 && h.Iters >= opts.Th.MaxIters {
			return opts.escalate("", "iteration limit exceeded", "(global cap)",
				"放宽 --max-iters 或检查为何 task 图迟迟不收敛")
		}

		if opts.DryRun {
			// Dry-run is a side-effect-free preview: print the next action (and the
			// command it would exec) and stop. We cannot advance the real state, so
			// printing a full sequence would loop forever; one step is the honest,
			// testable preview (spec §5).
			opts.previewNotify(view, act)
			return nil
		}

		switch act.Kind {
		case ActDone:
			return nil

		case ActRunOwner:
			if err := opts.runOwner(ctx, st, &h, act); err != nil {
				return err
			}

		case ActRunReviewer:
			if err := opts.runReviewer(ctx, st, &h, act); err != nil {
				return err
			}

		case ActMerge:
			done, err := opts.merge(ctx, st, act)
			if err != nil {
				return err
			}
			if done {
				// Escalated (gate failed): paused, not failed.
				return nil
			}

		case ActStuck:
			return opts.escalate(act.Task, act.Reason, evidenceFor(st, act.Task),
				"人工介入后 pactify orchestrate 续跑")

		case ActIdle:
			// Theoretically unreachable (unfinished work, no action, no threshold
			// tripped). Treat defensively as Stuck so the driver never spins.
			return opts.escalate(act.Task, "driver idle with unfinished work (unexpected)",
				evidenceFor(st, act.Task), "检查 task 图/依赖是否成环或卡死，修后续跑")
		}

		h.Iters++
	}
}

// runOwner launches the task owner with a worker briefing (carrying the most
// recent changes-requested reason, if any), then reprojects and records a
// failure if the task did not reach awaiting_review.
func (opts Options) runOwner(ctx context.Context, st projection.State, h *History, act Action) error {
	seat, task, ok := find(st, act.Feature, act.Task)
	if !ok {
		return fmt.Errorf("orchestrate: task %s not found for RunOwner", act.Task)
	}
	reason := lastChangesReason(opts.Dir, act.Task)
	brief := workerBrief(seatFor(st, act.Seat, seat), task, reason)

	if runErr := opts.Run.Run(ctx, task.Owner, opts.kind(task.Owner), brief, opts.Dir); runErr != nil {
		if ctx.Err() != nil {
			return runErr // cancellation: propagate, don't count as a task failure
		}
		// A transient agent crash (OOM / timeout / non-zero exit) is a soft failure,
		// not a driver-killer (spec §2.5): count it and let the next iteration's
		// tripped() guard escalate if it persists.
		h.Fails[act.Task]++
		return nil
	}

	after, err := pact.At(opts.Dir).StateProjection()
	if err != nil {
		return fmt.Errorf("orchestrate: reproject after owner: %w", err)
	}
	if _, t, ok := find(after, act.Feature, act.Task); !ok || t.Status != "awaiting_review" {
		h.Fails[act.Task]++
	} else {
		h.Fails[act.Task] = 0 // consecutive: a successful checkpoint clears the run
	}
	return nil
}

// runReviewer launches the task reviewer with a reviewer briefing, then
// reprojects: changes_requested bumps the rework count; neither accepted nor
// changes_requested bumps the failure count.
func (opts Options) runReviewer(ctx context.Context, st projection.State, h *History, act Action) error {
	_, task, ok := find(st, act.Feature, act.Task)
	if !ok {
		return fmt.Errorf("orchestrate: task %s not found for RunReviewer", act.Task)
	}
	brief := reviewerBrief(projection.Seat{ID: act.Seat}, task)

	if runErr := opts.Run.Run(ctx, task.Reviewer, opts.kind(task.Reviewer), brief, opts.Dir); runErr != nil {
		if ctx.Err() != nil {
			return runErr // cancellation: propagate, don't count as a task failure
		}
		h.Fails[act.Task]++ // transient agent crash → soft failure (spec §2.5)
		return nil
	}

	after, err := pact.At(opts.Dir).StateProjection()
	if err != nil {
		return fmt.Errorf("orchestrate: reproject after reviewer: %w", err)
	}
	_, t, ok := find(after, act.Feature, act.Task)
	switch {
	case ok && t.Status == "changes_requested":
		h.Rework[act.Task]++
		h.Fails[act.Task] = 0 // the review ran (progress): clear the consecutive count
	case ok && t.Status == "accepted":
		h.Fails[act.Task] = 0
	default:
		h.Fails[act.Task]++
	}
	return nil
}

// merge runs the independent hard gate over the feature's tasks, then merges on
// PASS. A gate FAIL escalates (returns done=true so the loop pauses). The hard
// gate is deliberately redundant with the LLM reviewer's own run: a deterministic
// safety net beneath "LLM accepted" (spec §2.4).
func (opts Options) merge(ctx context.Context, st projection.State, act Action) (done bool, err error) {
	var feat *projection.Feature
	for fi := range st.Features {
		if st.Features[fi].ID == act.Feature {
			feat = &st.Features[fi]
		}
	}
	if feat == nil {
		return false, fmt.Errorf("orchestrate: feature %s not found for Merge", act.Feature)
	}

	for _, cmd := range gateCommands(opts.Dir, *feat) {
		ok, detail := runGate(ctx, opts.Exec, opts.Dir, cmd)
		if !ok {
			return true, opts.escalate(act.Feature, "hard gate failed: "+detail,
				evidenceFor(st, ""), "修复实现/规格后 pactify orchestrate 续跑")
		}
	}

	if err := pact.At(opts.Dir).Merge(act.Feature); err != nil {
		return false, fmt.Errorf("orchestrate: merge %s: %w", act.Feature, err)
	}
	return false, nil
}

// gateCommands collects the deduplicated set of verify commands for a feature's
// tasks. A task whose spec declares no `verify:` line contributes the
// conservative full fallback command instead, so the feature is never merged
// unverified.
func gateCommands(dir string, f projection.Feature) []string {
	seen := map[string]bool{}
	var cmds []string
	add := func(c string) {
		if c != "" && !seen[c] {
			seen[c] = true
			cmds = append(cmds, c)
		}
	}
	for _, t := range f.Tasks {
		spec := readSpec(dir, t.Spec)
		if cmd, ok := extractVerify(spec); ok {
			add(cmd)
		} else {
			add(fallbackGate)
		}
	}
	if len(cmds) == 0 {
		cmds = append(cmds, fallbackGate)
	}
	return cmds
}

// escalate writes the escalation record and notifies. It returns nil even on a
// write/notify failure path's own error only when the record cannot be written,
// because escalation is a pause, not a hard stop — but a write error is a real
// IO failure worth surfacing.
func (opts Options) escalate(task, reason, evidence, suggestion string) error {
	// Now is injected for deterministic test filenames; fall back to wall-clock so
	// a caller that forgets to wire it doesn't panic at the worst moment (escalation).
	ts := time.Now().Format("20060102-150405")
	if opts.Now != nil {
		ts = opts.Now()
	}
	path, err := writeEscalation(opts.Dir, ts, task, reason, evidence, suggestion)
	if err != nil {
		return fmt.Errorf("orchestrate: write escalation: %w", err)
	}
	if opts.Notify != nil {
		opts.Notify.Notify(fmt.Sprintf("orchestrate paused: %s — see %s", reason, path))
	}
	return nil
}

// previewNotify prints (via Notify when present) the dry-run plan for one action.
func (opts Options) previewNotify(st projection.State, act Action) {
	if opts.Notify == nil {
		return
	}
	opts.Notify.Notify("dry-run: " + describe(st, opts.Dir, act))
}

// filtered narrows the state to opts.Feature when set, so the loop only drives
// the requested feature. An empty Feature drives everything.
func (opts Options) filtered(st projection.State) projection.State {
	if opts.Feature == "" {
		return st
	}
	out := st
	out.Features = nil
	for _, f := range st.Features {
		if f.ID == opts.Feature {
			out.Features = append(out.Features, f)
		}
	}
	return out
}

// kind maps a seat id to its agent kind via the injected mapper, defaulting to
// the empty string (the Runner then fails closed on an unknown kind).
func (opts Options) kind(seatID string) string {
	if opts.SeatKind == nil {
		return ""
	}
	return opts.SeatKind(seatID)
}

// describe renders a one-line dry-run summary of an action and the command it
// would run, without side effects.
func describe(st projection.State, dir string, act Action) string {
	switch act.Kind {
	case ActRunOwner:
		return fmt.Sprintf("RunOwner task=%s seat=%s", act.Task, act.Seat)
	case ActRunReviewer:
		return fmt.Sprintf("RunReviewer task=%s seat=%s", act.Task, act.Seat)
	case ActMerge:
		var feat projection.Feature
		for _, f := range st.Features {
			if f.ID == act.Feature {
				feat = f
			}
		}
		return fmt.Sprintf("Merge feature=%s gate=[%s]", act.Feature, strings.Join(gateCommands(dir, feat), " ; "))
	case ActStuck:
		return fmt.Sprintf("Stuck task=%s reason=%s", act.Task, act.Reason)
	case ActDone:
		return "Done"
	default:
		return "Idle"
	}
}

// tripped reports whether task has exceeded the rework or fail bound, with a
// human-readable reason for the escalation record.
func tripped(task string, h History, th Thresholds) (string, bool) {
	if th.MaxRework > 0 && h.Rework[task] >= th.MaxRework {
		return "rework limit exceeded", true
	}
	if th.MaxFails > 0 && h.Fails[task] >= th.MaxFails {
		return "failure limit exceeded", true
	}
	return "", false
}

// --- small state/log helpers -------------------------------------------------

// find returns the seat (by owner id) and task for a feature/task pair.
func find(st projection.State, feature, task string) (projection.Seat, projection.Task, bool) {
	for _, f := range st.Features {
		if f.ID != feature {
			continue
		}
		for _, t := range f.Tasks {
			if t.ID == task {
				return seatFor(st, t.Owner, projection.Seat{ID: t.Owner}), t, true
			}
		}
	}
	return projection.Seat{}, projection.Task{}, false
}

// seatFor resolves a seat's full roster entry (id + roles) by id, falling back
// to the supplied default when the roster has no such seat.
func seatFor(st projection.State, seatID string, fallback projection.Seat) projection.Seat {
	for _, s := range st.Agents {
		if s.ID == seatID {
			return s
		}
	}
	return fallback
}

// lastChangesReason returns the reason from the most recent changes_requested
// event for task, or "" if the task has never been sent back.
func lastChangesReason(dir, task string) string {
	evs, err := event.ReadAll(paths.LogIn(dir))
	if err != nil {
		return ""
	}
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.EventType == "changes_requested" && e.TaskID == task {
			if r, ok := e.Payload["reason"].(string); ok {
				return r
			}
			return ""
		}
	}
	return ""
}

// evidenceFor pulls the most recent evidence for a task from state (the
// checkpoint evidence projected onto the task), or a placeholder when absent or
// when task is empty (feature-level escalation).
func evidenceFor(st projection.State, task string) string {
	if task == "" {
		return "(feature-level gate; see gate detail in reason)"
	}
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if t.ID == task && t.Evidence != nil {
				return *t.Evidence
			}
		}
	}
	return "(no checkpoint evidence recorded)"
}

// readSpec reads a task spec file. task.Spec is a repo-relative path; an
// unreadable spec yields "" so extractVerify falls back to the full gate.
func readSpec(dir, specRel string) string {
	if specRel == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, specRel))
	if err != nil {
		return ""
	}
	return string(b)
}
