package pact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/lockx"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

// baseIntegrationLockTimeout bounds how long a merge waits for the base lock
// before giving up — long enough to queue behind a real merge, short enough that a
// stale/crashed holder doesn't hang a run forever. A var so tests can shrink it.
var baseIntegrationLockTimeout = 3 * time.Minute

// ledgerWriteLockTimeout bounds how long a mutating verb waits for the ledger
// write lock. Verbs are short (read log → check → append), so a minute is ample
// queueing headroom while still surfacing a stale/crashed holder. A var so tests
// can shrink it.
var ledgerWriteLockTimeout = time.Minute

var errNoAgent = errors.New("pactify: PACT_AGENT_ID not set; source your entry file")

// ClientVersion is the version string the CLI stamps onto join events as the
// self-reported client provenance (client.name = "pactify-cli"). main() sets it
// from the build-time version var at startup; "dev" is the default for tests and
// un-stamped builds. It is advisory metadata only — never an identity proof.
var ClientVersion = "dev"

// agentID resolves the acting seat: the handle's actor override if set, else
// PACT_AGENT_ID from the environment. Fails closed when neither is present.
func (p *Project) agentID() (string, error) {
	if p.actor != "" {
		return p.actor, nil
	}
	id := paths.AgentID()
	if id == "" {
		return "", errNoAgent
	}
	return id, nil
}

// StateProjection returns the current projected state for the repo (exported
// wrapper around the internal projection used by the orchestrate driver and serve's
// cold start). It reads through the persistent ledger snapshot: on a valid snapshot
// it folds only the bytes appended since the snapshot was written, else it full-
// folds. The result is identical to a full replay either way (the snapshot is a
// pure cache); PACT_NO_SNAPSHOT=1 bypasses it.
func (p *Project) StateProjection() (projection.State, error) {
	return projection.LoadSnapshotState(paths.LogIn(p.dir), paths.StateSnapshotIn(p.dir))
}

func (p *Project) state() (projection.State, []event.Event, error) {
	evs, err := event.ReadAll(paths.LogIn(p.dir))
	if err != nil {
		return projection.State{}, nil, err
	}
	return projection.Project(evs), evs, nil
}

// withLedgerLock serializes a mutating verb's read→check→append critical section
// against every other pact process writing the same ledger (CLI, MCP host,
// orchestrate driver). Without it two concurrent verbs both read the same state,
// both pass their rule check, and both append — e.g. two Assigns for one task id
// both clear checkAssign's uniqueness check and the projection silently keeps the
// later definition (TOCTOU). The lock file lives in the git dir GitPath resolves
// for THIS working dir — the per-worktree gitdir in a linked worktree, .git in
// the main tree. That is the right scope: .pact ledgers are per-worktree, so
// every writer of a given ledger resolves the same lock file, while writers of
// different worktrees' ledgers (different files) correctly don't contend.
//
// LOCK ORDER: when a caller also holds the base-integration lock
// (pactify-base.lock) it must be acquired FIRST, then this log lock — Merge is
// the only verb that holds both today; any future dual-holder must keep that
// order or two processes can deadlock.
func (p *Project) withLedgerLock(fn func() error) error {
	lockPath, err := gitx.GitPath(p.dir, "pactify-log.lock")
	if err != nil {
		// Not a git repo (or git unavailable): there is no shared lock handle to
		// contend on — run unserialized, mirroring Merge's soft-skip of the base lock.
		return fn()
	}
	ctx, cancel := context.WithTimeout(context.Background(), ledgerWriteLockTimeout)
	defer cancel()
	release, err := lockx.Acquire(ctx, lockPath)
	if err != nil {
		return fmt.Errorf("pact: acquire ledger write lock: %w", err)
	}
	defer release()
	return fn()
}

// commitLedgerIfTracked keeps the ledger self-consistent for a repo that has
// deliberately chosen to git-track .pact/ (e.g. this repo, for a full audit
// history of its own dogfooding) — the uncommon case; a gitignored .pact/ (the
// default RunSandbox assumes) is untouched by this.
//
// Verbs otherwise only commit REAL file changes (checkpointLocked's HasChanges
// + CommitAll, for the worker's actual work) and never the ledger files
// themselves, so in a tracked-.pact/ repo every single verb call would leave
// log.jsonl/STATE.yml modified-but-uncommitted. Two real problems trace back to
// exactly that gap (2026-07-05 dogfood P4/P5): (1) a coding-agent worker,
// mid-task, sees the driver's own concurrent verb writes as an unexplained
// ".pact modified" diff next to its own work and — reasonably, not knowing this
// is expected — "fixes" it with `git restore`/hand-edits, corrupting state
// (P4); (2) RunSandbox's dirty-working-tree gate trips on the ledger's own
// perpetual dirtiness, refusing runs that have nothing actually wrong.
// Auto-committing the ledger the instant it changes removes both frictions.
// Best-effort and silent: a failed/no-op commit (nothing changed, lock
// contention, not a repo) must never fail the verb whose event is already
// durably appended.
func (p *Project) commitLedgerIfTracked() {
	logPath := paths.LogIn(p.dir)
	if !gitx.PathTracked(p.dir, logPath) {
		return
	}
	_ = gitx.CommitPaths(p.dir, "pact: ledger sync", logPath, paths.StateIn(p.dir))
}

func (p *Project) appendAndRender(ev event.Event) error {
	logPath := paths.LogIn(p.dir)
	if err := event.Append(logPath, ev); err != nil {
		return err
	}
	// Read the whole log ONCE as bytes: reuse them both to re-render STATE.yml and
	// to fingerprint the snapshot (its head hash is over these exact bytes).
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	evs, err := event.ParseAll(data)
	if err != nil {
		return err
	}
	st, folder := projection.Fold(evs)
	if err := projection.WriteState(paths.StateIn(p.dir), st); err != nil {
		return err
	}
	// Refresh the snapshot cache while still holding the ledger lock (callers wrap
	// appendAndRender in withLedgerLock), so the on-disk snapshot never lags the log
	// a reader could see. Best-effort: the snapshot is only a cache — a write failure
	// (or PACT_NO_SNAPSHOT) must not fail the verb, whose event is already durable.
	p.writeSnapshot(data, folder)
	p.commitLedgerIfTracked()
	return nil
}

// writeSnapshot atomically rewrites the ledger snapshot for the just-folded log.
// All errors are swallowed: the snapshot is a cache, and the next reader silently
// full-folds on any mismatch. On first creation it also registers the snapshot file
// with the repo's runtime-ignore list so this cache is never committed.
func (p *Project) writeSnapshot(logBytes []byte, folder *projection.Folder) {
	if os.Getenv(projection.NoSnapshotEnv) == "1" {
		return
	}
	snapPath := paths.StateSnapshotIn(p.dir)
	if _, err := os.Stat(snapPath); os.IsNotExist(err) {
		// First write: keep the cache out of git via .git/info/exclude (best-effort;
		// a non-git dir or git error just leaves it untracked-by-absence).
		_ = gitx.EnsureExcluded(p.dir, snapshotIgnoreMarker)
	}
	_ = projection.WriteSnapshot(snapPath, logBytes, folder)
}

// snapshotIgnoreMarker is the .git/info/exclude entry that keeps the ledger
// snapshot cache out of git — it is a projection, never the source of truth (the
// log is), so committing it would be a lie waiting to drift. Matches the default
// .pact/ location; a PACT_DIR-absolute override lands the cache outside the repo,
// where it is untracked anyway.
const snapshotIgnoreMarker = ".pact/state-snapshot.json"

// Init scaffolds .pact/, bakes entry files, and writes the init event.
func (p *Project) Init(project string, seatSpecs []string) error {
	return p.withLedgerLock(func() error { return p.initLocked(project, seatSpecs) })
}

func (p *Project) initLocked(project string, seatSpecs []string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	if project == "" {
		return errors.New("pactify init: --project required")
	}
	seats := make([]Seat, 0, len(seatSpecs))
	for _, spec := range seatSpecs {
		s, err := ParseSeat(spec)
		if err != nil {
			return err
		}
		seats = append(seats, s)
	}
	if err := os.MkdirAll(paths.BinIn(p.dir), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.TasksIn(p.dir), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(paths.LogIn(p.dir), nil, 0o644); err != nil {
		return err
	}

	seatPayload := make([]any, 0, len(seats))
	for _, s := range seats {
		roles := make([]any, len(s.Roles))
		for i, r := range s.Roles {
			roles[i] = r
		}
		seatPayload = append(seatPayload, map[string]any{"id": s.ID, "roles": roles, "entry": s.Entry, "kind": s.Kind})
		if err := BakeEntry(p.dir, s); err != nil {
			return err
		}
	}
	if err := BakeProject(paths.DirIn(p.dir), project, seats, paths.ProtocolVersion); err != nil {
		return err
	}

	base, _ := gitx.CurrentBranch(p.dir)
	return p.appendAndRender(event.Event{
		AgentID:   id,
		Role:      event.RoleFor("init"),
		EventType: "init",
		Payload: map[string]any{
			"project":          project,
			"protocol_version": paths.ProtocolVersion,
			"seats":            seatPayload,
			"base_branch":      base,
		},
	})
}

// AddSeat appends a new seat to the roster after init — fixing the "roster frozen
// at init" rigidity (the protocol has no remove-seat yet; YAGNI). Only an acting
// seat with the orchestrator role may add one, and the seat id must be unique. The
// seat's kind is recorded so the roster carries it (init seats now do too — this
// lets orchestrate infer --seat-kind from the roster). It does NOT bake the entry
// file: wiring stays a separate step (pactify agent add), matching init/wire split.
func (p *Project) AddSeat(spec string) error {
	return p.withLedgerLock(func() error { return p.addSeatLocked(spec) })
}

func (p *Project) addSeatLocked(spec string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	seat, err := ParseSeat(spec)
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := checkAddSeat(st, id, seat); err != nil {
		return err
	}
	roles := make([]any, len(seat.Roles))
	for i, r := range seat.Roles {
		roles[i] = r
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: "orchestrator", EventType: "add-seat",
		Payload: map[string]any{"id": seat.ID, "roles": roles, "entry": seat.Entry, "kind": seat.Kind},
	})
}

// Join registers the seat via the CLI client identity (pactify-cli + the
// injected ClientVersion). It is the cwd/CLI entry point; richer hosts (MCP)
// call JoinWithClient directly with their own clientInfo.
func (p *Project) Join(seatID, roles string) error {
	return p.JoinWithClient(seatID, roles, "pactify-cli", ClientVersion)
}

// JoinKind is Join plus a declared agent kind (spec §6 WS-K dynamic seats): the
// join event carries `kind`, which the projection records onto Agents[].Kind so a
// seat can DECLARE how it is driven at cold-start (and orchestrate resolves it from
// the live roster). An empty kind is byte-identical to Join.
func (p *Project) JoinKind(seatID, roles, kind string) error {
	return p.JoinWithClientKind(seatID, roles, "pactify-cli", ClientVersion, kind)
}

// JoinWithClient registers the seat (join event) and moves it onto its assigned
// task's feature branch (creating the branch from HEAD if absent).
//
// clientName/clientVersion are optional self-reported provenance: when
// clientName is non-empty the join payload gains a "client": {name, version}
// object; when empty no client key is emitted, so client-free logs stay
// byte-identical to pre-feature logs. Provenance lives in the log only — it is
// never projected into STATE.yml.
func (p *Project) JoinWithClient(seatID, roles, clientName, clientVersion string) error {
	return p.JoinWithClientKind(seatID, roles, clientName, clientVersion, "")
}

// JoinWithClientKind is JoinWithClient plus a declared agent kind (spec §6 WS-K).
// When kind is non-empty the join payload gains a "kind" field the projection reads
// into Agents[].Kind; an empty kind emits no kind field, so kind-free joins stay
// byte-identical to pre-feature logs.
func (p *Project) JoinWithClientKind(seatID, roles, clientName, clientVersion, kind string) error {
	return p.withLedgerLock(func() error {
		return p.joinWithClientLocked(seatID, roles, clientName, clientVersion, kind)
	})
}

func (p *Project) joinWithClientLocked(seatID, roles, clientName, clientVersion, kind string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	evs, err := event.ReadAll(paths.LogIn(p.dir))
	if err != nil {
		return err
	}
	if projVer := projectProtocolVersion(evs); projVer > paths.ProtocolVersion {
		return fmt.Errorf("pactify join: project protocol_version %d exceeds this client's supported %d; upgrade pactify", projVer, paths.ProtocolVersion)
	}
	rolesArr := splitCSV(roles)
	// Join gate: a seat may not join while any task it owns is blocked by a
	// dependency that has not reached `accepted`. Evaluate against pre-join
	// state so the gate cannot be bypassed by the join itself.
	preState, _, err := p.state()
	if err != nil {
		return err
	}
	if err := checkJoinGate(preState, seatID); err != nil {
		return err
	}
	payload := map[string]any{"roles": rolesArr, "protocol_version": paths.ProtocolVersion}
	// client is emitted ONLY when a name is present, mirroring deps' additive
	// conditional-serialization discipline (byte-parity for client-free logs).
	if clientName != "" {
		payload["client"] = map[string]any{"name": clientName, "version": clientVersion}
	}
	// kind is emitted ONLY when declared (spec §6 WS-K dynamic seats), same additive
	// discipline: kind-free joins keep byte-identical payloads.
	if kind != "" {
		payload["kind"] = kind
	}
	if err := p.appendAndRender(event.Event{
		AgentID:   id,
		Role:      event.RoleFor("join"),
		EventType: "join",
		Payload:   payload,
	}); err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	// Move the seat onto the branch for the work it's about to do. If the working
	// tree is ALREADY on a branch one of the seat's tasks targets, STAY there — the
	// orchestrator checks out the specific task's branch before launching the
	// worker, and a seat that owns tasks in several features must not be yanked onto
	// some other feature's branch (the old code checked out the FIRST owned
	// feature's branch, so multi-feature seats committed to the wrong branch).
	// Otherwise prefer the first actionable (not-yet-accepted) owned task's branch,
	// then any owned task's branch.
	cur, _ := gitx.CurrentBranch(p.dir)
	var actionable, anyOwned string
	for _, f := range st.Features {
		if f.Branch == "" {
			continue
		}
		for _, tk := range f.Tasks {
			if tk.Owner != seatID {
				continue
			}
			if cur != "" && f.Branch == cur {
				return nil // already on a branch this seat works — don't override
			}
			if anyOwned == "" {
				anyOwned = f.Branch
			}
			if actionable == "" && tk.Status != "accepted" {
				actionable = f.Branch
			}
		}
	}
	target := actionable
	if target == "" {
		target = anyOwned
	}
	if target != "" {
		return gitx.CheckoutOrCreate(p.dir, target)
	}
	return nil
}

func splitCSV(s string) []any {
	if s == "" {
		return []any{}
	}
	parts := []any{}
	for _, p := range splitComma(s) {
		parts = append(parts, p)
	}
	return parts
}

// Assign records a task assignment (rule: owner != reviewer; task ids unique).
// deps is an optional set of task ids in the SAME feature that must reach
// `accepted` before the owner may join (see Join gate). deps is validated at
// assign time (existence, same-feature, no self-dep, acyclic) and is recorded
// in the payload ONLY when non-empty so deps-free logs stay byte-identical.
func (p *Project) Assign(taskID, feature, branch, owner, reviewer, spec string, deps []string) error {
	return p.withLedgerLock(func() error {
		// Legacy single-reviewer path: quorum 0 with a one-element reviewer set
		// (empty reviewer → empty set so the "reviewer required" check still fires).
		var reviewers []string
		if reviewer != "" {
			reviewers = []string{reviewer}
		}
		return p.assignLocked(taskID, feature, branch, owner, reviewers, 0, spec, deps)
	})
}

// AssignQuorum records a quorum multi-reviewer assignment (spec review-runtime-
// deepening §2): reviewers is the reviewer set and quorum (≥1, ≤len(reviewers)) is
// how many DISTINCT reviewers must accept. It is the strictly opt-in counterpart to
// Assign; single-reviewer callers keep using Assign unchanged. The assign event
// payload carries `reviewers[]` + `quorum` (never the single `reviewer` key), which
// the projection reads back; the cmd layer enforces --reviewer/--reviewers mutual
// exclusion before calling either.
func (p *Project) AssignQuorum(taskID, feature, branch, owner string, reviewers []string, quorum int, spec string, deps []string) error {
	return p.withLedgerLock(func() error {
		return p.assignLocked(taskID, feature, branch, owner, reviewers, quorum, spec, deps)
	})
}

func (p *Project) assignLocked(taskID, feature, branch, owner string, reviewers []string, quorum int, spec string, deps []string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := checkAssign(st, id, taskID, feature, branch, owner, reviewers, quorum); err != nil {
		return err
	}
	if err := checkDeps(st, taskID, feature, deps); err != nil {
		return err
	}
	if spec == "" {
		// Repo-relative convention so the shared log never leaks a host-absolute
		// path (p.dir may be absolute). The file itself is still written/read via
		// dir-aware paths elsewhere.
		spec = paths.Tasks() + "/" + taskID + ".md"
	}
	payload := map[string]any{"owner": owner, "branch": branch, "spec": spec}
	// Payload shape is format-specific so each round-trips byte-identically:
	//   - quorum assign  → `reviewers[]` + `quorum` (no single `reviewer` key)
	//   - legacy assign  → single `reviewer` key (no reviewers/quorum keys)
	// The legacy branch reproduces the exact pre-feature payload, so its ledger
	// bytes and projection are unchanged (golden).
	if quorum > 0 {
		rs := make([]any, len(reviewers))
		for i, r := range reviewers {
			rs[i] = r
		}
		payload["reviewers"] = rs
		payload["quorum"] = quorum
	} else {
		reviewer := ""
		if len(reviewers) > 0 {
			reviewer = reviewers[0]
		}
		payload["reviewer"] = reviewer
	}
	if len(deps) > 0 {
		ds := make([]any, len(deps))
		for i, d := range deps {
			ds[i] = d
		}
		payload["deps"] = ds
	}
	return p.appendAndRender(event.Event{
		AgentID:   id,
		Role:      event.RoleFor("assign"),
		EventType: "assign",
		TaskID:    taskID,
		Feature:   feature,
		Payload:   payload,
	})
}

// Merge integrates a feature branch into the base branch (rule: all accepted).
func (p *Project) Merge(feature string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	// Serialize base-branch integration across processes/worktrees: hold an advisory
	// lock for the whole checkout→merge→event→commit critical section (which all
	// write base) so a concurrent `pactify merge` — another orchestrate run, or a
	// worktree sharing this .git — queues instead of racing on base (spec
	// coordination-authority P1). The lock file lives in the shared git common dir,
	// so every worktree of this repo contends on the same handle.
	//
	// LOCK ORDER: this base lock FIRST, then the ledger log lock (withLedgerLock
	// below) — Merge is the only verb holding both; keep this order everywhere or
	// two processes can deadlock against a verb holding the log lock alone.
	if lockPath, lerr := gitx.GitCommonPath(p.dir, "pactify-base.lock"); lerr == nil {
		lctx, cancel := context.WithTimeout(context.Background(), baseIntegrationLockTimeout)
		defer cancel()
		release, aerr := lockx.Acquire(lctx, lockPath)
		if aerr != nil {
			return fmt.Errorf("merge %s: acquire base-integration lock: %w", feature, aerr)
		}
		defer release()
	}
	return p.withLedgerLock(func() error { return p.mergeLocked(id, feature) })
}

func (p *Project) mergeLocked(id, feature string) error {
	st, evs, err := p.state()
	if err != nil {
		return err
	}
	if err := checkMerge(st, id, feature); err != nil {
		return err
	}
	// Only run the git merge when the feature has its OWN branch that actually
	// exists. If that branch was never created — the worker ran in-place on the
	// current branch (a serial run where join didn't check out a separate branch),
	// the feature's commits already live here, so there is nothing to git-merge.
	// (The old code unconditionally checked out base then merged the missing
	// branch, which failed and stranded the working tree on base.)
	branch := featureBranch(st, feature)
	base, explicit := baseBranch(evs)
	// Base sanity: if the base was captured implicitly at init (not set via
	// `config base-branch`) and it differs from the repo's actual default branch,
	// init almost certainly recorded a feature branch as the base — every merge
	// would integrate into the wrong branch and never reach the default. Refuse and
	// point at the fix, rather than silently shipping onto the wrong base.
	if !explicit {
		if def := gitx.DefaultBranch(p.dir); def != "" && base != def {
			return fmt.Errorf("merge %s: pact base branch is %q but the repo default is %q — init likely captured a feature branch as the base; run `pactify config base-branch %s` to correct it", feature, base, def, def)
		}
	}
	// A feature that declares its own branch (≠ base) MUST have that branch, and the
	// merge MUST integrate it into base. If the declared branch is missing, the
	// feature's work never landed there (e.g. the owner committed to a different
	// branch) — refuse to record a no-op merge as `shipped`, which would let pact's
	// state run ahead of git. A feature worked in-place declares no branch (or
	// branch == base): nothing to merge, ship as-is.
	if branch != "" && branch != base && !gitx.BranchExists(p.dir, branch) {
		return fmt.Errorf("merge %s: feature branch %q does not exist — its work never landed there (the owner likely committed to a different branch); refusing to record a no-op merge as shipped", feature, branch)
	}
	// Empty-feature guard: the branch exists but carries NO commits over base (base
	// already contains all of it) → the merge would integrate nothing, and recording
	// `shipped` would leave base unchanged (a phantom ship, pact state ahead of git —
	// e.g. a worker that checkpointed without committing any work). Refuse so it
	// escalates loudly instead of silently shipping. In sandbox runs this is what
	// makes "shipped but main unchanged" surface as an error rather than pass.
	if branch != "" && branch != base && gitx.BranchExists(p.dir, branch) && gitx.IsAncestor(p.dir, branch, base) {
		return fmt.Errorf("merge %s: feature branch %q has no commits over base %q — no work was committed there (base would be unchanged); refusing to record shipped", feature, branch, base)
	}
	if branch != "" && branch != base && gitx.BranchExists(p.dir, branch) {
		if ch, _ := gitx.HasChanges(p.dir); ch {
			if err := gitx.CommitAll(p.dir, "pact "+feature+": ledger before merge"); err != nil {
				return err
			}
		}
		// Fetch-aware integration (spec coordination-authority P3-3): when a remote
		// exists, sync base to origin and rebase the feature on top BEFORE merging, so
		// the result is a clean descendant of the current origin base — never a
		// divergent merge that would fork on push. If base diverged from origin, or the
		// feature can't cleanly rebase, refuse here rather than build a forked history.
		// No remote → purely local project → skip (unchanged local-first behavior).
		if base != "" && gitx.HasRemote(p.dir, "origin") {
			if err := gitx.Fetch(p.dir, "origin", base); err != nil {
				return fmt.Errorf("merge %s: %w", feature, err)
			}
			if gitx.RemoteBranchExists(p.dir, "origin", base) {
				ref := "origin/" + base
				if err := gitx.FastForwardTo(p.dir, base, ref); err != nil {
					return fmt.Errorf("merge %s: base %q diverged from %s — reconcile base with origin before merging (refusing a divergent merge): %w", feature, base, ref, err)
				}
				if err := gitx.RebaseOnto(p.dir, branch, ref); err != nil {
					return fmt.Errorf("merge %s: feature %q does not cleanly rebase onto %s — resolve the conflict and retry (refusing a divergent merge): %w", feature, branch, ref, err)
				}
			}
		}
		if base != "" {
			if err := gitx.Checkout(p.dir, base); err != nil {
				return err
			}
		}
		if err := gitx.MergeNoFF(p.dir, branch, "Merge "+feature+" ("+branch+")"); err != nil {
			return err
		}
		// Post-verify the ship transition: base must now actually contain the
		// feature branch. Guards against recording `shipped` when the integration
		// didn't land (the "pact state ahead of git" class of bug).
		if base != "" && !gitx.IsAncestor(p.dir, branch, base) {
			return fmt.Errorf("merge %s: branch %q is not contained in base %q after merge — refusing to record shipped", feature, branch, base)
		}
	}
	if err := p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("merge"), EventType: "merge",
		Feature: feature, Payload: map[string]any{},
	}); err != nil {
		return err
	}
	// Commit the merge event + re-rendered STATE.yml (now shipped) so HEAD matches
	// the working tree. appendAndRender only writes these to the tree; without this
	// final commit the merge commit captured the pre-merge STATE and HEAD lagged.
	if ch, _ := gitx.HasChanges(p.dir); ch {
		return gitx.CommitAll(p.dir, "pact "+feature+": merge (state shipped)")
	}
	return nil
}

// baseBranch returns the project's integration base and whether it was set
// EXPLICITLY via `config base-branch` (a rebaseline event) rather than captured
// implicitly at init. A later rebaseline overrides the init-time base — the way to
// correct a project whose init recorded a feature branch as the base.
func baseBranch(evs []event.Event) (branch string, explicit bool) {
	branch = initBaseBranch(evs)
	for _, e := range evs {
		if e.EventType == "rebaseline" {
			if s, ok := e.Payload["base_branch"].(string); ok && s != "" {
				branch, explicit = s, true
			}
		}
	}
	return branch, explicit
}

func initBaseBranch(evs []event.Event) string {
	for _, e := range evs {
		if e.EventType == "init" {
			if s, ok := e.Payload["base_branch"].(string); ok {
				return s
			}
		}
	}
	return ""
}

// Start records that the task's owner has been launched — the task-scoped
// "working" fact the projection lifts to in_progress. It exists for the
// orchestrate driver: a worker's own cold-start `join` (seat-scoped) is
// worker-reported and headless agents often skip it, leaving the board stuck on
// `assigned` while the owner works. Only an `assigned` task can start; anything
// further along already has a stronger status the start must not disturb.
func (p *Project) Start(taskID string) error {
	return p.withLedgerLock(func() error { return p.startLocked(taskID) })
}

func (p *Project) startLocked(taskID string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := requireOrchestrator(st, "start", id); err != nil {
		return err
	}
	t, f := findTask(st, taskID)
	if t == nil {
		return fmt.Errorf("start %s: no such task", taskID)
	}
	if t.Status != "assigned" {
		return fmt.Errorf("start %s: task is %s, not assigned", taskID, t.Status)
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("start"), EventType: "start",
		TaskID: taskID, Feature: f.ID, Payload: map[string]any{"owner": t.Owner},
	})
}

// Accept marks a task accepted (reviewer-only; must be awaiting_review).
func (p *Project) Accept(taskID string) error {
	return p.withLedgerLock(func() error { return p.acceptLocked(taskID) })
}

func (p *Project) acceptLocked(taskID string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	f, err := checkReviewerVerdict(st, "accept", id, taskID)
	if err != nil {
		return err
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("accept"), EventType: "accept",
		TaskID: taskID, Feature: f.ID, Payload: map[string]any{},
	})
}

// Changes sends a task back (reviewer-only; must be awaiting_review).
func (p *Project) Changes(taskID, reason string) error {
	return p.withLedgerLock(func() error { return p.changesLocked(taskID, reason) })
}

func (p *Project) changesLocked(taskID, reason string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	f, err := checkReviewerVerdict(st, "changes", id, taskID)
	if err != nil {
		return err
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("changes_requested"), EventType: "changes_requested",
		TaskID: taskID, Feature: f.ID, Payload: map[string]any{"reason": reason},
	})
}

// Checkpoint commits the work and submits a task for review (owner-only).
func (p *Project) Checkpoint(taskID, evidence string) error {
	return p.withLedgerLock(func() error { return p.checkpointLocked(taskID, evidence) })
}

func (p *Project) checkpointLocked(taskID, evidence string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, evs, err := p.state()
	if err != nil {
		return err
	}
	f, err := checkCheckpoint(st, id, taskID, evidence)
	if err != nil {
		return err
	}
	// Base-write guard (spec coordination-authority P3-1): if this task's feature
	// declares its own branch, the work MUST land there — never on base. A checkpoint
	// made while HEAD is on base would commit straight to the integration branch,
	// which only `pactify merge` may write. Refuse and point at the fix. (An in-place
	// feature that declares no branch is unaffected — backward compatible.)
	if base, _ := baseBranch(evs); f.Branch != "" && f.Branch != base {
		if cur, _ := gitx.CurrentBranch(p.dir); cur == base {
			return fmt.Errorf("checkpoint %s: HEAD is on base %q but feature %q declares branch %q — check out the feature branch first (only `pactify merge` writes base)", taskID, base, f.ID, f.Branch)
		}
	}
	// Git-first write order (the discipline Merge already follows): commit the work
	// BEFORE appending the checkpoint event, so a failed commit (concurrent
	// index.lock, disk full) can never leave the ledger durably claiming
	// awaiting_review-with-evidence while the branch has nothing — the exact
	// phantom-checkpoint class v0.8.1 (#29/#30) paid down. If the append below fails
	// after a successful commit, a re-run finds HasChanges false, skips the commit,
	// and just appends — self-healing.
	if ch, _ := gitx.HasChanges(p.dir); ch {
		if err := gitx.CommitAll(p.dir, "pact "+taskID+": checkpoint by "+id); err != nil {
			return err
		}
	}
	return p.appendAndRender(event.Event{
		AgentID:   id,
		Role:      event.RoleFor("checkpoint"),
		EventType: "checkpoint",
		TaskID:    taskID,
		Feature:   f.ID,
		Payload:   map[string]any{"evidence": evidence},
	})
}

// Status returns the rendered STATE.yml text (from the live log, through the
// snapshot cache — identical to a full replay; see StateProjection).
func (p *Project) Status() (string, error) {
	st, err := projection.LoadSnapshotState(paths.LogIn(p.dir), paths.StateSnapshotIn(p.dir))
	if err != nil {
		return "", err
	}
	return projection.Render(st), nil
}

// LogReplay rebuilds STATE.yml from the log.
func (p *Project) LogReplay() error {
	st, _, err := p.state()
	if err != nil {
		return err
	}
	return projection.WriteState(paths.StateIn(p.dir), st)
}

// LogText returns the raw log contents.
func (p *Project) LogText() (string, error) {
	b, err := os.ReadFile(paths.LogIn(p.dir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

// Validate runs the v1 conformance checks.
func (p *Project) Validate() error { return p.validateLog() }

// ---------------------------------------------------------------------------
// cwd-bound package-level wrappers. These preserve the historical behavior:
// every verb operates against the process cwd's .pact and PACT_AGENT_ID.
// ---------------------------------------------------------------------------

// Init scaffolds .pact/ in the current working directory.
func Init(project string, seatSpecs []string) error { return At(".").Init(project, seatSpecs) }

func AddSeat(spec string) error { return At(".").AddSeat(spec) }

// Join registers the seat in the current working directory's repo (CLI client
// identity: pactify-cli + ClientVersion).
func Join(seatID, roles string) error { return At(".").Join(seatID, roles) }

// JoinKind registers the seat in the current working directory's repo with a
// declared agent kind (spec §6 WS-K); empty kind is identical to Join.
func JoinKind(seatID, roles, kind string) error { return At(".").JoinKind(seatID, roles, kind) }

// JoinWithClient registers the seat in the current working directory's repo with
// caller-supplied client provenance (empty clientName → no client field).
func JoinWithClient(seatID, roles, clientName, clientVersion string) error {
	return At(".").JoinWithClient(seatID, roles, clientName, clientVersion)
}

// Assign records a task assignment in the current working directory's repo.
func Assign(taskID, feature, branch, owner, reviewer, spec string, deps []string) error {
	return At(".").Assign(taskID, feature, branch, owner, reviewer, spec, deps)
}

// AssignQuorum records a quorum multi-reviewer assignment in the current working
// directory's repo (see (*Project).AssignQuorum).
func AssignQuorum(taskID, feature, branch, owner string, reviewers []string, quorum int, spec string, deps []string) error {
	return At(".").AssignQuorum(taskID, feature, branch, owner, reviewers, quorum, spec, deps)
}

// Merge integrates a feature branch in the current working directory's repo.
func Merge(feature string) error { return At(".").Merge(feature) }

// Accept marks a task accepted in the current working directory's repo.
func Accept(taskID string) error { return At(".").Accept(taskID) }

// Changes sends a task back in the current working directory's repo.
func Changes(taskID, reason string) error { return At(".").Changes(taskID, reason) }

// Cancel retires a single task in the current working directory's repo.
func Cancel(taskID string) error { return At(".").Cancel(taskID) }

// Withdraw retires a whole feature in the current working directory's repo.
func Withdraw(feature string) error { return At(".").Withdraw(feature) }

// ConfigBaseBranch sets the integration base in the current working directory's repo.
func ConfigBaseBranch(branch string) error { return At(".").ConfigBaseBranch(branch) }

// ConfigBaseBranch records a rebaseline event that overrides the init-time base
// branch — used to correct a project whose init captured a feature branch as the
// base (so merges would target the wrong branch). Append-only.
func (p *Project) ConfigBaseBranch(branch string) error {
	return p.withLedgerLock(func() error { return p.configBaseBranchLocked(branch) })
}

func (p *Project) configBaseBranchLocked(branch string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	if branch == "" {
		return fmt.Errorf("config base-branch: branch is required")
	}
	// Rebaselining redirects every future merge; only an orchestrator seat may.
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := requireOrchestrator(st, "config base-branch", id); err != nil {
		return err
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("rebaseline"), EventType: "rebaseline",
		Payload: map[string]any{"base_branch": branch},
	})
}

// BaseBranch returns the project's integration base branch: the init-time base
// overridden by any later rebaseline event. It returns "" when no base has been
// recorded.
func (p *Project) BaseBranch() (string, error) {
	evs, err := event.ReadAll(paths.LogIn(p.dir))
	if err != nil {
		return "", err
	}
	b, _ := baseBranch(evs)
	return b, nil
}

// BaseBranch returns the current repo's integration base branch.
func BaseBranch() (string, error) { return At(".").BaseBranch() }

// ConfigGate sets the project hard-gate command in the current working directory's repo.
func ConfigGate(command string) error { return At(".").ConfigGate(command) }

// ConfigGate records a config_gate event setting the hard-gate command the
// orchestrate driver runs before every merge for tasks that declare no per-task
// `verify:` line. A later config_gate overrides an earlier one. Append-only.
func (p *Project) ConfigGate(command string) error {
	return p.withLedgerLock(func() error { return p.configGateLocked(command) })
}

func (p *Project) configGateLocked(command string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("config gate: command is required")
	}
	// The gate command guards every merge; only an orchestrator seat may set it.
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := requireOrchestrator(st, "config gate", id); err != nil {
		return err
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("config_gate"), EventType: "config_gate",
		Payload: map[string]any{"gate": command},
	})
}

// GateConfig returns the project's configured hard-gate command (the latest
// `config gate` setting) and whether one was set. orchestrate uses it ahead of a
// project-type-inferred default. A log read failure is returned as an error,
// DISTINCT from "no gate configured" (a missing log reads as absent, not error):
// the consumer is the pre-merge safety gate, and swallowing a transient I/O error
// as "unconfigured" would silently degrade it to the type-default fallback.
func (p *Project) GateConfig() (string, bool, error) {
	evs, err := event.ReadAll(paths.LogIn(p.dir))
	if err != nil {
		return "", false, fmt.Errorf("pact: read log for gate config: %w", err)
	}
	gate, ok := "", false
	for _, e := range evs {
		if e.EventType == "config_gate" {
			if s, o := e.Payload["gate"].(string); o && s != "" {
				gate, ok = s, true
			}
		}
	}
	return gate, ok, nil
}

// ConfigCritic sets the project's pre-review critic seat in the current working
// directory's repo.
func ConfigCritic(seat string) error { return At(".").ConfigCritic(seat) }

// ConfigCritic records a config_critic event naming the seat the orchestrate
// driver runs as a read-only pre-review critic (spec review-runtime-deepening §3
// WS-H): after a task's verify gate is green and before its reviewer, the critic
// scores the diff vs the spec. The score has NO gating power — it only steers the
// reviewer's attention. Mirrors ConfigGate: a later config_critic overrides an
// earlier one; append-only; orchestrator-only.
func (p *Project) ConfigCritic(seat string) error {
	return p.withLedgerLock(func() error { return p.configCriticLocked(seat) })
}

func (p *Project) configCriticLocked(seat string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	if strings.TrimSpace(seat) == "" {
		return fmt.Errorf("config critic: seat is required")
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := requireOrchestrator(st, "config critic", id); err != nil {
		return err
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("config_critic"), EventType: "config_critic",
		Payload: map[string]any{"critic": seat},
	})
}

// CriticConfig returns the project's configured critic seat (the latest
// `config critic` setting) and whether one was set. orchestrate reads it to
// decide whether to run a pre-review critic stint. Mirrors GateConfig: absent is
// distinct from an unreadable log (a missing log reads as absent, not error).
func (p *Project) CriticConfig() (string, bool, error) {
	evs, err := event.ReadAll(paths.LogIn(p.dir))
	if err != nil {
		return "", false, fmt.Errorf("pact: read log for critic config: %w", err)
	}
	seat, ok := "", false
	for _, e := range evs {
		if e.EventType == "config_critic" {
			if s, o := e.Payload["critic"].(string); o && s != "" {
				seat, ok = s, true
			}
		}
	}
	return seat, ok, nil
}

// Note records a task-scoped annotation on the ledger that drives NO state
// transition — the "note-style" mechanism (spec §3 WS-H, I-3). It deliberately
// REUSES the existing `start` event_type rather than introducing a new one: the
// projection lifts a `start` event ONLY for an `assigned` task, so a note on any
// further-along task (the critic pre-review score's awaiting_review case) is a
// pure projection no-op. The payload carries the annotation (e.g. critic_score /
// critic_by / reason). Orchestrator-only, like the driver's other own writes
// (start / merge).
func (p *Project) Note(taskID string, payload map[string]any) error {
	return p.withLedgerLock(func() error { return p.noteLocked(taskID, payload) })
}

func (p *Project) noteLocked(taskID string, payload map[string]any) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := requireOrchestrator(st, "note", id); err != nil {
		return err
	}
	t, f := findTask(st, taskID)
	if t == nil {
		return fmt.Errorf("note %s: no such task", taskID)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("start"), EventType: "start",
		TaskID: taskID, Feature: f.ID, Payload: payload,
	})
}

// Escalate records a task-level escalate event. It does not change the task
// state machine (the projection ignores escalate events); it is an
// observability-only ledger write emitted by the orchestrate driver when a task
// cannot converge and needs human intervention. The payload carries the human
// reason and the seat that triggered the escalation.
func (p *Project) Escalate(taskID, feature, reason, seat string) error {
	return p.withLedgerLock(func() error { return p.escalateLocked(taskID, feature, reason, seat) })
}

func (p *Project) escalateLocked(taskID, feature, reason, seat string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := requireOrchestrator(st, "escalate", id); err != nil {
		return err
	}
	if feature == "" {
		for _, f := range st.Features {
			for _, t := range f.Tasks {
				if t.ID == taskID {
					feature = f.ID
					break
				}
			}
		}
	}
	if feature == "" {
		return fmt.Errorf("escalate: no feature for task %q", taskID)
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("escalate"), EventType: "escalate",
		TaskID: taskID, Feature: feature,
		Payload: map[string]any{"reason": reason, "seat": seat},
	})
}

// Cancel records a cancel event that excludes taskID from the projection — the
// structured way to retire a task without hand-editing the log. Append-only: the
// task's history stays in the log; the projection simply drops it.
func (p *Project) Cancel(taskID string) error {
	return p.withLedgerLock(func() error { return p.cancelLocked(taskID) })
}

func (p *Project) cancelLocked(taskID string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := requireOrchestrator(st, "cancel", id); err != nil {
		return err
	}
	feature := ""
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if t.ID == taskID {
				feature = f.ID
			}
		}
	}
	if feature == "" {
		return fmt.Errorf("cancel: task %q not found", taskID)
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("cancel"), EventType: "cancel",
		Feature: feature, TaskID: taskID, Payload: map[string]any{},
	})
}

// Withdraw records a withdraw event that excludes the whole feature from the
// projection. The feature's branch/commits stay in git untouched (retire ≠ delete).
func (p *Project) Withdraw(feature string) error {
	return p.withLedgerLock(func() error { return p.withdrawLocked(feature) })
}

func (p *Project) withdrawLocked(feature string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := requireOrchestrator(st, "withdraw", id); err != nil {
		return err
	}
	found := false
	for _, f := range st.Features {
		if f.ID == feature {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("withdraw: feature %q not found", feature)
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("withdraw"), EventType: "withdraw",
		Feature: feature, Payload: map[string]any{},
	})
}

// Checkpoint submits a task for review in the current working directory's repo.
func Checkpoint(taskID, evidence string) error { return At(".").Checkpoint(taskID, evidence) }

// Status returns rendered STATE.yml for the current working directory's repo.
func Status() (string, error) { return At(".").Status() }

// LogReplay rebuilds STATE.yml for the current working directory's repo.
func LogReplay() error { return At(".").LogReplay() }

// LogText returns the raw log for the current working directory's repo.
func LogText() (string, error) { return At(".").LogText() }

// Validate runs the v1 conformance checks against the current working dir.
func Validate() error { return At(".").Validate() }
