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

func requireAgentID() (string, error) {
	id := paths.AgentID()
	if id == "" {
		return "", errNoAgent
	}
	return id, nil
}

func state() (projection.State, []event.Event, error) {
	evs, err := event.ReadAll(paths.Log())
	if err != nil {
		return projection.State{}, nil, err
	}
	return projection.Project(evs), evs, nil
}

func appendAndRender(ev event.Event) error {
	if err := event.Append(paths.Log(), ev); err != nil {
		return err
	}
	evs, err := event.ReadAll(paths.Log())
	if err != nil {
		return err
	}
	return projection.WriteState(paths.State(), projection.Project(evs))
}

// Init scaffolds .pact/, bakes entry files, and writes the init event.
func Init(project string, seatSpecs []string) error {
	if _, err := requireAgentID(); err != nil {
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
	if err := os.MkdirAll(paths.Bin(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.Tasks(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(paths.Log(), nil, 0o644); err != nil {
		return err
	}

	seatPayload := make([]any, 0, len(seats))
	for _, s := range seats {
		roles := make([]any, len(s.Roles))
		for i, r := range s.Roles {
			roles[i] = r
		}
		seatPayload = append(seatPayload, map[string]any{"id": s.ID, "roles": roles, "entry": s.Entry})
		if err := BakeEntry(".", s); err != nil {
			return err
		}
	}
	if err := BakeProject(paths.Dir(), project, seats, paths.ProtocolVersion); err != nil {
		return err
	}

	base, _ := gitx.CurrentBranch(".")
	id := paths.AgentID()
	return appendAndRender(event.Event{
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
func Join(seatID, roles string) error {
	id, err := requireAgentID()
	if err != nil {
		return err
	}
	rolesArr := splitCSV(roles)
	if err := appendAndRender(event.Event{
		AgentID:   id,
		Role:      event.RoleFor("join"),
		EventType: "join",
		Payload:   map[string]any{"roles": rolesArr},
	}); err != nil {
		return err
	}
	st, _, err := state()
	if err != nil {
		return err
	}
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			if tk.Owner == seatID && f.Branch != "" {
				return gitx.CheckoutOrCreate(".", f.Branch)
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
func Assign(taskID, feature, branch, owner, reviewer, spec string) error {
	id, err := requireAgentID()
	if err != nil {
		return err
	}
	st, _, err := state()
	if err != nil {
		return err
	}
	if err := checkAssign(st, taskID, owner, reviewer); err != nil {
		return err
	}
	if spec == "" {
		spec = paths.Tasks() + "/" + taskID + ".md"
	}
	return appendAndRender(event.Event{
		AgentID:   id,
		Role:      event.RoleFor("assign"),
		EventType: "assign",
		TaskID:    taskID,
		Feature:   feature,
		Payload:   map[string]any{"owner": owner, "reviewer": reviewer, "branch": branch, "spec": spec},
	})
}

// Accept marks a task accepted (reviewer-only; must be awaiting_review).
func Accept(taskID string) error {
	id, err := requireAgentID()
	if err != nil {
		return err
	}
	st, _, err := state()
	if err != nil {
		return err
	}
	f, err := checkReviewerVerdict(st, "accept", id, taskID)
	if err != nil {
		return err
	}
	return appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("accept"), EventType: "accept",
		TaskID: taskID, Feature: f.ID, Payload: map[string]any{},
	})
}

// Changes sends a task back (reviewer-only; must be awaiting_review).
func Changes(taskID, reason string) error {
	id, err := requireAgentID()
	if err != nil {
		return err
	}
	st, _, err := state()
	if err != nil {
		return err
	}
	f, err := checkReviewerVerdict(st, "changes", id, taskID)
	if err != nil {
		return err
	}
	return appendAndRender(event.Event{
		AgentID: id, Role: event.RoleFor("changes_requested"), EventType: "changes_requested",
		TaskID: taskID, Feature: f.ID, Payload: map[string]any{"reason": reason},
	})
}

// Checkpoint submits a task for review (owner-only) and commits the work.
func Checkpoint(taskID, evidence string) error {
	id, err := requireAgentID()
	if err != nil {
		return err
	}
	st, _, err := state()
	if err != nil {
		return err
	}
	f, err := checkCheckpoint(st, id, taskID, evidence)
	if err != nil {
		return err
	}
	if err := appendAndRender(event.Event{
		AgentID:   id,
		Role:      event.RoleFor("checkpoint"),
		EventType: "checkpoint",
		TaskID:    taskID,
		Feature:   f.ID,
		Payload:   map[string]any{"evidence": evidence},
	}); err != nil {
		return err
	}
	if ch, _ := gitx.HasChanges("."); ch {
		return gitx.CommitAll(".", "pact "+taskID+": checkpoint by "+id)
	}
	return nil
}
