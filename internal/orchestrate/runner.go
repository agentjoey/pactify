package orchestrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentcfg"
	"github.com/agentjoey/pactify/internal/secret"
	"github.com/agentjoey/pactify/internal/sessions"
)

// glmDefaultBaseURL is the GLM Coding Plan's GLOBAL Anthropic-compatible endpoint.
// The CHINA plan uses a different host (open.bigmodel.cn), so the endpoint is
// overridable via the Keychain (see glmBaseURL). A claude-code seat whose
// effective model is a `glm-*` model runs against this endpoint with a
// Keychain-sourced token (GLM is not a separate kind — it's claude-code pointed
// at the GLM endpoint; see docs/agent-integration-candidates.md).
const glmDefaultBaseURL = "https://api.z.ai/api/anthropic"

// glmToken is overridable in tests; production reads the Keychain.
var glmToken = secret.GLMToken

// glmBaseURL resolves the GLM endpoint: a Keychain override (service "pactify",
// account "glm-base-url") if set and non-empty — e.g. the china coding plan's
// https://open.bigmodel.cn/api/anthropic — else the global default. Overridable
// in tests. Never errors: a missing override silently falls back to the default.
var glmBaseURL = func() string {
	if v, err := secret.Keychain("pactify", "glm-base-url"); err == nil {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return glmDefaultBaseURL
}

// briefingPlaceholder is the literal token the resolved Args carry where the real
// briefing text must be substituted (e.g. opencode → ["run","-m",model,"{briefing}"]).
// Kept equal to agentcfg.Placeholder, the token agentcfg.Resolve emits.
const briefingPlaceholder = agentcfg.Placeholder

// LaunchContext carries everything needed to launch one agent stint. It replaces
// the former loose (seatID, kind, briefing, repoDir) params so audit attribution
// (Task, Project) — stamped into the agent's env for the audit hook — and future
// fields add without churning the signature.
type LaunchContext struct {
	Seat, Kind, Task, Project, Briefing, RepoDir string
}

// Runner headless-launches a seat's agent for one stint, blocking until that turn
// ends. Seat + Kind identify the agent (the loop maps seat id → kind from the
// roster); Briefing is the prompt; RepoDir is the working directory; Task/Project
// are stamped into the child env (PACT_TASK_ID/PACT_PROJECT) for audit.
type Runner interface {
	Run(ctx context.Context, lc LaunchContext) error
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
func (r CmdRunner) Run(ctx context.Context, lc LaunchContext) error {
	if _, known := agent.Get(lc.Kind); !known {
		return fmt.Errorf("orchestrate: unknown agent kind %q — 改用 CLI 座席或人工那一棒", lc.Kind)
	}
	// Resolve the effective launch config: built-in profile overlaid with any
	// per-agent override (model / scoped permissions) from the machine registry.
	eff, ok := agentcfg.Resolve(lc.Kind)
	if !ok {
		return fmt.Errorf("orchestrate: kind %q 无 headless runner，改用 CLI 座席或人工那一棒", lc.Kind)
	}

	args := make([]string, len(eff.Args))
	for i, a := range eff.Args {
		if a == briefingPlaceholder {
			args[i] = lc.Briefing
		} else {
			args[i] = a
		}
	}

	// opencode session tagging: stamp each run with a per-seat title so the driver
	// can find and delete exactly this seat's sessions once the task is accepted
	// (session cleanup). No format change — --title is just metadata on the run.
	args = tagOpencodeSession(eff.Command, lc.Seat, args)

	// Inject the seat id so the launched agent joins as the right seat, plus the
	// task/project so the audit PreToolUse hook can attribute each tool call. The
	// production execFn appends these onto os.Environ(); test execFns assert them.
	env := []string{
		"PACT_AGENT_ID=" + lc.Seat,
		"PACT_TASK_ID=" + lc.Task,
		"PACT_PROJECT=" + lc.Project,
	}

	// GLM: a claude-code seat on a glm-* model runs against the Z.ai endpoint with
	// a Keychain-sourced token (no plaintext). Not a new kind — claude-code pointed
	// elsewhere. Fail closed with an actionable error if the token is missing.
	gEnv, err := glmEnv(eff.Command, eff.Model)
	if err != nil {
		return fmt.Errorf("orchestrate: %w", err)
	}
	env = append(env, gEnv...)
	return r.Exec(ctx, eff.Command, args, lc.RepoDir, env)
}

// tagOpencodeSession inserts `--title pact:<seat>` right after opencode's "run"
// subcommand (so it's a flag on the run, not swallowed by the trailing
// [message..] positional). No-op for any other command, or if the arg shape is
// unexpected. The title lets sessions.CleanupByTitle find this seat's sessions.
func tagOpencodeSession(command, seatID string, args []string) []string {
	if command != "opencode" || len(args) == 0 || args[0] != "run" {
		return args
	}
	tagged := []string{args[0], "--title", sessions.SessionTag(seatID)}
	return append(tagged, args[1:]...)
}

// glmEnv returns the Z.ai endpoint env (base URL + Keychain token) for a
// claude-code seat on a glm-* model; (nil, nil) for any other command/model. It
// errors only when the seat IS a GLM seat but the token is missing.
func glmEnv(command, model string) ([]string, error) {
	if command != "claude" || !strings.HasPrefix(model, "glm") {
		return nil, nil
	}
	tok, err := glmToken()
	if err != nil {
		return nil, fmt.Errorf("GLM model %q on claude-code needs a Z.ai token — %w", model, err)
	}
	return []string{"ANTHROPIC_BASE_URL=" + glmBaseURL(), "ANTHROPIC_AUTH_TOKEN=" + tok}, nil
}
