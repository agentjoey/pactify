package orchestrate

import "github.com/agentjoey/pactify/internal/projection"

// Budget is one task's effective verification budget (spec execution-tiering §5
// WS-B), resolved per task from its spec's tier with explicit operator flags
// taking precedence. It replaces the global opts.MaxFixRounds / opts.Th bounds
// at the consumption points (tripped, the fix-until-green loop, the critic/QA
// gates).
type Budget struct {
	FixRounds int // pre-review fix-until-green self-repair rounds (0 = self-repair disabled)
	MaxRework int // changes-requested rounds before escalation
	MaxFails  int // consecutive agent failures before escalation
	// Critic reports whether the tier PERMITS the optional pre-review critic
	// stint. The stint still only runs when a critic seat is configured
	// (--critic or `pactify config critic`); L0 forbids it outright unless the
	// operator passed --critic explicitly.
	Critic bool
	// QA reports whether the tier PERMITS the optional `qa:`-hint QA stint. The
	// stint still only runs when the task spec carries a `qa:` line; L0 skips
	// it even then.
	QA bool
}

// BudgetExplicit records which budget knobs the operator set EXPLICITLY (the
// CLI fills it from cmd.Flags().Changed — Go cannot distinguish a flag's
// default from the user typing that same value, and 0 is a legal explicit
// value: MaxFixRounds=0 disables self-repair). An explicit knob wins over the
// tier-derived value (spec §5 resolution rule 1). The zero value means
// "nothing explicit" — tests and direct callers keep today's behavior.
type BudgetExplicit struct {
	FixRounds bool
	MaxRework bool
	MaxFails  bool
	Critic    bool
}

// budgetFor resolves this task's effective budget from its tier, with explicit
// operator flags taking precedence. Pure function — no IO.
//
// L1 reproduces today's defaults byte-for-byte: the initialized value is simply
// the Options' current global settings (MaxFixRounds=2, MaxRework=3, MaxFails=2
// under withDefaults; critic permitted — it runs only when configured, exactly
// like today; QA per the task's `qa:` line).
func (opts Options) budgetFor(t Tier) Budget {
	fixRounds := opts.MaxFixRounds
	if fixRounds == 0 && !opts.ExplicitBudget.FixRounds {
		fixRounds = 2 // the withDefaults default, applied here too so direct callers agree
	}
	b := Budget{
		FixRounds: fixRounds,
		MaxRework: opts.Th.MaxRework,
		MaxFails:  opts.Th.MaxFails,
		Critic:    true, // permitted; runs only when a seat is configured (as today)
		QA:        true, // permitted; runs only when the spec carries a `qa:` line (as today)
	}
	switch t {
	case TierL0:
		b.FixRounds, b.MaxRework, b.MaxFails = 1, 2, 2
		b.Critic, b.QA = false, false
	case TierL2:
		b.FixRounds, b.MaxRework, b.MaxFails = 2, 3, 2
	case TierL3:
		b.FixRounds, b.MaxRework, b.MaxFails = 3, 4, 3
	}
	// Explicit operator flags win over the tier-derived value (spec §5 rule 1).
	if opts.ExplicitBudget.FixRounds {
		b.FixRounds = opts.MaxFixRounds // an explicit 0 stands: self-repair disabled
	}
	if opts.ExplicitBudget.MaxRework {
		b.MaxRework = opts.Th.MaxRework
	}
	if opts.ExplicitBudget.MaxFails {
		b.MaxFails = opts.Th.MaxFails
	}
	if opts.ExplicitBudget.Critic {
		b.Critic = opts.Critic != ""
	}
	return b
}

// budgetForTask resolves the effective budget for a task: the spec's `tier:`
// line (L1 when absent/unreadable) through budgetFor.
func (opts Options) budgetForTask(task projection.Task) Budget {
	return opts.budgetFor(extractTier(readSpec(opts.Dir, task.Spec)))
}

// thresholdsFor returns opts.Th with the per-task rework/fail bounds resolved
// from the task's tier budget (spec §5). MaxIters is a global backstop, not a
// budget item, so it always passes through unchanged. An unfindable task keeps
// the global bounds (defensive; the loop only calls this for dispatchable
// actions, which always reference a real task).
func (opts Options) thresholdsFor(st projection.State, act Action) Thresholds {
	th := opts.Th
	if _, task, ok := find(st, act.Feature, act.Task); ok {
		b := opts.budgetForTask(task)
		th.MaxRework, th.MaxFails = b.MaxRework, b.MaxFails
	}
	return th
}
