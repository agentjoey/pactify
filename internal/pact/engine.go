package pact

import (
	"errors"
	"os"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

var errNoAgent = errors.New("pactify: PACT_AGENT_ID not set; source your entry file")

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
		seatPayload = append(seatPayload, map[string]any{"id": s.ID, "roles": roles, "entry": s.Entry})
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

// Join registers the seat (join event) and moves it onto its assigned task's
// feature branch (creating the branch from HEAD if absent).
func (p *Project) Join(seatID, roles string) error {
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
	if err := p.appendAndRender(event.Event{
		AgentID:   id,
		Role:      event.RoleFor("join"),
		EventType: "join",
		Payload:   map[string]any{"roles": rolesArr},
	}); err != nil {
		return err
	}
	st, _, err := p.state()
	if err != nil {
		return err
	}
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			if tk.Owner == seatID && f.Branch != "" {
				return gitx.CheckoutOrCreate(p.dir, f.Branch)
			}
		}
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
		spec = paths.TasksIn(p.dir) + "/" + taskID + ".md"
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
	branch := featureBranch(st, feature)
	if branch != "" {
		if ch, _ := gitx.HasChanges(p.dir); ch {
			if err := gitx.CommitAll(p.dir, "pact "+feature+": ledger before merge"); err != nil {
				return err
			}
		}
		base := initBaseBranch(evs)
		if base != "" && base != branch {
			if err := gitx.Checkout(p.dir, base); err != nil {
				return err
			}
		}
		if err := gitx.MergeNoFF(p.dir, branch, "Merge "+feature+" ("+branch+")"); err != nil {
			return err
		}
	}
	return p.appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("merge"), EventType: "merge",
		Feature: feature, Payload: map[string]any{},
	})
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

// Join registers the seat in the current working directory's repo.
func Join(seatID, roles string) error { return At(".").Join(seatID, roles) }

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
