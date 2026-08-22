package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	// StreamDir is where the per-task live stream is mirrored (dashboard-observable).
	// "" falls back to RepoDir. A sandbox run sets it to the MAIN dir while RepoDir
	// is the worktree, so serve tails a path that survives worktree teardown.
	StreamDir string
	// Effort is the reasoning-effort budget for this stint, derived from the
	// task's tier by the driver (execution-tiering §4.5). "" = no effort
	// injection (launch byte-identical to pre-tiering behavior).
	Effort string
}

// streamDir is StreamDir when set, else RepoDir.
func (lc LaunchContext) streamDir() string {
	if lc.StreamDir != "" {
		return lc.StreamDir
	}
	return lc.RepoDir
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
	cmd.Env = append(filteredEnviron(), env...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = teeStdout(capture)
	cmd.Stderr = os.Stderr
	// TODO(phase0-h7): osExec currently does NOT place the child in its own
	// process group. Adding cmd.Cancel without also adding Setpgid would make
	// killGroup send SIGKILL to the parent's process group, so this path is
	// intentionally left unchanged. The default cmd transport (osExecIdle) has
	// the group-kill fix; revisit here only if the plain runner is restored.
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
	// per-agent override (model / scoped permissions) from the machine registry,
	// plus the tier-derived effort budget (a bound seat's explicit profile
	// effort, when set, wins over lc.Effort inside ResolveSeat).
	eff, ok := agentcfg.ResolveSeat(lc.Seat, lc.Kind, lc.Effort)
	if !ok {
		return fmt.Errorf("orchestrate: kind %q 无 headless runner，改用 CLI 座席或人工那一棒", lc.Kind)
	}

	args := make([]string, len(eff.Args))
	copy(args, eff.Args)

	// codex retry-resume: if a previous successful stint recorded a thread id for
	// this (seat,task), switch from a cold `exec` to `exec resume <id>`.
	// antigravity retry-resume: same idea, isomorphic mechanic — agy's headless
	// JSON carries conversation_id where codex's carries thread_id, and agy takes
	// a `--conversation <id>` flag rather than a `resume` subcommand (agy-resume).
	resumed := false
	switch lc.Kind {
	case "codex-cli":
		args, resumed = codexResumeArgsIfAny(lc, args)
	case "antigravity":
		args, resumed = agyResumeArgsIfAny(lc, args)
	}

	for i, a := range args {
		switch a {
		case briefingPlaceholder:
			args[i] = lc.Briefing
		case "{seat}":
			// A custom-agent manifest with identity.via=arg carries {seat} in its
			// argv; substitute the acting seat id at exec (built-in kinds never emit it).
			args[i] = lc.Seat
		case "{repoDir}":
			// A kind whose CLI needs the workspace passed explicitly (antigravity/agy
			// uses --add-dir; without it the agent writes into its own scratch dir)
			// carries this token in its argv; substitute the stint's repo at exec.
			args[i] = lc.RepoDir
		}
	}

	// opencode session tagging: stamp each run with a per-seat title so the driver
	// can find and delete exactly this seat's sessions once the task is accepted
	// (session cleanup). No format change — --title is just metadata on the run.
	args = tagOpencodeSession(eff.Command, lc.Seat, args)

	// Inject the seat id so the launched agent joins as the right seat, plus the
	// task/project so the audit PreToolUse hook can attribute each tool call. The
	// production execFn appends these onto os.Environ(); test execFns assert them.
	//
	// PACT_DIR pins the worker's pact (its checkpoint via the cwd-bound MCP server)
	// to the DRIVER's worktree, regardless of where the agent's MCP server resolves
	// its own cwd/project root. Absolute, so paths.Dir + pact.At honor it over cwd.
	// Without this, an agent (notably opencode, whose MCP is project-scoped and
	// resolves the root independently) checkpoints into a divergent .pact and its
	// commit never reaches the driver — the worker "delivers" but the branch stays
	// empty and the driver escalates "failure limit / no checkpoint evidence".
	pactDir := filepath.Join(lc.RepoDir, ".pact")
	if abs, aerr := filepath.Abs(pactDir); aerr == nil {
		pactDir = abs
	}
	env := []string{
		"PACT_AGENT_ID=" + lc.Seat,
		"PACT_TASK_ID=" + lc.Task,
		"PACT_PROJECT=" + lc.Project,
		"PACT_DIR=" + pactDir,
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

	// gemini hooks (audit capture) only load from a TRUSTED workspace. The
	// launch args use --skip-trust, which merely skips the trust dialog while
	// still treating the folder as untrusted — project .gemini/settings.json
	// hooks then register as "0 hook entries" and the audit BeforeTool hook
	// never fires (verified live, gemini 0.49.0). GEMINI_CLI_TRUST_WORKSPACE is
	// the documented headless trust grant; the driver already fully trusts the
	// repo it is orchestrating.
	if eff.Command == "gemini" {
		env = append(env, "GEMINI_CLI_TRUST_WORKSPACE=true")
	}

	// Isolate a single-vendor agent from sibling vendors' credentials (M11): a
	// kimi / gemini / codex worker must not inherit the user's ANTHROPIC key, etc.
	// Each kind keeps its OWN key (gemini's is added just above); model-agnostic
	// kinds (opencode, custom) are left untouched.
	env = append(env, crossVendorStrip(lc.Kind)...)

	// Siphon a bounded tail of the child's stdout so we can parse token usage from
	// its headless JSON (the read side surfaces it on the dashboard). Best-effort
	// telemetry: capture and recording never affect the run's result.
	cap := &tailWriter{max: tokenCaptureCap}
	// A HEAD window as well as the tail. Every JSONL kind (codex/opencode/claude)
	// puts its usage on the LAST line, so a tail is sufficient for them. agy emits
	// ONE object whose leading fields are conversation_id and status and whose
	// trailing field is usage — so a >1 MiB response truncates the tail's opening
	// brace away and loses both the resume id and the status, silently (no tokens
	// recorded, resume never engages). 8 KiB is far more than those leading fields
	// need and is negligible next to the 1 MiB tail.
	headCap := &headWriter{max: headCaptureCap}
	// Mirror this stint's stdout to the per-task live stream (best-effort: a sink
	// error just means no live mirror this run, never a run failure).
	var capture io.Writer = io.MultiWriter(cap, headCap)
	if lc.Task != "" {
		if sink, serr := OpenStreamSink(lc.streamDir(), lc.Task); serr == nil {
			defer sink.Close()
			// bestEffortWriter is REQUIRED here: io.MultiWriter aborts the whole
			// write on any sub-writer error, which would propagate up through the
			// child's stdout pipe and stall (hang) the agent. Swallowing the sink's
			// errors keeps the live mirror truly best-effort — a failing stream file
			// never affects the run.
			capture = io.MultiWriter(cap, headCap, bestEffortWriter{sink})
		}
	}
	err = r.Exec(ctx, eff.Command, args, lc.RepoDir, env, capture)
	recordTokens(lc, cap.String())

	// codex session lifecycle: capture the thread id on a successful cold run so a
	// future retry can resume; if a resume attempt itself fails, delete the stale
	// record so the next retry falls back to a cold start (self-healing).
	if lc.Kind == "codex-cli" && lc.Task != "" {
		if err != nil && resumed {
			_ = RemoveSession(lc.RepoDir, lc.Seat, lc.Task, "codex-cli")
		} else if err == nil {
			if id := parseCodexThreadID(cap.String()); id != "" {
				_ = RecordSession(lc.RepoDir, lc.Seat, lc.Task, "codex-cli", id)
			}
		}
	}

	// antigravity session lifecycle (agy-resume): reuses the SAME store as the
	// ACP path (LookupSession/RecordSession/ClearSession, keyed by (seat,task)
	// regardless of kind) per spec, rather than codex's kind-filtered
	// LoadSessions/RemoveSession loop above.
	//
	// On success, ALWAYS re-record whatever conversation_id THIS stint actually
	// reports (not conditionally, unlike codex — agy's result JSON carries
	// conversation_id on every SUCCESS, cold or resumed; codex only emits
	// thread.started on a cold run). This is what makes a rejected resume
	// self-heal without any special-case detection: live-verified 2026-08-22, a
	// stale/unknown --conversation id does NOT fail the agy process — agy prints
	// a `warning: ... not found` line to stderr (invisible to this stdout-only
	// capture) and transparently mints a fresh conversation, still exiting
	// 0/SUCCESS. Re-recording that fresh id overwrites the stale one — the
	// store's record of "cold start" for next time — while THIS stint never
	// fails because of it (mirrors the ACP path's "LoadSession fails →
	// ClearSession then NewSession, stint still succeeds" shape, just landing on
	// agy's actual self-healing behavior instead of a synchronous in-process
	// retry, since a one-shot CLI process has no separate load-then-prompt step
	// to intercept).
	//
	// The err!=nil branch below is defensive symmetry with codex's pattern for a
	// resumed run that hard-fails — NOT reproduced against the real binary (every
	// --conversation rejection observed live fell back silently, never a
	// non-zero exit); kept so a future agy hard-failure mode still self-heals the
	// store instead of wedging every subsequent retry on a now-dead id.
	//
	// Cleanup uses kind-checked RemoveSession, not kind-blind ClearSession: the
	// store is shared with codex-cli and the ACP path, and an agy failure must not
	// delete another kind's record for the same (seat,task) — see LookupSessionKind.
	if lc.Kind == "antigravity" && lc.Task != "" {
		// agy can exit 0 while reporting {"status":"ERROR"} — verified live
		// 2026-08-22, a mid-run tool rejection does exactly that, while an
		// argv-validation failure (--model/--effort conflict) exits 1 with the same
		// status. So the exit code alone does not tell us the run was healthy.
		//
		// This deliberately does NOT convert status=ERROR into a stint failure. It
		// was written that way first and the live e2e proved it wrong: in the
		// observed ERROR runs agy had ALREADY delivered — marker file written,
		// `pactify checkpoint` executed — because it recovered from the rejected
		// tool call via a shell command and only then reported ERROR. Failing the
		// stint would invent a failure for a seat that did its job. Whether a stint
		// delivered is the LOOP's question, answered from the ledger (the v0.8.1
		// "clean exit, nothing committed" attribution), not the runner's to guess
		// from a vendor status string.
		//
		// What the status IS used for: gating the resume record. Re-entering a
		// conversation that ended in an error state is not something we have any
		// evidence works, so a non-SUCCESS run starts cold next time. Tokens stay
		// recorded either way — they were really spent.
		healthy := true
		if st, ok := parseAntigravityStatus(headCap.String()); ok && st != "SUCCESS" {
			healthy = false
		}
		switch {
		case err != nil && resumed:
			_ = RemoveSession(lc.RepoDir, lc.Seat, lc.Task, "antigravity")
		case err == nil && !healthy && resumed:
			// The resumed conversation is the thing that errored — drop it so the
			// next attempt cold-starts rather than re-entering the same bad state.
			_ = RemoveSession(lc.RepoDir, lc.Seat, lc.Task, "antigravity")
		case err == nil && healthy:
			// conversation_id is the FIRST field of agy's single result object, so it
			// is read from the HEAD capture: the tail window alone loses it whenever
			// the object exceeds tokenCaptureCap (agy emits one object, not JSONL).
			if id, ok := parseAntigravityConversationID(headCap.String()); ok {
				_ = RecordSession(lc.RepoDir, lc.Seat, lc.Task, "antigravity", id)
			}
		}
	}

	return err
}

// tokenCaptureCap bounds the stdout tail kept for token parsing — enough to hold
// an agent's final usage JSON (a claude result object or the last stream-json
// line) without buffering an entire long run. Usage lives at the END of JSONL
// output, so a tail is the right window.
const tokenCaptureCap = 1 << 20 // 1 MiB

// headCaptureCap bounds the stdout HEAD kept for agy's leading result fields
// (conversation_id, status). See the headWriter splice in Run for why a tail
// alone is not sufficient for a single-object emitter.
const headCaptureCap = 8 << 10 // 8 KiB

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
	n, ok := parseTokenUsage(lc.Kind, output)
	if !ok || n <= 0 {
		return
	}
	// streamDir, not RepoDir: in a sandbox run RepoDir is the throwaway worktree
	// and a store written there dies at teardown (2026-07-19 e2e F6). Tokens are
	// teardown-surviving runtime, same as status/streams/escalation.
	recordTaskTokens(lc.streamDir(), lc.Task, n)
}

// parseTokenUsage dispatches to the kind-specific stdout token parser. antigravity
// (agy) gets its own path (parseAntigravityTokens, agy_tokens.go) because agy's
// usage shape is not a documented/versioned contract pactify controls —
// tokens.Parse's generic input+output-sum heuristic could silently diverge from
// the real usage.total_tokens the moment agy's shape changes, so agy reads
// total_tokens verbatim instead (dm-agy-tokens). Every other kind is untouched:
// same tokens.Parse call as before, so no behavior change for codex/claude/opencode/etc.
func parseTokenUsage(kind, output string) (int, bool) {
	if kind == "antigravity" {
		return parseAntigravityTokens(output)
	}
	return tokens.Parse(kind, output)
}

// recordTaskTokens accumulates n tokens for task into the repo's token store
// (internal/tokens at .pact/orchestrate/tokens.json). Shared by the CmdRunner
// stdout-parse path (recordTokens) and the ACP usage path (recordAcpUsage) so both
// transports write the SAME on-disk store, keyed by task. Best-effort: an empty
// task, non-positive count, or a write error are all silent no-ops.
func recordTaskTokens(repoDir, task string, n int) {
	if task == "" || n <= 0 {
		return
	}
	s := tokens.Load(repoDir)
	s.Add(task, n)
	_ = tokens.Save(repoDir, s)
}

// parseCodexThreadID scans codex's JSONL stdout for the first
// `{"type":"thread.started","thread_id":"..."}` line and returns the thread id.
// The headless `codex exec --json` run emits this on its first line, but the
// scanner is tolerant of leading/trailing noise.
func parseCodexThreadID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type == "thread.started" && ev.ThreadID != "" {
			return ev.ThreadID
		}
	}
	return ""
}

// codexResumeArgsIfAny returns resume-form argv when a prior codex session for
// (seat,task) is stored; otherwise it returns base unchanged. The bool reports
// whether a resume was selected.
func codexResumeArgsIfAny(lc LaunchContext, base []string) ([]string, bool) {
	if lc.Task == "" {
		return base, false
	}
	recs, err := LoadSessions(lc.RepoDir)
	if err != nil {
		return base, false
	}
	for _, r := range recs {
		if r.Seat == lc.Seat && r.Task == lc.Task && r.Kind == "codex-cli" && r.SessionID != "" {
			return codexResumeArgs(base, r.SessionID), true
		}
	}
	return base, false
}

// codexResumeArgs converts a cold `codex exec ...` argv into a resume argv. The
// `resume` subcommand does not accept --sandbox (the session already carries the
// original policy), so that flag and its value are stripped; --json and -m are
// preserved.
func codexResumeArgs(base []string, threadID string) []string {
	if len(base) == 0 || base[0] != "exec" {
		// Defensive fallback for an unexpected base shape.
		return append([]string{"resume", threadID}, base...)
	}
	out := make([]string, 0, len(base)+2)
	out = append(out, "exec", "resume", threadID)
	for i := 1; i < len(base); i++ {
		a := base[i]
		if a == "--sandbox" {
			if i+1 < len(base) {
				i++
			}
			continue
		}
		if strings.HasPrefix(a, "--sandbox=") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// agyResumeArgsIfAny returns argv with `--conversation <id>` appended when a
// prior agy session for (seat,task) is stored; otherwise base unchanged. The
// bool reports whether a resume was selected.
//
// This reuses the shared session store (rather than rolling a codex-shaped
// duplicate) per the task spec, but goes through the kind-CHECKED
// LookupSessionKind rather than the kind-blind LookupSession the spec named.
// The spec's instruction predates the observation that (seat,task) alone is not
// a sufficient key: the store is shared with codex-cli and the ACP path, and a
// seat can change kind mid-feature (dynamic `join --kind`, `--seat-kind`, a
// fallback-role switch). Kind-blind, an agy stint would then be handed a codex
// thread_id as its --conversation. Isolation across (seat,task) pairs was never
// the exposure — kind was.
func agyResumeArgsIfAny(lc LaunchContext, base []string) ([]string, bool) {
	if lc.Task == "" {
		return base, false
	}
	id, ok := LookupSessionKind(lc.RepoDir, lc.Seat, lc.Task, "antigravity")
	if !ok {
		return base, false
	}
	out := make([]string, 0, len(base)+2)
	out = append(out, base...)
	out = append(out, "--conversation", id)
	return out, true
}

// headWriter keeps only the FIRST max bytes written to it and discards the rest.
// Counterpart to tailWriter for a kind whose result object leads with the fields
// we need (agy: conversation_id, status) — those are unrecoverable from a tail
// window once the object outgrows it.
type headWriter struct {
	max int
	buf []byte
}

func (w *headWriter) Write(p []byte) (int, error) {
	if n := w.max - len(w.buf); n > 0 {
		if n > len(p) {
			n = len(p)
		}
		w.buf = append(w.buf, p[:n]...)
	}
	return len(p), nil
}

func (w *headWriter) String() string { return string(w.buf) }

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
