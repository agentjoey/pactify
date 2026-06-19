package orchestrate

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentcfg"
	"github.com/agentjoey/pactify/internal/secret"
	"github.com/agentjoey/pactify/internal/sessions"
	"github.com/agentjoey/pactify/internal/tokens"
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
// is the full child environment; dir is the working directory. capture (when
// non-nil) receives a copy of the child's stdout so the caller can parse token
// usage from the agent's headless JSON; the child's stdio still streams to the
// parent unchanged.
type execFn func(ctx context.Context, name string, args []string, dir string, env []string, capture io.Writer) error

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
// with the supplied env, then runs the command to completion in dir. When capture
// is non-nil the child's stdout is tee'd to it (for token parsing) while still
// streaming to the parent.
func osExec(ctx context.Context, name string, args []string, dir string, env []string, capture io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = teeStdout(capture)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// teeStdout returns the writer the child's stdout should target: os.Stdout alone
// when capture is nil, else a MultiWriter that also feeds the capture sink. Keeps
// the live parent stream intact while siphoning a copy for token parsing.
func teeStdout(capture io.Writer) io.Writer {
	if capture == nil {
		return os.Stdout
	}
	return io.MultiWriter(os.Stdout, capture)
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
		switch a {
		case briefingPlaceholder:
			args[i] = lc.Briefing
		case "{seat}":
			// A custom-agent manifest with identity.via=arg carries {seat} in its
			// argv; substitute the acting seat id at exec (built-in kinds never emit it).
			args[i] = lc.Seat
		default:
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

	// gemini: a Keychain API key (pactify/gemini) switches gemini-cli to the
	// API-key auth tier, which honors the -m model pin. The free oauth-personal
	// tier silently falls back to flash on quota (FLASH_FALLBACK only mounts under
	// oauth/compute-default creds), making the pin unreliable. Inert when no key is
	// set — the seat keeps its current auth.
	env = append(env, geminiEnv(eff.Command)...)

	// Siphon a bounded tail of the child's stdout so we can parse token usage from
	// its headless JSON (the read side surfaces it on the dashboard). Best-effort
	// telemetry: capture and recording never affect the run's result.
	cap := &tailWriter{max: tokenCaptureCap}
	// Mirror this stint's stdout to the per-task live stream (best-effort: a sink
	// error just means no live mirror this run, never a run failure).
	var capture io.Writer = cap
	if lc.Task != "" {
		if sink, serr := OpenStreamSink(lc.RepoDir, lc.Task); serr == nil {
			defer sink.Close()
			// bestEffortWriter is REQUIRED here: io.MultiWriter aborts the whole
			// write on any sub-writer error, which would propagate up through the
			// child's stdout pipe and stall (hang) the agent. Swallowing the sink's
			// errors keeps the live mirror truly best-effort — a failing stream file
			// never affects the run.
			capture = io.MultiWriter(cap, bestEffortWriter{sink})
		}
	}
	err = r.Exec(ctx, eff.Command, args, lc.RepoDir, env, capture)
	recordTokens(lc, cap.String())
	return err
}

// tokenCaptureCap bounds the stdout tail kept for token parsing — enough to hold
// an agent's final usage JSON (a claude result object or the last stream-json
// line) without buffering an entire long run. Usage lives at the END of JSONL
// output, so a tail is the right window.
const tokenCaptureCap = 1 << 20 // 1 MiB

// recordTokens parses token usage from an agent stint's captured stdout and
// accumulates it into the repo's token store, keyed by task. Pure best-effort: a
// missing task, unparseable output, or a write error are all silent no-ops — token
// telemetry must never break a run. Concurrency-safe by construction: the serial
// loop runs one stint at a time, and parallel features each run in their own
// worktree (distinct RepoDir), so no two callers touch the same tokens.json.
func recordTokens(lc LaunchContext, output string) {
	if lc.Task == "" {
		return
	}
	n, ok := tokens.Parse(lc.Kind, output)
	if !ok || n <= 0 {
		return
	}
	s := tokens.Load(lc.RepoDir)
	s.Add(lc.Task, n)
	_ = tokens.Save(lc.RepoDir, s)
}

// tailWriter keeps only the last max bytes written to it. Splicing it into the
// child's stdout bounds memory regardless of how chatty an agent is, while
// preserving the tail where the final usage JSON lives.
type tailWriter struct {
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		w.buf = w.buf[len(w.buf)-w.max:]
	}
	return len(p), nil
}

func (w *tailWriter) String() string { return string(w.buf) }

// bestEffortWriter forwards writes to w but always reports success, so a failing
// or slow live-stream sink can never abort an io.MultiWriter write and stall the
// child process's stdout (which would hang the agent). The mirror is telemetry —
// it must never affect the run.
type bestEffortWriter struct{ w io.Writer }

func (b bestEffortWriter) Write(p []byte) (int, error) {
	_, _ = b.w.Write(p)
	return len(p), nil
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

// geminiKey is overridable in tests; production reads the Keychain (pactify/gemini).
var geminiKey = func() (string, error) { return secret.Keychain("pactify", "gemini") }

// geminiEnv returns GEMINI_API_KEY env for a gemini-cli seat when a Keychain key
// is set (switching it to the API-key auth tier so the model pin holds); nil for
// any other command or when no key is configured. Never errors — a missing key is
// a no-op (the seat keeps its existing auth), unlike GLM where the token is
// required once the seat is GLM.
func geminiEnv(command string) []string {
	if command != "gemini" {
		return nil
	}
	if k, err := geminiKey(); err == nil {
		if k = strings.TrimSpace(k); k != "" {
			return []string{"GEMINI_API_KEY=" + k}
		}
	}
	return nil
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
