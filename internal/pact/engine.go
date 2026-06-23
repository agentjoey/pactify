package pact

import (
	"context"
	"errors"
	"fmt"
	"os"
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
// wrapper around the internal projection used by the orchestrate driver).
func (p *Project) StateProjection() (projection.State, error) {
	st, _, err := p.state()
	return st, err
}

func (p *Project) state() (projection.State, []event.Event, error) {
	evs, err := event.ReadAll(paths.LogIn(p.dir))
	if err != nil {
		return projection.State{}, nil, err
	}
	return projection.Project(evs), evs, nil
}

func (p *Project) appendAndRender(ev event.Event) error {
	if err := event.Append(paths.LogIn(p.dir), ev); err != nil {
		return err
	}
	evs, err := event.ReadAll(paths.LogIn(p.dir))
	if err != nil {
		return err
	}
	return projection.WriteState(paths.StateIn(p.dir), projection.Project(evs))
}

// Init scaffolds .pact/, bakes entry files, and writes the init event.
func (p *Project) Init(project string, seatSpecs []string) error {
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

// JoinWithClient registers the seat (join event) and moves it onto its assigned
// task's feature branch (creating the branch from HEAD if absent).
//
// clientName/clientVersion are optional self-reported provenance: when
// clientName is non-empty the join payload gains a "client": {name, version}
// object; when empty no client key is emitted, so client-free logs stay
// byte-identical to pre-feature logs. Provenance lives in the log only — it is
// never projected into STATE.yml.
func (p *Project) JoinWithClient(seatID, roles, clientName, clientVersion string) error {
	id, err := p.agentID()
	if err != nil {
		return err
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
	payload := map[string]any{"roles": rolesArr}
	// client is emitted ONLY when a name is present, mirroring deps' additive
	// conditional-serialization discipline (byte-parity for client-free logs).
	if clientName != "" {
		payload["client"] = map[string]any{"name": clientName, "version": clientVersion}
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
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	if err := checkAssign(st, taskID, owner, reviewer); err != nil {
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
	payload := map[string]any{"owner": owner, "reviewer": reviewer, "branch": branch, "spec": spec}
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
	st, evs, err := p.state()
	if err != nil {
		return err
	}
	if err := checkMerge(st, feature); err != nil {
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
	// Serialize base-branch integration across processes/worktrees: hold an advisory
	// lock for the whole checkout→merge→event→commit critical section (which all
	// write base) so a concurrent `pactify merge` — another orchestrate run, or a
	// worktree sharing this .git — queues instead of racing on base (spec
	// coordination-authority P1). The lock file lives in the shared git common dir,
	// so every worktree of this repo contends on the same handle.
	if lockPath, lerr := gitx.GitPath(p.dir, "pactify-base.lock"); lerr == nil {
		lctx, cancel := context.WithTimeout(context.Background(), baseIntegrationLockTimeout)
		defer cancel()
		release, aerr := lockx.Acquire(lctx, lockPath)
		if aerr != nil {
			return fmt.Errorf("merge %s: acquire base-integration lock: %w", feature, aerr)
		}
		defer release()
	}
	if branch != "" && branch != base && gitx.BranchExists(p.dir, branch) {
		if ch, _ := gitx.HasChanges(p.dir); ch {
			if err := gitx.CommitAll(p.dir, "pact "+feature+": ledger before merge"); err != nil {
				return err
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

// Accept marks a task accepted (reviewer-only; must be awaiting_review).
func (p *Project) Accept(taskID string) error {
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

// Checkpoint submits a task for review (owner-only) and commits the work.
func (p *Project) Checkpoint(taskID, evidence string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	f, err := checkCheckpoint(st, id, taskID, evidence)
	if err != nil {
		return err
	}
	if err := p.appendAndRender(event.Event{
		AgentID:   id,
		Role:      event.RoleFor("checkpoint"),
		EventType: "checkpoint",
		TaskID:    taskID,
		Feature:   f.ID,
		Payload:   map[string]any{"evidence": evidence},
	}); err != nil {
		return err
	}
	if ch, _ := gitx.HasChanges(p.dir); ch {
		return gitx.CommitAll(p.dir, "pact "+taskID+": checkpoint by "+id)
	}
	return nil
}

// Status returns the rendered STATE.yml text (from the live log).
func (p *Project) Status() (string, error) {
	st, _, err := p.state()
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

// JoinWithClient registers the seat in the current working directory's repo with
// caller-supplied client provenance (empty clientName → no client field).
func JoinWithClient(seatID, roles, clientName, clientVersion string) error {
	return At(".").JoinWithClient(seatID, roles, clientName, clientVersion)
}

// Assign records a task assignment in the current working directory's repo.
func Assign(taskID, feature, branch, owner, reviewer, spec string, deps []string) error {
	return At(".").Assign(taskID, feature, branch, owner, reviewer, spec, deps)
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
	id, err := p.agentID()
	if err != nil {
		return err
	}
	if branch == "" {
		return fmt.Errorf("config base-branch: branch is required")
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("rebaseline"), EventType: "rebaseline",
		Payload: map[string]any{"base_branch": branch},
	})
}

// Cancel records a cancel event that excludes taskID from the projection — the
// structured way to retire a task without hand-editing the log. Append-only: the
// task's history stays in the log; the projection simply drops it.
func (p *Project) Cancel(taskID string) error {
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
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
	id, err := p.agentID()
	if err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
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
