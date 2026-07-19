// Package remoteexec is the pactify serve-side of the U3 reverse control plane:
// it turns a pact command received over the relay (rpc) into a local pact verb
// execution, account-scoped. This package is transport-agnostic and holds no
// socket — the Dispatcher is a pure command handler so it can be unit-tested
// without a relay, and wired to a real relay socket in a later phase (see
// .agent/plans/u3-reverse-control-plane). It defines pactify's OWN rpc types in
// Go; adding them to the shared cloud/wire RpcRequest + redeploying the relay is
// the coordinated step P-b, separate from this slice.
package remoteexec

import (
	"errors"
	"fmt"
	"strings"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
)

// RPC is one pact command couriered from a remote control plane (hosted web, or
// another machine) via the relay. Only pactify's own command surface — never a
// linx rpc type. Args carry the verb parameters (task/feature/branch/owner/
// reviewer/spec/reason/evidence/deps) so the wire shape stays a flat, bounded map.
type RPC struct {
	Type        string            // e.g. "pact.assign" (see the switch in Handle for the set)
	Account     string            // account scope of the caller — must match this machine's
	Project     string            // registered project name the verb targets
	Task        string            // task id (assign/accept/changes/checkpoint)
	Feature     string            // feature id (assign/merge)
	Branch      string            // feature branch (assign)
	Owner       string            // task owner seat (assign)
	Reviewer    string            // task reviewer seat (assign)
	Spec        string            // task spec path (assign)
	Reason      string            // changes reason
	Evidence    string            // checkpoint evidence
	Deps        []string          // assign dependencies
	Seat        string            // stint: seat to act as
	AgentKind   string            // stint: agent CLI kind to spawn
	Briefing    string            // stint: prompt for the agent
	SeatKinds   map[string]string // orchestrate.run: seat → agent kind
	Goal        string            // plan.generate: the goal prompt
	PlannerKind string            // plan.generate: planner agent kind
	RepoURL     string            // provision: git URL to clone
	Name        string            // provision: registry name for the clone
	ID          string            // task: task slug
	SpecMD      string            // task: task draft spec markdown
	Text        string            // cockpit.prompt: user prompt text
	RequestID   string            // cockpit.permission: approval id
	Decision    string            // cockpit.permission: allow/deny/allow_session
}

// StintRequest is a remote request to run one agent stint (spawn an agent CLI to
// work a task, as a seat). It runs code, so serve gates it behind a per-project
// policy. Distinct from the pact-verb path (PactEngine).
type StintRequest struct {
	Project   string
	Task      string
	Seat      string
	AgentKind string
	Briefing  string
	Branch    string // feature branch to run on (from the driver; "" = resolve locally)
}

// Provisioner clones a repo onto the machine and registers it (pact.provision).
// serve implements it (machine-level policy: requires a configured provision
// dir; default off). Nil = remote provisioning disabled. It has no project (the
// project doesn't exist yet), so it's dispatched before the project resolve.
type Provisioner interface {
	Provision(repoURL, name string) (registeredName string, err error)
}

// PlanRequest drives remote plan generation/application.
type PlanRequest struct {
	Project     string
	Feature     string
	Goal        string // generate only
	PlannerKind string // generate only
	Apply       bool   // false = generate, true = apply
}

// Planner runs remote plan.generate/apply. serve implements it (policy-gated,
// same spawn/apply path the dashboard uses). Nil = remote plan disabled.
type Planner interface {
	RunPlan(req PlanRequest) error
}

// OrchestrateRequest asks the machine to start (or resume) its orchestrate
// driver for a project — the remote entry to a whole autonomous run, vs a single
// stint. Policy-gated like stint (it runs many agents).
type OrchestrateRequest struct {
	Project   string
	Feature   string
	SeatKinds map[string]string
	Resume    bool
}

// Orchestrator starts the orchestrate driver remotely. serve implements it
// (policy check + the same spawn path the dashboard's Run button uses). Nil =
// remote orchestrate disabled.
type Orchestrator interface {
	RunOrchestrate(req OrchestrateRequest) error
}

// TaskAuthor writes a task draft spec (.pact/tasks/{id}.md). serve implements it;
// it's a coordination write (no code runs), gated like the pact verbs (available
// under --remote-control). Nil = disabled.
type TaskAuthor interface {
	AuthorTask(project, id, specMD string) error
}

// Stinter runs a remote agent stint. serve implements it (policy check + agent
// spawn via the orchestrate runner). Nil on a Dispatcher = remote stint disabled.
// RunStint validates + accepts synchronously (returning an error to reject, e.g.
// policy-denied), then runs the agent asynchronously — the effect returns via the
// event stream (the agent's checkpoint), not this call.
type Stinter interface {
	RunStint(req StintRequest) error
}

// Cockpiter drives a project's hosted cockpit session remotely (prompt,
// permission resolve, cancel, resume, subscribe). serve implements it; the
// upward event mirror is emitted back through the machine socket.
type Cockpiter interface {
	Prompt(project, seat, text string) error
	Permission(project, seat, requestID, decision string) error
	Cancel(project, seat string) error
	Resume(project, seat string) error
	Subscribe(project, seat string) error
}

// CockpitPolicy checks whether hosted cockpit is enabled for a project. It
// returns an error for unknown/unregistered projects; false means the project
// exists but remote cockpit is disabled.
type CockpitPolicy func(project string) (bool, error)

// Reply is the executor's result, couriered back to the caller. Errors are
// operational strings (never leak internals); OK=false always carries Error.
type Reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// RunID carries an operation identifier back to the caller (e.g. the
	// registered project name from a provision). Optional.
	RunID string `json:"runId,omitempty"`
}

// PactEngine is the subset of *pact.Project the dispatcher drives. *pact.Project
// satisfies it; tests pass a fake. Kept minimal — remote control is intentionally
// limited to the coordination verbs, not raw ledger surgery.
type PactEngine interface {
	Assign(taskID, feature, branch, owner, reviewer, spec string, deps []string) error
	Accept(taskID string) error
	Changes(taskID, reason string) error
	Merge(feature string) error
	Checkpoint(taskID, evidence string) error
}

// Resolver maps a registered project name to its pact engine (its repo dir). It
// returns an error for an unknown/unregistered project so a stray or malicious
// rpc can't touch an arbitrary path.
type Resolver func(project string) (PactEngine, error)

// ErrAccountScope is returned when an rpc's account does not match the machine's.
var ErrAccountScope = errors.New("account scope mismatch")

// Dispatcher executes an authorized RPC against the resolved project's pact
// engine. It is stateless apart from the machine's account id and the resolver.
type Dispatcher struct {
	// Account is this machine's account; every rpc must match it (defense in
	// depth — the relay already scopes delivery by account, this re-asserts it).
	Account string
	// Resolve maps rpc.Project → engine. Required for the pact verbs.
	Resolve Resolver
	// Stint runs remote agent stints (pact.stint). Nil = remote stint disabled
	// (the safe default; serve wires a policy-gated Stinter when enabled).
	Stint Stinter
	// Orch starts the orchestrate driver (orchestrate.run/resume). Nil = disabled.
	Orch Orchestrator
	// Plan runs remote plan generate/apply (plan.generate/apply). Nil = disabled.
	Plan Planner
	// Prov clones + registers a repo (pact.provision). Nil = disabled.
	Prov Provisioner
	// Task authors a task draft (pact.task). Nil = disabled.
	Task TaskAuthor
	// Cockpit drives hosted cockpit sessions remotely (cockpit.*). Nil = disabled.
	Cockpit Cockpiter
	// CockpitPolicy gates cockpit.* rpcs per project. Nil = disabled (fail closed).
	CockpitPolicy CockpitPolicy
}

// Handle executes one rpc and returns a Reply. It never panics on bad input:
// unknown types, scope mismatches, unknown projects, and verb errors all become
// Reply{OK:false, Error:...}.
func (d *Dispatcher) Handle(rpc RPC) Reply {
	// Auth scope: an empty machine account accepts nothing (fail closed).
	if d.Account == "" || rpc.Account != d.Account {
		return fail(ErrAccountScope.Error())
	}
	// Provision has no project (it creates one) — dispatch before the project check.
	if rpc.Type == "pact.provision" {
		if d.Prov == nil {
			return fail("remote provisioning not enabled for this machine")
		}
		if rpc.RepoURL == "" || rpc.Name == "" {
			return fail("pact.provision requires repoUrl and name")
		}
		name, err := d.Prov.Provision(rpc.RepoURL, rpc.Name)
		if err != nil {
			return fail(err.Error())
		}
		return Reply{OK: true, RunID: name}
	}
	if rpc.Type == "pact.task" {
		if d.Task == nil {
			return fail("remote task authoring not enabled for this machine")
		}
		if rpc.Project == "" || rpc.ID == "" {
			return fail("pact.task requires project and id")
		}
		if err := d.Task.AuthorTask(rpc.Project, rpc.ID, rpc.SpecMD); err != nil {
			return fail(err.Error())
		}
		return Reply{OK: true}
	}
	if rpc.Project == "" {
		return fail("rpc missing project")
	}
	// plan.generate/apply — policy-gated (generate spawns the planner agent).
	if rpc.Type == "plan.generate" || rpc.Type == "plan.apply" {
		if d.Plan == nil {
			return fail("remote plan not enabled for this machine")
		}
		if rpc.Feature == "" {
			return fail("plan rpc requires feature")
		}
		if err := d.Plan.RunPlan(PlanRequest{
			Project: rpc.Project, Feature: rpc.Feature, Goal: rpc.Goal,
			PlannerKind: rpc.PlannerKind, Apply: rpc.Type == "plan.apply",
		}); err != nil {
			return fail(err.Error())
		}
		return Reply{OK: true}
	}
	// orchestrate.run/resume starts the whole driver — policy-gated like stint.
	if rpc.Type == "orchestrate.run" || rpc.Type == "orchestrate.resume" {
		if d.Orch == nil {
			return fail("remote orchestrate not enabled for this machine")
		}
		if err := d.Orch.RunOrchestrate(OrchestrateRequest{
			Project: rpc.Project, Feature: rpc.Feature,
			SeatKinds: rpc.SeatKinds, Resume: rpc.Type == "orchestrate.resume",
		}); err != nil {
			return fail(err.Error())
		}
		return Reply{OK: true}
	}
	// Stint runs an agent (code), not a pact verb — a distinct, policy-gated path
	// (the Stinter), handled before the pact-engine resolve.
	if rpc.Type == "pact.stint" {
		if d.Stint == nil {
			return fail("remote stint not enabled for this machine")
		}
		if rpc.Seat == "" || rpc.AgentKind == "" || rpc.Task == "" {
			return fail("pact.stint requires task, seat, agentKind")
		}
		if rpc.Branch != "" && !gitx.ValidBranchName(rpc.Branch) {
			return fail("pact.stint: invalid branch")
		}
		if err := d.Stint.RunStint(StintRequest{
			Project: rpc.Project, Task: rpc.Task, Seat: rpc.Seat,
			AgentKind: rpc.AgentKind, Briefing: rpc.Briefing, Branch: rpc.Branch,
		}); err != nil {
			return fail(err.Error())
		}
		return Reply{OK: true}
	}
	// Hosted cockpit remote control (prompt/permission/cancel/resume/subscribe).
	// Gated per-project by RemotePolicy.Cockpit before touching the session.
	if strings.HasPrefix(rpc.Type, "cockpit.") {
		if rpc.Project == "" {
			return fail("rpc missing project")
		}
		if d.Cockpit == nil {
			return fail("remote cockpit not enabled for this machine")
		}
		if d.CockpitPolicy == nil {
			return fail("remote cockpit policy not configured for this machine")
		}
		allowed, err := d.CockpitPolicy(rpc.Project)
		if err != nil {
			return fail(fmt.Sprintf("cockpit policy: %v", err))
		}
		if !allowed {
			return fail("remote cockpit not enabled for this project (see .pact/remote.json)")
		}
		switch rpc.Type {
		case "cockpit.prompt":
			if rpc.Seat == "" || rpc.Text == "" {
				return fail("cockpit.prompt requires seat and text")
			}
			if err := d.Cockpit.Prompt(rpc.Project, rpc.Seat, rpc.Text); err != nil {
				return fail(err.Error())
			}
		case "cockpit.permission":
			if rpc.Seat == "" || rpc.RequestID == "" || rpc.Decision == "" {
				return fail("cockpit.permission requires seat, requestId and decision")
			}
			if err := d.Cockpit.Permission(rpc.Project, rpc.Seat, rpc.RequestID, rpc.Decision); err != nil {
				return fail(err.Error())
			}
		case "cockpit.cancel":
			if rpc.Seat == "" {
				return fail("cockpit.cancel requires seat")
			}
			if err := d.Cockpit.Cancel(rpc.Project, rpc.Seat); err != nil {
				return fail(err.Error())
			}
		case "cockpit.resume":
			if rpc.Seat == "" {
				return fail("cockpit.resume requires seat")
			}
			if err := d.Cockpit.Resume(rpc.Project, rpc.Seat); err != nil {
				return fail(err.Error())
			}
		case "cockpit.subscribe":
			if rpc.Seat == "" {
				return fail("cockpit.subscribe requires seat")
			}
			if err := d.Cockpit.Subscribe(rpc.Project, rpc.Seat); err != nil {
				return fail(err.Error())
			}
		default:
			return fail(fmt.Sprintf("unknown rpc type %q", rpc.Type))
		}
		return Reply{OK: true}
	}
	if d.Resolve == nil {
		return fail("dispatcher has no project resolver")
	}
	eng, err := d.Resolve(rpc.Project)
	if err != nil {
		return fail(fmt.Sprintf("resolve project %q: %v", rpc.Project, err))
	}
	if eng == nil {
		return fail(fmt.Sprintf("resolve project %q: nil engine", rpc.Project))
	}

	switch rpc.Type {
	case "pact.assign":
		err = eng.Assign(rpc.Task, rpc.Feature, rpc.Branch, rpc.Owner, rpc.Reviewer, rpc.Spec, rpc.Deps)
	case "pact.accept":
		err = eng.Accept(rpc.Task)
	case "pact.changes":
		err = eng.Changes(rpc.Task, rpc.Reason)
	case "pact.merge":
		err = eng.Merge(rpc.Feature)
	case "pact.checkpoint":
		err = eng.Checkpoint(rpc.Task, rpc.Evidence)
	default:
		return fail(fmt.Sprintf("unknown rpc type %q", rpc.Type))
	}
	if err != nil {
		return fail(err.Error())
	}
	return Reply{OK: true}
}

func fail(msg string) Reply { return Reply{OK: false, Error: msg} }

// projectEngine adapts *pact.Project to PactEngine (compile-time assertion that
// the real engine satisfies the interface this package drives).
var _ PactEngine = (*pact.Project)(nil)
