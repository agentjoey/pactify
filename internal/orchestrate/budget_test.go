package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// --- budgetFor: pure tier → budget resolution (spec execution-tiering §5) ----

// L1 is the hard backward-compat requirement: every field must equal today's
// defaults byte-for-byte (MaxFixRounds=2, MaxRework=3, MaxFails=2; critic
// permitted — it runs only when a seat is configured, exactly like today; QA
// per the task's `qa:` line). Pin it so a future edit cannot drift L1.
func TestBudgetForL1EqualsTodayDefaults(t *testing.T) {
	// The withDefaults-applied shape (what the loop actually consumes).
	opts := Options{Th: Thresholds{MaxRework: 3, MaxFails: 2, MaxIters: 50}}.withDefaults()
	b := opts.budgetFor(TierL1)
	want := Budget{FixRounds: 2, MaxRework: 3, MaxFails: 2, Critic: true, QA: true}
	if b != want {
		t.Fatalf("budgetFor(L1) = %+v, want today's defaults %+v", b, want)
	}
	// A bare Options (no withDefaults) must agree — budgetFor is self-contained.
	bare := Options{Th: Thresholds{MaxRework: 3, MaxFails: 2, MaxIters: 50}}.budgetFor(TierL1)
	if bare != want {
		t.Fatalf("budgetFor(L1) without withDefaults = %+v, want %+v", bare, want)
	}
}

// The tier table (spec §5): L0 trims the optional stints and the fix budget;
// L2/L3 raise the churn bounds. Critic stays "permitted" for L1+ (the stint
// runs only when a seat is configured); QA stays per-`qa:`-line for L1+.
func TestBudgetForTierTable(t *testing.T) {
	opts := Options{Th: Thresholds{MaxRework: 3, MaxFails: 2, MaxIters: 50}}.withDefaults()
	cases := []struct {
		tier Tier
		want Budget
	}{
		{TierL0, Budget{FixRounds: 1, MaxRework: 2, MaxFails: 2, Critic: false, QA: false}},
		{TierL1, Budget{FixRounds: 2, MaxRework: 3, MaxFails: 2, Critic: true, QA: true}},
		{TierL2, Budget{FixRounds: 2, MaxRework: 3, MaxFails: 2, Critic: true, QA: true}},
		{TierL3, Budget{FixRounds: 3, MaxRework: 4, MaxFails: 3, Critic: true, QA: true}},
	}
	for _, c := range cases {
		if got := opts.budgetFor(c.tier); got != c.want {
			t.Fatalf("budgetFor(%s) = %+v, want %+v", c.tier, got, c.want)
		}
	}
}

// Resolution rule 1: an explicit operator flag wins over the tier-derived value.
func TestBudgetExplicitFlagsOverrideTier(t *testing.T) {
	base := Options{Th: Thresholds{MaxRework: 3, MaxFails: 2, MaxIters: 50}}.withDefaults()

	fix := base
	fix.MaxFixRounds = 5
	fix.ExplicitBudget.FixRounds = true
	if got := fix.budgetFor(TierL0).FixRounds; got != 5 {
		t.Fatalf("explicit --max-fix-rounds=5 + L0 = %d, want 5 (not the tier's 1)", got)
	}

	rework := base
	rework.Th.MaxRework = 9
	rework.ExplicitBudget.MaxRework = true
	if got := rework.budgetFor(TierL3).MaxRework; got != 9 {
		t.Fatalf("explicit --max-rework=9 + L3 = %d, want 9 (not the tier's 4)", got)
	}

	fails := base
	fails.Th.MaxFails = 7
	fails.ExplicitBudget.MaxFails = true
	if got := fails.budgetFor(TierL0).MaxFails; got != 7 {
		t.Fatalf("explicit --max-fails=7 + L0 = %d, want 7 (not the tier's 2)", got)
	}
}

// An explicit 0 is legal semantics (self-repair disabled), not "unset": it must
// survive both withDefaults defaulting and the tier-derived value.
func TestBudgetExplicitZeroFixRoundsRespected(t *testing.T) {
	opts := Options{
		Th:             Thresholds{MaxRework: 3, MaxFails: 2, MaxIters: 50},
		MaxFixRounds:   0,
		ExplicitBudget: BudgetExplicit{FixRounds: true},
	}.withDefaults()
	if opts.MaxFixRounds != 0 {
		t.Fatalf("withDefaults bumped an explicit --max-fix-rounds=0 to %d", opts.MaxFixRounds)
	}
	if got := opts.budgetFor(TierL3).FixRounds; got != 0 {
		t.Fatalf("explicit 0 + L3 = %d, want 0 (tier must not top it up)", got)
	}
	if got := opts.budgetFor(TierL0).FixRounds; got != 0 {
		t.Fatalf("explicit 0 + L0 = %d, want 0", got)
	}
}

// An explicit --critic flag re-enables the optional critic stint even on L0;
// without it L0 forbids the critic outright (config alone does not re-enable).
func TestBudgetExplicitCriticFlagOverridesL0(t *testing.T) {
	base := Options{Th: Thresholds{MaxRework: 3, MaxFails: 2}}.withDefaults()
	if got := base.budgetFor(TierL0).Critic; got {
		t.Fatal("L0 without an explicit --critic must forbid the critic stint")
	}
	explicit := base
	explicit.Critic = "crit"
	explicit.ExplicitBudget.Critic = true
	if got := explicit.budgetFor(TierL0).Critic; !got {
		t.Fatal("explicit --critic must win over the L0 tier gate")
	}
}

// thresholdsFor resolves the per-task rework/fail bounds from the task's tier
// while leaving the global MaxIters backstop untouched.
func TestThresholdsForUsesTierBudget(t *testing.T) {
	dir := newProject(t)
	spec := writeSpecTier(t, dir, "t1", "go test ./...", "L3", "")
	assign(t, dir, "t1", "f", "feat/x", spec)

	opts := baseOpts(dir, newFakeRunner(t, dir), &okExec{}, &recNotify{})
	st := mustState(t, dir)
	th := opts.thresholdsFor(st, Action{Kind: ActRunOwner, Feature: "f", Task: "t1"})
	if th.MaxRework != 4 || th.MaxFails != 3 {
		t.Fatalf("L3 thresholds = {rework %d, fails %d}, want {4, 3}", th.MaxRework, th.MaxFails)
	}
	if th.MaxIters != 50 {
		t.Fatalf("MaxIters is a global backstop, not a budget item: got %d, want 50", th.MaxIters)
	}
	// An untiered (L1) task keeps the operator's global bounds byte-for-byte.
	spec2 := writeSpec(t, dir, "t2", "go test ./...")
	assign(t, dir, "t2", "f", "feat/x", spec2)
	th2 := opts.thresholdsFor(mustState(t, dir), Action{Kind: ActRunOwner, Feature: "f", Task: "t2"})
	if th2 != opts.Th {
		t.Fatalf("L1 thresholds = %+v, want the global %+v", th2, opts.Th)
	}
}

// --- driver-level: L0 skips the OPTIONAL stints, never the reviewer ---------

// writeSpecTier writes a task spec with verify/tier(/qa) lines under .pact/tasks/.
func writeSpecTier(t *testing.T, dir, taskID, verify, tier, qa string) string {
	t.Helper()
	rel := filepath.Join(".pact", "tasks", taskID+".md")
	body := "# " + taskID + "\n\nverify: " + verify + "\ntier: " + tier + "\n"
	if qa != "" {
		body += "qa: " + qa + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return rel
}

// tierRunner drives the pact engine for the tier-budget tests, counting every
// stint kind: worker → checkpoint; fix round → re-checkpoint; QA/critic →
// return success (no verdict marker → lenient paths); reviewer → accept.
type tierRunner struct {
	dir           string
	workerCalls   int
	fixCalls      int
	qaCalls       int
	criticCalls   int
	reviewerCalls int
}

func (r *tierRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	switch {
	case strings.Contains(lc.Briefing, "pact fix round"):
		r.fixCalls++
		return pact.At(r.dir).As(lc.Seat).Checkpoint(task, "evidence: fixed")
	case strings.Contains(lc.Briefing, "pact QA"):
		r.qaCalls++
		return nil
	case strings.Contains(lc.Briefing, "pact critic"):
		r.criticCalls++
		return nil
	case isWorker(lc.Briefing):
		r.workerCalls++
		return pact.At(r.dir).As(lc.Seat).Checkpoint(task, "evidence: tests pass")
	default:
		r.reviewerCalls++
		return pact.At(r.dir).As(lc.Seat).Accept(task)
	}
}

// An L0 task pays for NONE of the optional stints — even with a `qa:` line in
// its spec and a critic configured project-wide — but the reviewer stint ALWAYS
// runs: "a worker cannot accept its own work" is a protocol invariant, not a
// budget item (spec §5).
func TestTierL0SkipsCriticAndQAButRunsReviewer(t *testing.T) {
	dir := newProject(t)
	spec := writeSpecTier(t, dir, "t1", "go test ./...", "L0", "click through the settings page")
	assign(t, dir, "t1", "f", "feat/x", spec)
	if err := pact.At(dir).As("orch").ConfigCritic("crit"); err != nil {
		t.Fatalf("config critic: %v", err)
	}

	runner := &tierRunner{dir: dir}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	if runner.qaCalls != 0 {
		t.Fatalf("L0 ran %d QA stints, want 0 (tier gate)", runner.qaCalls)
	}
	if runner.criticCalls != 0 {
		t.Fatalf("L0 ran %d critic stints, want 0 (tier gate; config alone must not re-enable)", runner.criticCalls)
	}
	if runner.reviewerCalls != 1 {
		t.Fatalf("reviewer stints = %d, want exactly 1 — the reviewer is NEVER tier-gated", runner.reviewerCalls)
	}
}

// An explicit --critic flag beats the L0 tier gate (resolution rule 1): the
// critic stint runs even on an L0 task.
func TestTierL0ExplicitCriticFlagStillRuns(t *testing.T) {
	dir := newProject(t)
	spec := writeSpecTier(t, dir, "t1", "go test ./...", "L0", "")
	assign(t, dir, "t1", "f", "feat/x", spec)

	runner := &tierRunner{dir: dir}
	opts := baseOpts(dir, runner, &okExec{}, &recNotify{})
	opts.Critic = "crit"
	opts.ExplicitBudget.Critic = true // the CLI sets this from Flags().Changed("critic")
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.criticCalls != 1 {
		t.Fatalf("critic stints = %d, want 1 (explicit --critic overrides the L0 gate)", runner.criticCalls)
	}
	if runner.reviewerCalls != 1 {
		t.Fatalf("reviewer stints = %d, want 1", runner.reviewerCalls)
	}
}

// The L0 fix budget is exactly 1: a permanently-red gate gets ONE self-repair
// round, then escalates — and the reviewer never sees a red gate.
func TestTierL0FixBudgetIsOne(t *testing.T) {
	dir := newProject(t)
	spec := writeSpecTier(t, dir, "t1", "go test ./...", "L0", "")
	assign(t, dir, "t1", "f", "feat/x", spec)

	runner := &tierRunner{dir: dir}
	notify := &recNotify{}
	if err := Run(context.Background(), baseOpts(dir, runner, &failExec{}, notify)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.fixCalls != 1 {
		t.Fatalf("L0 fix rounds = %d, want exactly 1 (tier-derived budget)", runner.fixCalls)
	}
	if runner.reviewerCalls != 0 {
		t.Fatalf("reviewer ran on a permanently-red gate (reviewerCalls=%d, want 0)", runner.reviewerCalls)
	}
	if len(notify.msgs) == 0 {
		t.Fatal("expected an escalation after the single L0 fix round was exhausted")
	}
}
