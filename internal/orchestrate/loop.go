package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
	"github.com/agentjoey/pactify/internal/sessions"
)

// Run is the package entry point: it drives opts to completion (or pause). See
// Options.run for the loop semantics.
func Run(ctx context.Context, opts Options) error { return opts.withDefaults().run(ctx) }

// fallbackGate is the conservative full command run when a feature's tasks
// declare no machine-readable `verify:` command. It is intentionally broad so
// the hard gate never silently passes on an unverified feature (spec §4).
const fallbackGate = "go build ./... && go test ./..."

// Options carries everything the driver needs, with every external dependency
// injected so the loop is deterministically testable: the agent launcher
// (Run), the gate command executor (Exec), the human-escalation notifier
// (Notify), the timestamp source (Now), and the seat→kind mapping (SeatKind).
type Options struct {
	Dir     string        // repo root
	Feature string        // limit the loop to this feature ("" = all features)
	Th      Thresholds    // rework / fail / iteration bounds
	DryRun  bool          // compute + print actions, never exec/merge/escalate
	Run     Runner        // injected agent launcher (prod CmdRunner, test fake)
	Exec    cmdExec       // injected gate command executor
	Notify  Notifier      // injected escalation notifier
	Now     func() string // injected timestamp for escalation filenames
	// SeatKind is the seat→kind OVERRIDE, highest priority: prod passes only the
	// operator's explicit `--seat-kind` flags, tests inject a fixed map. A non-empty
	// result wins over the live roster. When it returns "" (or is nil) opts.kind
	// falls back to re-reading the live roster's Agents[].Kind each iteration, so a
	// seat that joins mid-run is drivable next iteration (spec §6 WS-K).
	SeatKind func(seatID string) string
	// Orchestrator is the seat the driver acts as for its OWN protocol writes
	// (Merge). "" falls back to PACT_AGENT_ID (tests rely on the env fallback; the
	// CLI resolves it from --as / PACT_AGENT_ID and fail-fasts when both are empty).
	Orchestrator string
	// Critic names the seat run as a read-only pre-review critic (spec §3 WS-H):
	// after a task's verify gate is green and before its reviewer, the critic scores
	// the diff vs the spec and the score is injected into the reviewer briefing. This
	// is the --critic flag override; "" falls back to the project's `config critic`
	// setting, and absent-everywhere leaves the feature OFF (byte-identical flow, no
	// extra stint). The score has NO gating power (I-2).
	Critic string
	// MaxFixRounds bounds the pre-review fix-until-green self-repair loop (spec §1
	// WS-F): after a worker checkpoints a task, the driver runs the task's verify
	// gate BEFORE launching the reviewer; on a RED gate it re-runs the SAME owner
	// with a "fix round" briefing up to this many times before falling through to
	// the existing changes/escalation path. Fix rounds are in-stint self-repair —
	// they do NOT count toward MaxFails/MaxRework. Default 2 (withDefaults); 0
	// means a RED gate escalates immediately (no self-repair).
	MaxFixRounds int
	// RunTimeout bounds a single agent run end-to-end (a hard backstop). On expiry
	// the agent subprocess is killed and the run counts as a soft failure (retry →
	// escalate). 0 = no timeout (tests use the fake runner which returns instantly).
	RunTimeout time.Duration
	// IdleTimeout kills an agent that produces NO output for this long — the
	// precise fix for a hung agent (vs the blunt total RunTimeout, which either
	// over-shoots a hang or under-shoots a legitimately slow task). An idle kill is
	// a soft failure (retry the worker). 0 = no idle watchdog. Plumbed into the
	// default CmdRunner; ignored when a custom Run is injected (tests).
	IdleTimeout time.Duration
	// SessionRun is the injected runner for agent session-management CLIs (close a
	// finished task's sessions). nil disables session cleanup entirely — the safe
	// default for tests (no CLI spawn). withDefaults wires the real exec runner only
	// when CleanupSessions is set, so cleanup never runs unless explicitly enabled.
	SessionRun sessions.Runner
	// CleanupSessions enables closing an agent's sessions once its task is accepted
	// (opencode-only today; see internal/sessions). Off → sessions are kept.
	CleanupSessions bool
	// RuntimeDir is the repo dir under which dashboard-observable runtime artifacts
	// (.pact/orchestrate/status.json, streams/, escalation records) are written.
	// "" = Dir. A sandbox run sets it to the user's MAIN dir while Dir is the
	// throwaway worktree, so serve keeps seeing live progress (spec
	// coordination-authority P0b). Git work (checkout/commit/merge) always uses Dir.
	RuntimeDir string
	// pactIgnoredMemo caches mirrorLedger's "does the runtime dir git-ignore .pact"
	// probe: the answer cannot change mid-run and mirrorLedger runs every loop
	// iteration, so resolving it once avoids spawning `git check-ignore` in steady
	// state. A pointer so every value copy of Options the loop passes around shares
	// the one resolution. RunSandbox wires it (the only caller with RuntimeDir ≠
	// Dir); nil probes live (direct mirrorLedger use in tests).
	pactIgnoredMemo *ignoredMemo
}

// projectBase returns the repo's integration base branch, or "" when it cannot be
// determined. A missing base is safe for non-scoped gates (they simply get an
// empty PACT_CHANGED_FILES) and triggers fail-closed behavior for `{files}` gates.
func (opts Options) projectBase() string {
	base, _ := pact.At(opts.Dir).BaseBranch()
	return base
}

// ignoredMemo is the once-per-run backing store for Options.pactIgnored.
type ignoredMemo struct {
	once    sync.Once
	ignored bool
}

// runtimeDir is the base for dashboard-observable runtime artifacts: RuntimeDir
// when set, else Dir (the in-place default).
func (opts Options) runtimeDir() string {
	if opts.RuntimeDir != "" {
		return opts.RuntimeDir
	}
	return opts.Dir
}

// mirrorLedger unions opts.Dir's ledger into the runtime dir mid-run, so a
// sandboxed run's board reflects live progress (worker checkpoints, reviewer
// accepts) instead of freezing on the seed snapshot until RunSandbox's final
// copy-back. It reads only the sandbox ledger and writes only the runtime dir —
// never touching the running task's tree/ledger — so it is a pure observation
// refresh (union-merge is idempotent, reprojection is pure).
//
// No-op in-place (runtimeDir == Dir: the ledger already lives where the board
// reads). Skipped when the runtime dir TRACKS .pact: writing it there would
// dirty the parked main tree and block teardown's branch restore — those repos
// keep the run-end copy-back only. Common case (.pact gitignored) mirrors live.
func (opts Options) mirrorLedger() {
	dst := opts.runtimeDir()
	if dst == opts.Dir {
		return
	}
	if !opts.pactIgnored(dst) {
		return
	}
	writeLedger(dst, readLedger(opts.Dir))
}

// pactIgnored reports whether dst git-ignores .pact, memoized per run when the
// memo is wired (RunSandbox) — the ignore state cannot change mid-run, so the
// first probe's answer stands for the whole loop.
func (opts Options) pactIgnored(dst string) bool {
	if m := opts.pactIgnoredMemo; m != nil {
		m.once.Do(func() { m.ignored = gitx.PathIgnored(dst, ".pact") })
		return m.ignored
	}
	return gitx.PathIgnored(dst, ".pact")
}

// launchAgent runs one agent under an optional per-run timeout. The timeout is a
// CHILD of ctx, so a per-run expiry cancels just that subprocess (soft failure),
// while a parent-ctx cancellation (Ctrl-C) propagates. Callers distinguish the
// two by checking the PARENT ctx.Err() after a non-nil return.
// errOrchestratorSeat marks a launch that can never succeed: the seat is the
// orchestrator itself (this driver process) with no --seat-kind override, so
// there is no agent to spawn — the stint must be done manually. Distinguished
// so secondary launch sites (fix rounds, QA stints, quorum reviewer sweeps)
// that don't pass through the main loop's orchestrator-as-actor guard can
// surface the REAL cause instead of burning bounded budgets on a
// deterministic, instant failure with a generic reason (dogfood P2's shape).
var errOrchestratorSeat = errors.New("seat is the orchestrator and has no agent kind — its stint must be done manually")

// errPausedForEscalation signals run() that an escalation was already written
// and the loop must STOP (paused for a human), from a site whose plain-nil
// return would otherwise let the loop iterate straight back into the same
// deterministic dead-end (e.g. a quorum sweep re-hitting an unlaunchable
// orchestrator reviewer every iteration until MaxIters drowns the accurate
// reason under "iteration limit exceeded" — caught by
// TestLoopQuorumOrchestratorReviewerEscalatesAccurately). run() maps it to a
// clean nil return, exactly like the escalate-and-return sites do inline.
var errPausedForEscalation = errors.New("paused for escalation")

func (opts Options) launchAgent(ctx context.Context, seatID, kind, brief, task string) error {
	// Centralized orchestrator-as-actor check: the primary loop guard catches
	// act.Seat before dispatch, but the owner is also launched from fix/QA
	// rounds inside the ActRunReviewer branch, and quorum sweeps launch each
	// reviewer individually — every site funnels through here.
	if seatID != "" && seatID == opts.Orchestrator && kind == "" {
		return fmt.Errorf("launch %s for task %s: %w", seatID, task, errOrchestratorSeat)
	}
	runCtx := ctx
	if opts.RunTimeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, opts.RunTimeout)
		defer cancel()
	}
	return opts.Run.Run(runCtx, LaunchContext{
		Seat: seatID, Kind: kind, Task: task, Project: projectID(opts.Dir),
		Briefing: brief, RepoDir: opts.Dir, StreamDir: opts.runtimeDir(),
	})
}

// projectID derives a stable project name from the repo dir (its base name) — the
// same fallback the audit hook uses when PACT_PROJECT is unset, so they agree.
func projectID(dir string) string { return filepath.Base(dir) }

// featureBranchIn returns the declared branch of the named feature ("" if none).
func featureBranchIn(st projection.State, feature string) string {
	for _, f := range st.Features {
		if f.ID == feature {
			return f.Branch
		}
	}
	return ""
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
	// Ignore .pact/orchestrate/ before any runtime file (stream logs, status.json,
	// escalation records) is written, so they never land in the user's git status
	// or an agent's `git add -A`. Routed through .git/info/exclude (local, never
	// committed) so the driver never writes a chore commit to base out from under a
	// concurrent writer (spec coordination-authority P0a). DryRun stays
	// side-effect-free, so skip it there.
	if !opts.DryRun {
		if err := ensureRuntimeExcludedLocal(opts.Dir); err != nil {
			return fmt.Errorf("orchestrate: exclude runtime files: %w", err)
		}
	}

	// Reconstruct threshold history so a driver restart (crash, session limit,
	// supervisor restart) cannot hand a persistently-failing task a fresh budget:
	// rework rounds are re-counted from the ledger, and the process-internal
	// failure counters are reloaded from the persisted history file.
	scope := historyScope(opts.Feature)
	h := History{Rework: seedRework(opts.Dir), Fails: map[string]int{}, LastFail: map[string]string{}}
	loadHistory(opts.runtimeDir(), scope, &h)

	// fixRounds counts the pre-review self-repair rounds already spent per task
	// (spec §1 WS-F). It is DELIBERATELY driver in-memory only — I-3 forbids a new
	// ledger event_type for it, and it is not a failure budget, so it is neither
	// persisted (unlike History.Fails) nor ledger-derived (unlike History.Rework).
	fixRounds := map[string]int{}

	// Startup sweep: drop ACP session records whose task went terminal while the
	// driver was down (spec §2 orphan cleanup). Best-effort, off the DryRun path.
	if !opts.DryRun {
		if st, err := pact.At(opts.Dir).StateProjection(); err == nil {
			opts.pruneOrphanSessions(st)
			// Fail loud, once, if a feature filter was requested but the ledger this
			// run actually sees has no such feature at all. Silently proceeding would
			// hit ActDone on the very first iteration (total=0, done=true) and report
			// success — exactly the misdiagnosed "sandbox can't see the feature"
			// dogfood incident (2026-07-05 P5): a RunSandbox worktree that was seeded
			// from a stale/wrong ledger (or a typo'd --feature) looked like a healthy
			// "nothing to do" instead of the setup bug it actually was. A feature that
			// legitimately doesn't exist yet (or a typo) gets the same clear error.
			if opts.Feature != "" && !hasFeature(st, opts.Feature) {
				return fmt.Errorf("orchestrate: feature %q not found in %s — check --feature, or that the ledger seeded into this run actually has it (e.g. a stale sandbox worktree)", opts.Feature, opts.Dir)
			}
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		st, err := pact.At(opts.Dir).StateProjection()
		if err != nil {
			return fmt.Errorf("orchestrate: read state: %w", err)
		}
		// Mid-run copy-back: mirror the (just-reprojected) ledger into the runtime
		// dir so a sandboxed run's board tracks live progress instead of freezing on
		// the seed snapshot until the run ends. No-op in-place; RunSandbox's final
		// copy-back stays authoritative (spec coordination-authority P0b).
		opts.mirrorLedger()
		view := opts.filtered(st)

		act := nextAction(view, h, opts.Th)

		// Orchestrator-as-actor guard, checked BEFORE the failure-threshold guard
		// and BEFORE any launch is attempted: the orchestrator seat (opts.
		// Orchestrator — the human/session running THIS `pactify orchestrate`
		// process, who does its own protocol writes like Merge/Start) is static
		// for the whole run, never resolved dynamically, and structurally cannot
		// be headlessly launched as a subprocess of itself. When it's ALSO a
		// task's owner/reviewer (a normal pattern — "claude" is orchestrator AND
		// reviewer) and has no explicit --seat-kind override, every launch
		// attempt fails deterministically and instantly on kind resolution, not
		// from a transient crash. Routing it through the normal Fails++/
		// tripped() retry cycle burns the whole failure budget in a handful of
		// near-instant iterations and escalates with a MISLEADING "failure limit
		// exceeded" whose attached evidence is often the task's own successful
		// checkpoint — exactly what happened in the 2026-07-05 dogfood (P2): the
		// worker's checkpoint had already landed, but the very next iteration
		// tried to headlessly launch reviewer seat "claude" (the orchestrator,
		// meant to accept by hand) and instantly exhausted MaxFails trying.
		// Requiring act.Seat == opts.Orchestrator (not just "any unresolved
		// kind") keeps this from firing on a dynamic seat that simply hasn't
		// joined YET and may still get a kind on a later iteration (spec §6
		// rp-dynamic-seats) — Orchestrator can't ever "join later", so there is
		// nothing to wait for. An explicit --seat-kind for the orchestrator
		// (a deliberate choice to also drive it headlessly) opts back out.
		if !opts.DryRun && (act.Kind == ActRunOwner || act.Kind == ActRunReviewer) &&
			act.Seat != "" && act.Seat == opts.Orchestrator && opts.kind(act.Seat) == "" {
			reason := fmt.Sprintf("seat %q is the orchestrator (this pactify orchestrate process) and has no --seat-kind override — it cannot launch itself headlessly; this task's %s stint must be done manually (e.g. `pactify accept`/`pactify checkpoint`/`pactify changes`)", act.Seat, actKindLabel(act.Kind))
			opts.emitEscalatedStatus(view, act.Task, reason, h)
			return opts.escalate(act.Feature, act.Task, reason, evidenceFor(st, act.Task),
				"人工完成该棒后 pactify orchestrate 续跑（该座席是 orchestrator，永远不会被无人值守拉起）")
		}

		// Threshold guard for the churn loop. nextAction only escalates from its
		// idle path (no available action), but a task stuck in a rework or
		// fail cycle always HAS an action (rerun owner/reviewer), so the idle path
		// is never reached. Enforce per-task bounds here, before dispatching the
		// action that would spin, so the offending task escalates instead.
		if !opts.DryRun && (act.Kind == ActRunOwner || act.Kind == ActRunReviewer) {
			if reason, tripped := tripped(act.Task, h, opts.Th); tripped {
				opts.emitEscalatedStatus(view, act.Task, reason, h)
				// The threshold has fired and the human is being notified: drop this
				// task's persisted failure budget so a post-fix rerun resumes instead
				// of re-tripping on the loaded count (rework is ledger-derived and
				// stands until a human accepts the task or raises the bound).
				delete(h.Fails, act.Task)
				delete(h.LastFail, act.Task)
				_ = writeHistory(opts.runtimeDir(), scope, h)
				return opts.escalate(act.Feature, act.Task, reason, evidenceFor(st, act.Task),
					"人工介入后 pactify orchestrate 续跑")
			}
		}
		if !opts.DryRun && opts.Th.MaxIters > 0 && h.Iters >= opts.Th.MaxIters {
			opts.emitEscalatedStatus(view, "", "iteration limit exceeded", h)
			return opts.escalate(opts.Feature, "", "iteration limit exceeded", "(global cap)",
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

		opts.emitLoopStatus(view, act, h)

		switch act.Kind {
		case ActDone:
			total, accepted := progress(view)
			s := Status{
				Feature:   act.Feature,
				Action:    "done",
				Phase:     "done",
				Done:      true,
				Total:     total,
				Accepted:  accepted,
				Iter:      h.Iters,
				UpdatedAt: statusNow(opts.Now),
			}
			writeStatus(opts.runtimeDir(), s)
			// A completed run's failure history must not poison the next run.
			clearHistory(opts.runtimeDir(), scope)
			return nil

		case ActRunOwner:
			if err := opts.runOwner(ctx, st, &h, act); err != nil {
				return err
			}
			_ = writeHistory(opts.runtimeDir(), scope, h)

		case ActRunReviewer:
			// Fix-until-green self-repair (spec §1 WS-F): before handing a
			// checkpointed task to the reviewer, run its verify gate. GREEN → fall
			// through to the reviewer exactly as before. RED → re-run the SAME owner
			// with a fix briefing (bounded by MaxFixRounds; NOT a failure), rechecking
			// the gate each round. Rounds exhausted → escalate (proceed=false).
			proceed, err := opts.fixUntilGreen(ctx, st, view, act, &h, fixRounds)
			if err != nil {
				return err
			}
			if !proceed {
				return nil // rounds exhausted → escalated: paused, not failed
			}
			// QA-agent gate (spec §4 WS-I, experimental, task-level opt-in): with the
			// verify gate green and BEFORE the critic (order: 先 QA 后 critic), if the
			// task declares a `qa:` hint, run the software to verify it. A QA FAIL feeds
			// the SAME WS-F fix loop, sharing fixRounds/MaxFixRounds. No `qa:` line → ""
			// and zero extra stints (byte-identical flow).
			qaNote, proceed, err := opts.runQA(ctx, st, view, act, &h, fixRounds)
			if err != nil {
				return err
			}
			if !proceed {
				return nil // QA fix rounds exhausted → escalated: paused, not failed
			}
			// Critic pre-review score (spec §3 WS-H): with the gate green and before
			// the reviewer, if a critic seat is configured, run it read-only, record
			// its score as a task note, and inject the score into the reviewer's
			// briefing. No gating power (I-2); no critic configured → "" and the
			// reviewer launch is byte-for-byte unchanged.
			criticNote := opts.runCritic(ctx, st, act)
			// Both pre-review injections (QA report path + critic score) share the
			// reviewer briefing's single pre-review section; all-empty leaves it
			// byte-for-byte unchanged.
			if err := opts.runReviewer(ctx, st, &h, act, joinNotes(qaNote, criticNote)); err != nil {
				if errors.Is(err, errPausedForEscalation) {
					return nil // escalation written + notified: paused, not failed
				}
				return err
			}
			_ = writeHistory(opts.runtimeDir(), scope, h)

		case ActMerge:
			done, err := opts.merge(ctx, st, view, act, &h)
			if err != nil {
				return err
			}
			if done {
				// Escalated (gate failed): paused, not failed.
				return nil
			}

		case ActStuck:
			opts.emitEscalatedStatus(view, act.Task, act.Reason, h)
			return opts.escalate(act.Feature, act.Task, act.Reason, evidenceFor(st, act.Task),
				"人工介入后 pactify orchestrate 续跑")

		case ActIdle:
			// Theoretically unreachable (unfinished work, no action, no threshold
			// tripped). Treat defensively as Stuck so the driver never spins.
			opts.emitEscalatedStatus(view, act.Task, "driver idle with unfinished work (unexpected)", h)
			return opts.escalate(act.Feature, act.Task, "driver idle with unfinished work (unexpected)",
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
	// A prior soft failure (crash / timeout / idle-kill) on this task means the
	// worker may have left half-done work in the tree; tell it so it continues or
	// cleanly redoes rather than starting blind (error-handling design: retry the
	// worker, never hand off to the orchestrator).
	retrying := h.Fails[act.Task] > 0
	brief := workerBrief(opts.Dir, seatFor(st, act.Seat, seat), task, reason, retrying)

	// Check out THIS task's feature branch before launching the worker, so a seat
	// that owns tasks in several features commits to the right branch — not whichever
	// branch the worker's `pactify join` happens to pick first. Parallel runs already
	// start in the feature's worktree, so this is a no-op there.
	if br := featureBranchIn(st, act.Feature); br != "" {
		if err := gitx.CheckoutOrCreate(opts.Dir, br); err != nil {
			return fmt.Errorf("orchestrate: checkout feature branch %q for task %s: %w", br, act.Task, err)
		}
	}

	// Record the task-scoped start (assigned → in_progress on the board) as the
	// driver's own protocol write, like Merge. Guarded on `assigned` so a retry
	// (already in_progress) doesn't re-emit. Best-effort: the launch must never
	// be blocked by a bookkeeping write. Mirror immediately — the loop-top mirror
	// won't run again until the (long) agent launch below returns, which is
	// exactly the window the start exists to make visible.
	if task.Status == "assigned" {
		_ = pact.At(opts.Dir).As(opts.Orchestrator).Start(act.Task)
		opts.mirrorLedger()
	}

	if runErr := opts.launchAgent(ctx, task.Owner, opts.kind(task.Owner), brief, act.Task); runErr != nil {
		if ctx.Err() != nil {
			return runErr // cancellation: propagate, don't count as a task failure
		}
		// A transient agent crash (OOM / timeout / non-zero exit) is a soft failure,
		// not a driver-killer (spec §2.5). Before counting it, classify: if the
		// task's verify command now passes, the work is actually done and only the
		// checkpoint was missing — record it (as owner) and let the next iteration
		// route to the reviewer, instead of re-burning the worker.
		if opts.classifyAndCheckpoint(ctx, act.Task, task) {
			if after, err := pact.At(opts.Dir).StateProjection(); err == nil {
				if _, t, ok := find(after, act.Feature, act.Task); ok && t.Status == "awaiting_review" {
					h.Fails[act.Task] = 0
					return nil
				}
			}
		}
		h.LastFail[act.Task] = "agent run failed (crash, timeout, or non-zero exit)"
		h.Fails[act.Task]++
		return nil
	}

	after, err := pact.At(opts.Dir).StateProjection()
	if err != nil {
		return fmt.Errorf("orchestrate: reproject after owner: %w", err)
	}
	if _, t, ok := find(after, act.Feature, act.Task); !ok || t.Status != "awaiting_review" {
		// The worker exited cleanly but the task never reached awaiting_review: it
		// reported done yet recorded no checkpoint (its commit never landed on the
		// branch / never reached the driver's ledger — the opencode delivery class).
		// Same rescue as the crash path above: if the task's verify command passes,
		// the work IS done and only the delivery was lost — checkpoint on the
		// worker's behalf instead of burning failures toward escalation.
		if opts.classifyAndCheckpoint(ctx, act.Task, task) {
			if rescued, err := pact.At(opts.Dir).StateProjection(); err == nil {
				if _, t, ok := find(rescued, act.Feature, act.Task); ok && t.Status == "awaiting_review" {
					h.Fails[act.Task] = 0
					return nil
				}
			}
		}
		// Name it so the escalation isn't a bare "failure limit".
		h.LastFail[act.Task] = "worker finished but recorded no checkpoint — no commit landed on the feature branch (delivery did not reach the driver's ledger)"
		h.Fails[act.Task]++
	} else {
		h.Fails[act.Task] = 0 // consecutive: a successful checkpoint clears the run
	}
	return nil
}

// runReviewer launches the task reviewer with a reviewer briefing, then
// reprojects: changes_requested bumps the rework count; neither accepted nor
// changes_requested bumps the failure count.
func (opts Options) runReviewer(ctx context.Context, st projection.State, h *History, act Action, criticNote string) error {
	_, task, ok := find(st, act.Feature, act.Task)
	if !ok {
		return fmt.Errorf("orchestrate: task %s not found for RunReviewer", act.Task)
	}

	if runErr := opts.launchReviewers(ctx, act, task, criticNote); runErr != nil {
		if ctx.Err() != nil {
			return runErr // cancellation: propagate, don't count as a task failure
		}
		// An orchestrator-seat reviewer (e.g. a quorum member that is this very
		// driver process) can NEVER be launched — deterministic, not transient.
		// Counting it as a soft failure would burn the whole budget in instant
		// iterations and escalate with a misleading generic reason (P2's shape,
		// via the quorum sweep the main-loop guard doesn't see). Escalate now,
		// accurately, without touching the failure budget.
		if errors.Is(runErr, errOrchestratorSeat) {
			reason := runErr.Error() + " (e.g. `pactify accept`/`pactify changes`)"
			opts.emitEscalatedStatus(st, act.Task, reason, *h)
			if err := opts.escalate(act.Feature, act.Task, reason, evidenceFor(st, act.Task),
				"人工完成该评审后 pactify orchestrate 续跑"); err != nil {
				return err
			}
			// A plain nil here would let the loop iterate straight back into this
			// same deterministic dead-end; signal run() to stop (paused).
			return errPausedForEscalation
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
		// Task is done: close the owner's & reviewer's sessions for it (opencode-
		// only today). Best-effort — a cleanup failure never blocks the loop.
		opts.cleanupTaskSessions(task)
		// Drop the persisted ACP session records for this task so its resume state
		// doesn't outlive it (spec §2 cleanup). Best-effort; keyed by (seat,task).
		opts.clearTaskSessionRecords(task)
	default:
		h.Fails[act.Task]++
	}
	return nil
}

// launchReviewers runs the reviewer stint(s) for an awaiting_review task. A legacy
// single-reviewer task runs its one reviewer exactly as before (byte-identical:
// same seat, same briefing). A quorum task (opt-in `reviewers[]`) runs its
// reviewers SERIALLY, re-reading live state each iteration to skip any reviewer who
// already accepted in the current round and to STOP EARLY the moment the task
// leaves awaiting_review — quorum reached (accepted) or any reviewer requested
// changes (round reset). A met quorum therefore never burns the remaining
// reviewers' stints. The post-sweep classification (rework/accept/soft-fail) is the
// caller's, shared with the single-reviewer path.
func (opts Options) launchReviewers(ctx context.Context, act Action, task projection.Task, criticNote string) error {
	if len(task.Reviewers) == 0 {
		// Legacy single-reviewer: unchanged. act.Seat == task.Reviewer (nextAction).
		brief := reviewerBrief(opts.Dir, projection.Seat{ID: act.Seat}, task, criticNote)
		return opts.launchAgent(ctx, task.Reviewer, opts.kind(task.Reviewer), brief, act.Task)
	}
	for _, reviewer := range task.Reviewers {
		// Re-read live state: an earlier reviewer's accept/changes in THIS sweep may
		// have already met quorum or reset the round.
		cur, err := pact.At(opts.Dir).StateProjection()
		if err != nil {
			return err
		}
		_, t, ok := find(cur, act.Feature, act.Task)
		if !ok {
			return nil
		}
		if t.Status != "awaiting_review" {
			return nil // quorum met (accepted) or changes requested → stop early
		}
		if containsSeat(t.Accepts, reviewer) {
			continue // already voted this round → skip
		}
		brief := reviewerBrief(opts.Dir, projection.Seat{ID: reviewer}, t, criticNote)
		if err := opts.launchAgent(ctx, reviewer, opts.kind(reviewer), brief, act.Task); err != nil {
			return err
		}
		// Mirror the fresh verdict into the runtime dir so a sandboxed board reflects
		// each reviewer's vote mid-sweep instead of freezing until the next loop top.
		opts.mirrorLedger()
	}
	return nil
}

// containsSeat reports whether seat is in xs (the current round's accepts tally).
func containsSeat(xs []string, seat string) bool {
	for _, x := range xs {
		if x == seat {
			return true
		}
	}
	return false
}

// fixUntilGreen is the pre-review fix-until-green self-repair loop (spec §1
// WS-F). It is called for an awaiting_review task BEFORE its reviewer launches:
//
//   - It runs the task's verify gate (its own `verify:` line, else the config
//     gate — the same "task verify: > config gate" resolution the merge gate uses).
//   - GREEN → returns proceed=true and performs NO state writes, so the caller's
//     reviewer launch is byte-for-byte identical to the pre-WS-F flow (the only
//     added effect on a green first try is the gate exec itself).
//   - RED → re-runs the SAME owner with a fix briefing (tail of the verify output
//   - "you already checkpointed; fix until the gate is green, then checkpoint
//     again"), rechecking the gate after each round, bounded by opts.MaxFixRounds.
//     Fix rounds are in-stint self-repair: they NEVER touch h.Fails/h.Rework, so
//     they do not count toward MaxFails/MaxRework.
//   - Rounds exhausted → escalates with the last verify output as the reason and
//     returns proceed=false so the loop pauses (the existing escalation path).
func (opts Options) fixUntilGreen(ctx context.Context, st, view projection.State, act Action, h *History, fixRounds map[string]int) (proceed bool, err error) {
	_, task, ok := find(st, act.Feature, act.Task)
	if !ok {
		// No such task (should not happen for an awaiting_review action): defer the
		// lookup error to runReviewer so the handling stays in one place.
		return true, nil
	}
	cmd := opts.taskGateCommand(task)
	base := opts.projectBase()
	for {
		passed, detail := runGateScoped(ctx, opts.Exec, opts.Dir, cmd, base)
		if passed {
			return true, nil
		}
		// RED. Out of fix rounds → escalate with the last verify output as reason.
		if fixRounds[act.Task] >= opts.MaxFixRounds {
			reason := fmt.Sprintf("fix-until-green exhausted after %d round(s); verify still failing:\n%s",
				fixRounds[act.Task], detail)
			opts.emitEscalatedStatus(view, act.Task, reason, *h)
			return false, opts.escalate(act.Feature, act.Task, reason, evidenceFor(st, act.Task),
				"人工修复 verify 失败后 pactify orchestrate 续跑")
		}
		// Re-run the SAME owner with a fix briefing. Count the round FIRST (driver
		// in-memory only — not a failure, not a ledger event) so a persistently
		// erroring fixer still terminates at MaxFixRounds.
		fixRounds[act.Task]++
		opts.emitFixingStatus(view, act, task.Owner, *h, fixRounds[act.Task])
		brief := fixBrief(task, detail)
		if runErr := opts.launchAgent(ctx, task.Owner, opts.kind(task.Owner), brief, act.Task); runErr != nil {
			if ctx.Err() != nil {
				return false, runErr // cancellation: propagate, don't swallow
			}
			// An orchestrator-seat OWNER can never be re-launched for a fix round —
			// deterministic, so burning the remaining rounds re-checking an
			// unchanged gate would escalate with only the gate output as reason,
			// hiding the real cause. Escalate now with the accurate one.
			if errors.Is(runErr, errOrchestratorSeat) {
				reason := runErr.Error() + " — the verify gate is red and its owner cannot be driven headlessly"
				opts.emitEscalatedStatus(view, act.Task, reason, *h)
				return false, opts.escalate(act.Feature, act.Task, reason, detail,
					"人工修复 verify 失败后 pactify orchestrate 续跑")
			}
			// A fix-round agent error is not a task failure (in-stint self-repair):
			// leave h.Fails untouched and let the gate recheck decide. The round is
			// already counted, so this cannot spin.
		}
		// Mirror the (possibly new) checkpoint into the runtime dir so a sandboxed
		// run's board reflects the fix mid-loop instead of freezing until the next
		// top-of-loop mirror.
		opts.mirrorLedger()
	}
}

// taskGateCommand resolves the verify command for a single task: its own
// `verify:` line when present, else the project config gate (resolveGate) — the
// same "task verify: > config gate" precedence gateCommands applies per feature.
func (opts Options) taskGateCommand(task projection.Task) string {
	if cmd, ok := extractVerify(readSpec(opts.Dir, task.Spec)); ok {
		return cmd
	}
	return resolveGate(opts.Dir)
}

// fixBrief renders the fix-round briefing for the pre-review self-repair loop: it
// tells the owner it already checkpointed, shows the tail (~2KB) of the failing
// verify output, and asks it to fix until the gate is green and checkpoint again.
func fixBrief(task projection.Task, verifyOutput string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pact fix round — task `%s`\n\n", task.ID)
	b.WriteString("你上一轮已经 `pactify checkpoint` 了这个 task,但驱动器在交给 reviewer 前先跑验收门(verify)——**门是红的**。\n")
	b.WriteString("这不是返工、也不算失败,是评审前的自愈轮:请直接修到门绿。\n\n")
	b.WriteString("## 修什么\n")
	fmt.Fprintf(&b, "- 读规格:`%s`。只碰该 spec 列出的文件。\n", task.Spec)
	b.WriteString("- 下面是失败的 verify 输出(尾部),按它定位并修复:\n\n")
	b.WriteString("```\n" + tailBytes(verifyOutput, 2048) + "\n```\n\n")
	fmt.Fprintf(&b, "- 修好后重新 `pactify checkpoint %s` 附上验收命令输出。修到门绿为止,不要自标 accepted。\n", task.ID)
	return b.String()
}

// tailBytes returns the last n bytes of s, trimmed forward to the next rune
// boundary so a multi-byte character is never split, prefixed with a truncation
// marker when it dropped anything.
func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	for i := 0; i < len(cut) && i < utf8.UTFMax; i++ {
		if utf8.RuneStart(cut[i]) {
			cut = cut[i:]
			break
		}
	}
	return "...(truncated)...\n" + cut
}

// cleanupTaskSessions closes the sessions an accepted task's agents created,
// matched by the per-seat title the runner stamped (sessions.SessionTag). No-op
// when cleanup is disabled (SessionRun nil) or the seat's kind has no verified
// list+delete support. Reports what it closed via Notify; failures are logged,
// never fatal.
func (opts Options) cleanupTaskSessions(task projection.Task) {
	if opts.SessionRun == nil {
		return
	}
	seen := map[string]bool{}
	for _, seat := range []string{task.Owner, task.Reviewer} {
		if seat == "" || seen[seat] {
			continue
		}
		seen[seat] = true
		kind := opts.kind(seat)

		// kimi has no headless list/delete CLI, so its sessions are cleaned at the
		// filesystem level, matched by the seat marker the briefing leaves in each
		// session's title (see sessions.CleanupKimiSeat).
		if sessions.IsKimi(kind) {
			ids, err := sessions.CleanupKimiSeat(sessions.KimiHome(), seat)
			opts.notifyCleanup(task, seat, kind, ids, err)
			continue
		}

		if !sessions.CanCleanup(kind) {
			continue
		}
		// Run the session CLI in opts.Dir — the worktree (parallel) or repo (serial)
		// the agent worked in. opencode scopes its session store to the cwd, so a
		// cleanup in the wrong dir lists the wrong project and deletes nothing.
		mgr := sessions.Manager{Run: opts.SessionRun, Dir: opts.Dir}
		ids, _, err := mgr.CleanupByTitle(kind, sessions.SessionTag(seat))
		opts.notifyCleanup(task, seat, kind, ids, err)
	}
}

// clearTaskSessionRecords drops the persisted ACP session store rows for a task's
// owner and reviewer once the task is terminal, so a resume never re-attaches a
// dead task's context. Best-effort: keyed by (seat,task); a store write failure is
// swallowed (resume degrades to a cold session — never fatal).
func (opts Options) clearTaskSessionRecords(task projection.Task) {
	for _, seat := range []string{task.Owner, task.Reviewer} {
		if seat == "" {
			continue
		}
		_ = ClearSession(opts.Dir, seat, task.ID)
	}
}

// pruneOrphanSessions sweeps the ACP session store on startup, dropping records
// whose task already reached a terminal state (accepted/cancelled) while the
// driver was down — the crash-safe complement to clearTaskSessionRecords. Records
// for still-live or unknown tasks are kept (a resume of a live task is the point).
// Best-effort: a read/write failure is swallowed so startup is never blocked.
func (opts Options) pruneOrphanSessions(st projection.State) {
	terminal := map[string]bool{}
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if t.Status == "accepted" || t.Status == "cancelled" {
				terminal[t.ID] = true
			}
		}
	}
	_ = PruneSessions(opts.Dir, func(_ /*seat*/, task string) bool {
		return !terminal[task]
	})
}

// notifyCleanup reports the outcome of one seat's session cleanup: an error, or
// the count closed (silent when nothing matched). Shared by the CLI-based
// (opencode) and file-based (kimi) cleanup paths.
func (opts Options) notifyCleanup(task projection.Task, seat, kind string, ids []string, err error) {
	if opts.Notify == nil {
		return
	}
	switch {
	case err != nil:
		opts.Notify.Notify(fmt.Sprintf("session cleanup: seat %s (%s): %v", seat, kind, err))
	case len(ids) > 0:
		opts.Notify.Notify(fmt.Sprintf("closed %d %s session(s) for seat %s after task %s accepted", len(ids), kind, seat, task.ID))
	}
}

// merge runs the independent hard gate over the feature's tasks, then merges on
// PASS. A gate FAIL escalates (returns done=true so the loop pauses). The hard
// gate is deliberately redundant with the LLM reviewer's own run: a deterministic
// safety net beneath "LLM accepted" (spec §2.4).
func (opts Options) merge(ctx context.Context, st, view projection.State, act Action, h *History) (done bool, err error) {
	var feat *projection.Feature
	for fi := range st.Features {
		if st.Features[fi].ID == act.Feature {
			feat = &st.Features[fi]
		}
	}
	if feat == nil {
		return false, fmt.Errorf("orchestrate: feature %s not found for Merge", act.Feature)
	}

	base := opts.projectBase()
	for _, cmd := range gateCommands(opts.Dir, *feat) {
		ok, detail := runGateScoped(ctx, opts.Exec, opts.Dir, cmd, base)
		if !ok {
			opts.emitEscalatedStatus(view, act.Feature, "hard gate failed: "+detail, *h)
			return true, opts.escalate(act.Feature, "", "hard gate failed: "+detail,
				evidenceFor(st, ""), "修复实现/规格后 pactify orchestrate 续跑")
		}
	}

	// Merge is the driver's OWN protocol write (unlike worker/reviewer verbs, which
	// the spawned agents run). It needs an acting seat: As(Orchestrator) sets it
	// explicitly so the merge doesn't depend on the driver process's PACT_AGENT_ID
	// (which the worker runner overrides per-spawn). As("") falls back to the env
	// (the CLI fail-fasts when neither --as nor PACT_AGENT_ID is set).
	if err := pact.At(opts.Dir).As(opts.Orchestrator).Merge(act.Feature); err != nil {
		return false, fmt.Errorf("orchestrate: merge %s: %w", act.Feature, err)
	}
	// The feature just shipped: any of its OWN escalation files are resolved and
	// no longer actionable — archive them out of the live directory so it only
	// ever shows escalations for features still in progress (spec P1: a stale,
	// unrelated escalation sitting next to a live one is how one got mistaken
	// for the current run's state during the 2026-07-05 dogfood).
	archiveEscalationsForFeature(opts.runtimeDir(), act.Feature)
	return false, nil
}

// gateCommands collects the deduplicated set of verify commands for a feature's
// tasks. A task whose spec declares no `verify:` line contributes the
// conservative full fallback command instead, so the feature is never merged
// unverified.
func gateCommands(dir string, f projection.Feature) []string {
	// The fallback for a task with no `verify:` line is the project gate: an explicit
	// `pactify config gate` if set, else a default inferred from the project type
	// (pnpm/npm/cargo/go) — see resolveGate. Resolved once per feature.
	gate := resolveGate(dir)
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
			add(gate)
		}
	}
	if len(cmds) == 0 {
		cmds = append(cmds, gate)
	}
	return cmds
}

// escalate writes the escalation record and notifies. It returns nil even on a
// write/notify failure path's own error only when the record cannot be written,
// because escalation is a pause, not a hard stop — but a write error is a real
// IO failure worth surfacing.
func (opts Options) escalate(feature, task, reason, evidence, suggestion string) error {
	// Now is injected for deterministic test filenames; fall back to wall-clock so
	// a caller that forgets to wire it doesn't panic at the worst moment (escalation).
	ts := time.Now().Format("20060102-150405")
	if opts.Now != nil {
		ts = opts.Now()
	}
	path, err := writeEscalation(opts.runtimeDir(), ts, feature, task, reason, evidence, suggestion)
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
// actKindLabel names an action kind for a human-facing escalation message.
func actKindLabel(k ActionKind) string {
	if k == ActRunReviewer {
		return "reviewer"
	}
	return "worker"
}

// hasFeature reports whether id is present anywhere in st.Features.
func hasFeature(st projection.State, id string) bool {
	for _, f := range st.Features {
		if f.ID == id {
			return true
		}
	}
	return false
}

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

// kind resolves a seat id to its agent kind at the moment of launch, so a seat
// that joined mid-run (or re-declared its kind via `pactify join --kind`) is
// drivable on the NEXT iteration without restarting the driver (spec §6 WS-K).
// Priority (highest first):
//  1. the injected SeatKind override — the operator's explicit `--seat-kind` flags
//     (prod) or a test's fixed map. A non-empty override always wins.
//  2. the LIVE roster's Agents[].Kind, re-read from opts.Dir's ledger on every
//     call. This is what makes a mid-run join take effect: the startup km no longer
//     freezes the mapping.
//
// Empty when neither resolves (the Runner then fails closed on an unknown kind).
func (opts Options) kind(seatID string) string {
	if opts.SeatKind != nil {
		if k := opts.SeatKind(seatID); k != "" {
			return k
		}
	}
	if st, err := pact.At(opts.Dir).StateProjection(); err == nil {
		for _, a := range st.Agents {
			if a.ID == seatID && a.Kind != "" {
				return a.Kind
			}
		}
	}
	return ""
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
		reason := "failure limit exceeded"
		if c := h.LastFail[task]; c != "" {
			reason += ": " + c
		}
		return reason, true
	}
	return "", false
}

// --- status emission ----------------------------------------------------------

// emitLoopStatus writes a per-iteration status snapshot for the current action.
// Write errors are silently ignored (status is observation, not a transaction source).
func (opts Options) emitLoopStatus(view projection.State, act Action, h History) {
	writeStatus(opts.runtimeDir(), buildLoopStatus(view, act, h, func() string { return statusNow(opts.Now) }))
}

// emitEscalatedStatus writes an escalated status snapshot.
// Write errors are silently ignored (status is observation, not a transaction source).
func (opts Options) emitEscalatedStatus(view projection.State, task, reason string, h History) {
	writeStatus(opts.runtimeDir(), buildEscalatedStatus(view, task, reason, h, func() string { return statusNow(opts.Now) }))
}

// emitFixingStatus writes a `fixing` snapshot so the board shows "fixing n/max"
// while the pre-review self-repair loop re-runs the owner (spec §1 WS-F).
// Write errors are silently ignored (status is observation, not a transaction source).
func (opts Options) emitFixingStatus(view projection.State, act Action, owner string, h History, round int) {
	writeStatus(opts.runtimeDir(), buildFixingStatus(view, act, owner, h, round, opts.MaxFixRounds, func() string { return statusNow(opts.Now) }))
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

// seedRework reconstructs per-task rework counts from the ledger's
// changes_requested events, so MaxRework bounds the task's total review rounds
// across driver restarts (History is otherwise zeroed at loop start and a
// restart would grant a churning task a fresh budget forever). An unreadable
// log seeds nothing — a clean start, matching the old behavior.
func seedRework(dir string) map[string]int {
	rework := map[string]int{}
	evs, err := event.ReadAll(paths.LogIn(dir))
	if err != nil {
		return rework
	}
	for _, e := range evs {
		if e.EventType == "changes_requested" && e.TaskID != "" {
			rework[e.TaskID]++
		}
	}
	return rework
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
