package orchestrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	Dir      string                     // repo root
	Feature  string                     // limit the loop to this feature ("" = all features)
	Th       Thresholds                 // rework / fail / iteration bounds
	DryRun   bool                       // compute + print actions, never exec/merge/escalate
	Run      Runner                     // injected agent launcher (prod CmdRunner, test fake)
	Exec     cmdExec                    // injected gate command executor
	Notify   Notifier                   // injected escalation notifier
	Now      func() string              // injected timestamp for escalation filenames
	SeatKind func(seatID string) string // seat id → agent kind (prod parses roster, test injects)
	// Orchestrator is the seat the driver acts as for its OWN protocol writes
	// (Merge). "" falls back to PACT_AGENT_ID (tests rely on the env fallback; the
	// CLI resolves it from --as / PACT_AGENT_ID and fail-fasts when both are empty).
	Orchestrator string
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
func (opts Options) launchAgent(ctx context.Context, seatID, kind, brief, task string) error {
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
				return opts.escalate(act.Task, reason, evidenceFor(st, act.Task),
					"人工介入后 pactify orchestrate 续跑")
			}
		}
		if !opts.DryRun && opts.Th.MaxIters > 0 && h.Iters >= opts.Th.MaxIters {
			opts.emitEscalatedStatus(view, "", "iteration limit exceeded", h)
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
			if err := opts.runReviewer(ctx, st, &h, act); err != nil {
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
			return opts.escalate(act.Task, act.Reason, evidenceFor(st, act.Task),
				"人工介入后 pactify orchestrate 续跑")

		case ActIdle:
			// Theoretically unreachable (unfinished work, no action, no threshold
			// tripped). Treat defensively as Stuck so the driver never spins.
			opts.emitEscalatedStatus(view, act.Task, "driver idle with unfinished work (unexpected)", h)
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
func (opts Options) runReviewer(ctx context.Context, st projection.State, h *History, act Action) error {
	_, task, ok := find(st, act.Feature, act.Task)
	if !ok {
		return fmt.Errorf("orchestrate: task %s not found for RunReviewer", act.Task)
	}
	brief := reviewerBrief(opts.Dir, projection.Seat{ID: act.Seat}, task)

	if runErr := opts.launchAgent(ctx, task.Reviewer, opts.kind(task.Reviewer), brief, act.Task); runErr != nil {
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
		// Task is done: close the owner's & reviewer's sessions for it (opencode-
		// only today). Best-effort — a cleanup failure never blocks the loop.
		opts.cleanupTaskSessions(task)
	default:
		h.Fails[act.Task]++
	}
	return nil
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

	for _, cmd := range gateCommands(opts.Dir, *feat) {
		ok, detail := runGate(ctx, opts.Exec, opts.Dir, cmd)
		if !ok {
			opts.emitEscalatedStatus(view, act.Feature, "hard gate failed: "+detail, *h)
			return true, opts.escalate(act.Feature, "hard gate failed: "+detail,
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
func (opts Options) escalate(task, reason, evidence, suggestion string) error {
	// Now is injected for deterministic test filenames; fall back to wall-clock so
	// a caller that forgets to wire it doesn't panic at the worst moment (escalation).
	ts := time.Now().Format("20060102-150405")
	if opts.Now != nil {
		ts = opts.Now()
	}
	path, err := writeEscalation(opts.runtimeDir(), ts, task, reason, evidence, suggestion)
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
