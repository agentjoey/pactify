package orchestrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentcfg"
)

// briefingPlaceholder is the literal token the resolved Args carry where the real
// briefing text must be substituted (e.g. opencode → ["run","-m",model,"{briefing}"]).
// Kept equal to agentcfg.Placeholder, the token agentcfg.Resolve emits.
const briefingPlaceholder = agentcfg.Placeholder

// Runner headless-launches a seat's agent with a briefing, blocking until that
// turn ends. seatID + kind identify the agent (the orchestrate loop holds a
// projection.State whose Seat has no Kind field, so the loop maps seat id → kind
// from the roster and passes both explicitly); briefing is the prompt; repoDir is
// the working directory the agent runs in.
type Runner interface {
	Run(ctx context.Context, seatID, kind, briefing, repoDir string) error
}

// execFn abstracts process spawn so the production path (os/exec) and the test
// fakes share one seam — keeping test stubs out of production code (spec §7). env
// is the full child environment; dir is the working directory.
type execFn func(ctx context.Context, name string, args []string, dir string, env []string) error

// CmdRunner is the production Runner: it resolves agent.Get(kind).Runner(),
// substitutes the {briefing} placeholder, injects PACT_AGENT_ID=seatID, and execs
// via its Exec seam.
type CmdRunner struct{ Exec execFn }

// NewCmdRunner returns a CmdRunner wired to the real os/exec-backed execFn. When
// idle>0 the execFn kills a child that produces no output for that long (errIdle
// → soft failure → worker retry); idle<=0 keeps the plain run-to-completion
// behavior. The production execFn builds the child environment from os.Environ()
// plus the caller-supplied additions (so PACT_AGENT_ID is appended on top of the
// inherited env) and streams stdio to the parent.
func NewCmdRunner(idle time.Duration) CmdRunner {
	return CmdRunner{Exec: osExecIdle(idle)}
}

// osExec is the production execFn: it merges the inherited process environment
// with the supplied env, then runs the command to completion in dir.
func osExec(ctx context.Context, name string, args []string, dir string, env []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Run resolves the headless runner for kind, substitutes the briefing, and execs
// it in repoDir with PACT_AGENT_ID=seatID injected so the agent joins the right
// seat. A kind with no headless runner (GUI/desktop kinds, or an unknown kind)
// fails closed: no process is spawned and an actionable error is returned.
func (r CmdRunner) Run(ctx context.Context, seatID, kind, briefing, repoDir string) error {
	if _, known := agent.Get(kind); !known {
		return fmt.Errorf("orchestrate: unknown agent kind %q — 改用 CLI 座席或人工那一棒", kind)
	}
	// Resolve the effective launch config: built-in profile overlaid with any
	// per-agent override (model / scoped permissions) from the machine registry.
	eff, ok := agentcfg.Resolve(kind)
	if !ok {
		return fmt.Errorf("orchestrate: kind %q 无 headless runner，改用 CLI 座席或人工那一棒", kind)
	}

	args := make([]string, len(eff.Args))
	for i, a := range eff.Args {
		if a == briefingPlaceholder {
			args[i] = briefing
		} else {
			args[i] = a
		}
	}

	// Inject the seat id so the launched agent joins as the right seat. The seam
	// passes this as an addition; the production execFn appends it onto
	// os.Environ(), while test execFns assert it is present.
	env := []string{"PACT_AGENT_ID=" + seatID}
	return r.Exec(ctx, eff.Command, args, repoDir, env)
}
