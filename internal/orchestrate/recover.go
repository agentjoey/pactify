package orchestrate

import (
	"context"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/projection"
)

// classifyAndCheckpoint runs the recovery classification after a worker soft-fail:
// it re-runs the task's verify command and, if it now passes, the work is actually
// finished and only the checkpoint was missing — it records a checkpoint as the
// owner (carrying the gate output as evidence) and returns true, so the driver
// routes the task to its reviewer instead of re-burning the worker. A failing or
// erroring verify returns false: the work is genuinely incomplete, keep retrying.
//
// It uses the task's own `verify:` line (per-task, not the feature-wide gate),
// falling back to fallbackGate when the spec carries none.
//
// delivered is the caller's launch-window delivery signal (tree fingerprint
// before vs after the stint — see runOwner). A green verify only means "the
// work is finished" when there IS work: a worker killed before producing
// anything (mid-setup, mid-sleep) changes nothing, and with a weak gate
// (`verify: true`) the rescue would checkpoint a phantom delivery AND clear
// the fail budget, so the timeout loop could burn forever without tripping
// escalation (2026-07-19 Phase C rerun F2-b). No delivery → no rescue.
func (opts Options) classifyAndCheckpoint(ctx context.Context, taskID string, task projection.Task, delivered bool) bool {
	if !delivered {
		return false
	}
	cmd, ok := extractVerify(readSpec(opts.Dir, task.Spec))
	if !ok {
		cmd = fallbackGate
	}
	passed, detail := runGateScoped(ctx, opts.Exec, opts.Dir, cmd, opts.projectBase())
	if !passed {
		return false
	}
	ev := "verify passed on recovery:\n" + summarize(detail)
	return pact.At(opts.Dir).As(task.Owner).Checkpoint(taskID, ev) == nil
}
