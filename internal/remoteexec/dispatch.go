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

	"github.com/agentjoey/pactify/internal/pact"
)

// RPC is one pact command couriered from a remote control plane (hosted web, or
// another machine) via the relay. Only pactify's own command surface — never a
// linx rpc type. Args carry the verb parameters (task/feature/branch/owner/
// reviewer/spec/reason/evidence/deps) so the wire shape stays a flat, bounded map.
type RPC struct {
	Type    string   // e.g. "pact.assign" (see the switch in Handle for the set)
	Account string   // account scope of the caller — must match this machine's
	Project string   // registered project name the verb targets
	Task    string   // task id (assign/accept/changes/checkpoint)
	Feature string   // feature id (assign/merge)
	Branch  string   // feature branch (assign)
	Owner   string   // task owner seat (assign)
	Reviewer string  // task reviewer seat (assign)
	Spec    string   // task spec path (assign)
	Reason  string   // changes reason
	Evidence string  // checkpoint evidence
	Deps    []string // assign dependencies
}

// Reply is the executor's result, couriered back to the caller. Errors are
// operational strings (never leak internals); OK=false always carries Error.
type Reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
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
	// Resolve maps rpc.Project → engine. Required.
	Resolve Resolver
}

// Handle executes one rpc and returns a Reply. It never panics on bad input:
// unknown types, scope mismatches, unknown projects, and verb errors all become
// Reply{OK:false, Error:...}.
func (d *Dispatcher) Handle(rpc RPC) Reply {
	if d.Resolve == nil {
		return fail("dispatcher has no project resolver")
	}
	// Auth scope: an empty machine account accepts nothing (fail closed).
	if d.Account == "" || rpc.Account != d.Account {
		return fail(ErrAccountScope.Error())
	}
	if rpc.Project == "" {
		return fail("rpc missing project")
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
