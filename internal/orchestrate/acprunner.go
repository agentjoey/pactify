package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/agentcfg"
	"github.com/agentjoey/pactify/internal/audit"
)

// resumeBriefingPrefix is prepended to the briefing when a stint resumes a prior
// ACP session (LoadSession succeeded), telling the agent to re-orient off its own
// prior work before continuing rather than restarting from scratch.
const resumeBriefingPrefix = "续接上次会话:先自查上次进度(git log/diff),再继续任务\n\n"

// acpConn is the subset of *acp.Client that AcpRunner drives — the seam tests
// inject a fake through, mirroring CmdRunner's execFn. *acp.Client satisfies it.
type acpConn interface {
	Initialize(ctx context.Context) (acp.InitializeResult, error)
	NewSession(ctx context.Context, cwd string) (acp.SessionID, error)
	LoadSession(ctx context.Context, id acp.SessionID) error
	Prompt(ctx context.Context, sid acp.SessionID, text string) (acp.StopReason, error)
	OnSessionUpdate(fn func(acp.SessionUpdate))
	OnPermissionRequest(fn func(acp.PermissionRequest) acp.PermissionOutcome)
	Close() error
}

// acpSpawnFn spawns an ACP connection to command with args in dir. Production
// wraps acp.Spawn; tests inject an in-process fake so the whole runner path runs
// without a real subprocess or JSON-RPC wire.
type acpSpawnFn func(ctx context.Context, command string, args, env []string, dir string) (acpConn, error)

// PermissionPolicy governs how AcpRunner answers a server's
// session/request_permission — the ACP equivalent of a headless CLI's blanket
// auto-approve (CmdRunner has no permission concept; ACP surfaces each request).
type PermissionPolicy int

const (
	// PermissionAuto auto-selects an allow/approve/yes/once option (the default:
	// the driver is already running headlessly, so it grants routine requests).
	PermissionAuto PermissionPolicy = iota
	// PermissionEscalate writes an escalation record and denies the request, so a
	// human reviews the requested action before it proceeds.
	PermissionEscalate
	// PermissionDeny cancels every request outright (no side effects, no record).
	PermissionDeny
)

// AcpRunner is a Runner that drives a seat's agent over the Agent Client Protocol
// (a stdio JSON-RPC subprocess) instead of a one-shot headless command. Lifecycle
// per stint: Spawn → Initialize → NewSession(RepoDir) → Prompt(Briefing) → wait
// for the stop reason → Close. It idle-kills a silent agent (soft failure → the
// loop retries), answers permission requests per Policy, and surfaces the stint's
// aggregated token usage through OnUsage.
type AcpRunner struct {
	// Spawn opens the ACP connection. nil is a programming error — construct via
	// NewAcpRunner (production) or inject a fake (tests).
	Spawn acpSpawnFn
	// Idle kills an agent that emits NO session update for this long, returning a
	// soft failure (errIdle) so the loop retries the worker — the ACP analogue of
	// CmdRunner's output-idle watchdog. 0 disables the watchdog.
	Idle time.Duration
	// Policy answers session/request_permission (default PermissionAuto).
	Policy PermissionPolicy
	// OnUsage, when set, receives the stint's aggregated token usage keyed by
	// repoDir+seat+task. Production (NewAcpRunner) wires it to recordAcpUsage, which
	// folds the usage into the SAME per-task token store the CmdRunner path writes
	// (internal/tokens at .pact/orchestrate/tokens.json). repoDir is passed because
	// the store is per-worktree (parallel features run in distinct RepoDirs).
	OnUsage func(repoDir, seat, task string, u acp.Usage)
	// Now supplies the escalation-record timestamp; nil defaults to wall clock.
	Now func() string
	// Warn is the operator-facing warning sink, mirroring CmdRunner.Warn: nil
	// falls back to stderr, the same channel escalate/sandbox use so a headless
	// run still records the notice. Currently carries one notice: a role
	// binding's model pin that this transport cannot honor.
	Warn func(string)
}

// warn surfaces an operator-facing notice, nil-safe (injected sink, else stderr).
func (r AcpRunner) warn(msg string) {
	if r.Warn != nil {
		r.Warn(msg)
		return
	}
	fmt.Fprintln(os.Stderr, "orchestrate: "+msg)
}

// acpModelEnv returns the child-environment entries that pin model for kind over
// the ACP transport, and whether this kind has such a channel at all.
//
// [ACP-MODEL] root fix. acpCommand carries no --model for any kind (opencode's
// `acp` subcommand has no model flag at all — checked against opencode 1.18.23),
// so the pin has to travel some other way. For opencode that way is
// OPENCODE_CONFIG_CONTENT, a JSON config injected by env.
//
// Verified end-to-end against a real `opencode acp` server before shipping:
// without it a session runs providerID=deepseek modelID=deepseek-v4-pro (the
// reported bug); with it the same session runs providerID=minimax
// modelID=MiniMax-M3. `opencode debug config` confirms the value MERGES into the
// resolved config (provider/mcp/agent/permission/plugin stay byte-identical), so
// pinning a model cannot clobber the operator's own setup.
func acpModelEnv(kind, model string) ([]string, bool) {
	switch kind {
	case "opencode":
		b, err := json.Marshal(map[string]string{"model": model})
		if err != nil {
			return nil, false
		}
		return []string{"OPENCODE_CONFIG_CONTENT=" + string(b)}, true
	}
	return nil, false
}

// applyModelPin honours this seat's role-binding model pin if the kind has an
// ACP channel for it, and otherwise says out loud that the pin is being dropped.
//
// [ACP-MODEL],坐实于 2026-08-31: the ACP path never calls agentcfg.ResolveSeat*
// and acpCommand emits no --model, so an unpinnable kind runs on its own global
// default. Because opencode DEFAULTS to ACP (DefaultTransportModes), every bound
// opencode seat hit this — a Human Owner read the resulting mismatch as "the
// worker did nothing".
func (r AcpRunner) applyModelPin(lc LaunchContext) []string {
	model, role, ok := agentcfg.SeatModelPin(lc.Seat)
	if !ok {
		return nil
	}
	if env, ok := acpModelEnv(lc.Kind, model); ok {
		return env
	}
	r.warn(fmt.Sprintf(
		"seat %s: role %q pins model %s, but the ACP transport has no way to pass a model to %s — it will run on its own global default. Route this seat over the command transport (--transport %s=cmd) to make the pin take effect.",
		lc.Seat, role, model, lc.Kind, lc.Kind))
	return nil
}

// NewAcpRunner returns an AcpRunner wired to the real acp.Spawn, with the given
// idle watchdog and permission policy.
func NewAcpRunner(idle time.Duration, policy PermissionPolicy) AcpRunner {
	return AcpRunner{
		Spawn: func(ctx context.Context, command string, args, env []string, dir string) (acpConn, error) {
			return acp.Spawn(ctx, command, args, env, dir)
		},
		Idle:    idle,
		Policy:  policy,
		OnUsage: recordAcpUsage,
	}
}

// recordAcpUsage folds an ACP stint's aggregated token usage into the per-task
// token store shared with the CmdRunner path (via recordTaskTokens), keyed by
// task. It sums input+output tokens to match CmdRunner's tokens.Parse total; cost
// is intentionally not recorded (dm-acp-usage tracks token counts only). seat is
// part of the callback's attribution contract but unused by the task-keyed store.
func recordAcpUsage(repoDir, _ /*seat*/, task string, u acp.Usage) {
	recordTaskTokens(repoDir, task, u.InputTokens+u.OutputTokens)
}

// Run drives one ACP stint for lc's seat. A kind with no ACP command mapping fails
// closed with an error pointing the operator at the command transport.
func (r AcpRunner) Run(ctx context.Context, lc LaunchContext) error {
	command, args, ok := acpCommand(lc.Kind)
	if !ok {
		return fmt.Errorf("orchestrate: kind %q has no ACP transport — route it over the command transport with --transport %s=cmd", lc.Kind, lc.Kind)
	}

	// Before the agent starts, not after a turn has already run on the wrong model.
	env := append(acpEnv(lc), r.applyModelPin(lc)...)

	conn, err := r.Spawn(ctx, command, args, env, lc.RepoDir)
	if err != nil {
		return fmt.Errorf("orchestrate: acp spawn %q: %w", lc.Kind, err)
	}
	defer conn.Close()

	// Aggregate this stint's usage across every update that carries it, plus signal
	// activity so the idle watchdog can tell a working agent from a hung one.
	var umu sync.Mutex
	var agg acp.Usage
	activity := make(chan struct{}, 1)
	conn.OnSessionUpdate(func(u acp.SessionUpdate) {
		select {
		case activity <- struct{}{}:
		default:
		}
		if u.Kind == "tool_call" {
			if rec, ok := acpAuditRecord(lc, u, time.Now()); ok {
				_ = audit.Append(rec)
			}
		}
		if u.Usage != nil {
			umu.Lock()
			agg.InputTokens += u.Usage.InputTokens
			agg.OutputTokens += u.Usage.OutputTokens
			agg.Cost += u.Usage.Cost
			umu.Unlock()
		}
	})
	conn.OnPermissionRequest(r.permissionHandler(lc))

	init, err := conn.Initialize(ctx)
	if err != nil {
		return fmt.Errorf("orchestrate: acp initialize %q: %w", lc.Kind, err)
	}

	// Session resume (spec §2): if a prior stint for this (seat,task) recorded a
	// session and the server advertised loadSession, re-attach that context instead
	// of starting cold. On any LoadSession failure (expired/rejected) we drop the
	// stale record and fall through to NewSession — the pre-resume behavior.
	var sid string
	briefing := lc.Briefing
	resumed := false
	if init.LoadSession {
		if old, ok := LookupSession(lc.RepoDir, lc.Seat, lc.Task); ok {
			if lerr := conn.LoadSession(ctx, acp.SessionID(old)); lerr == nil {
				sid = old
				resumed = true
				briefing = resumeBriefingPrefix + lc.Briefing
			} else {
				_ = ClearSession(lc.RepoDir, lc.Seat, lc.Task)
			}
		}
	}
	if !resumed {
		newID, nerr := conn.NewSession(ctx, lc.RepoDir)
		if nerr != nil {
			return fmt.Errorf("orchestrate: acp session %q: %w", lc.Kind, nerr)
		}
		sid = string(newID)
		// Persist the mapping so a later retry of this (seat,task) can resume it.
		// Best-effort: a store write failure must not fail the stint.
		_ = RecordSession(lc.RepoDir, lc.Seat, lc.Task, lc.Kind, sid)
	}

	// The stint prompted: surface its usage on every exit path (success, idle
	// kill, cancellation) so dm-acp-usage sees the tokens spent regardless.
	defer func() {
		if r.OnUsage != nil {
			umu.Lock()
			u := agg
			umu.Unlock()
			// streamDir, not RepoDir: sandbox runs record tokens where they
			// survive teardown (2026-07-19 e2e F6), matching recordTokens (cmd).
			r.OnUsage(lc.streamDir(), lc.Seat, lc.Task, u)
		}
	}()

	// Prompt blocks until the turn ends; run it off-goroutine so the watchdog can
	// kill a silent agent (Close → Prompt errors out).
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, perr := conn.Prompt(ctx, acp.SessionID(sid), briefing)
		done <- result{perr}
	}()

	// killAndReap tears the connection down and waits BOUNDEDLY for the prompt
	// goroutine. The reap must not be `<-done` bare: if the child survives the
	// kill or the transport read wedges, Prompt never returns and Run would hang
	// past its deadline — turning --run-timeout into a no-op (e2e F2's failure
	// shape). After the grace period the goroutine is abandoned; it holds no
	// locks and dies with the process.
	killAndReap := func(err error) error {
		conn.Close() // kill → the pending Prompt unblocks with an error
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		return err
	}
	if r.Idle <= 0 {
		// No idle watchdog — but the ctx deadline (--run-timeout) must still cut
		// the stint short; a bare <-done here ignored cancellation entirely.
		select {
		case res := <-done:
			return res.err
		case <-ctx.Done():
			return killAndReap(ctx.Err())
		}
	}
	timer := time.NewTimer(r.Idle)
	defer timer.Stop()
	for {
		select {
		case res := <-done:
			return res.err
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(r.Idle)
		case <-timer.C:
			return killAndReap(fmt.Errorf("%w: no ACP session update for %s — killed", errIdle, r.Idle))
		case <-ctx.Done():
			return killAndReap(ctx.Err())
		}
	}
}

// permissionHandler returns the OnPermissionRequest callback for r's policy.
func (r AcpRunner) permissionHandler(lc LaunchContext) func(acp.PermissionRequest) acp.PermissionOutcome {
	return func(req acp.PermissionRequest) acp.PermissionOutcome {
		switch r.Policy {
		case PermissionDeny:
			return acp.PermissionOutcome{Cancelled: true}
		case PermissionEscalate:
			r.escalatePermission(lc, req)
			return acp.PermissionOutcome{Cancelled: true}
		default: // PermissionAuto
			if id, ok := autoSelectPermission(req.Options); ok {
				return acp.PermissionOutcome{OptionID: id}
			}
			return acp.PermissionOutcome{Cancelled: true}
		}
	}
}

// escalatePermission records a permission request as an escalation under the
// dashboard-observable dir and (best-effort) never blocks the run — the outcome
// the handler returns denies the request regardless.
func (r AcpRunner) escalatePermission(lc LaunchContext, req acp.PermissionRequest) {
	now := r.Now
	if now == nil {
		now = func() string { return time.Now().Format("20060102-150405") }
	}
	reason := fmt.Sprintf("agent %q requested a permission the driver is configured to escalate (policy=escalate)", lc.Seat)
	evidence := fmt.Sprintf("tool call: %s\noptions: %s", req.ToolCall.Title, describeOptions(req.Options))
	suggestion := "review the requested action; approve it manually and re-run, or set the permission policy to auto to grant routine requests"
	// LaunchContext carries no feature id (task/seat/project-scoped only), so
	// this escalation is filed under its task alone — fine here since a
	// permission request is inherently task-specific already, not the routine
	// which-feature-is-this-about ambiguity the main loop's escalations address.
	_, _ = writeEscalation(lc.streamDir(), now(), "", lc.Task, reason, evidence, suggestion)
}

// acpAuditRecord builds an audit.Record from an ACP session/update whose Kind is
// "tool_call". It returns false for tool_call_update and any other non-auditable
// update. Raw is best-effort parsed: title feeds Summary, kind/title feed the tool
// name and risk classification, and rawInput.command forces "exec" risk. Summary
// never carries rawInput contents.
func acpAuditRecord(lc LaunchContext, u acp.SessionUpdate, now time.Time) (audit.Record, bool) {
	if u.Kind != "tool_call" {
		return audit.Record{}, false
	}
	var raw struct {
		Title    string `json:"title"`
		Kind     string `json:"kind"`
		RawInput struct {
			Command string `json:"command"`
		} `json:"rawInput"`
	}
	_ = json.Unmarshal(u.Raw, &raw)

	summary := raw.Title
	if summary == "" {
		summary = "tool_call"
	}
	tool := raw.Kind
	if tool == "" {
		tool = summary
	}

	clue := strings.ToLower(raw.Kind + " " + raw.Title)
	risk := "read"
	if raw.RawInput.Command != "" || strings.Contains(clue, "shell") || strings.Contains(clue, "exec") || strings.Contains(clue, "bash") {
		risk = "exec"
	} else if strings.Contains(clue, "write") || strings.Contains(clue, "edit") || strings.Contains(clue, "create") || strings.Contains(clue, "delete") {
		risk = "write"
	}

	return audit.Record{
		TS:       now.Format(time.RFC3339),
		Project:  lc.Project,
		Repo:     lc.RepoDir,
		Seat:     lc.Seat,
		Task:     lc.Task,
		Kind:     "acp:" + lc.Kind,
		Session:  string(u.SessionID),
		Tool:     tool,
		Summary:  summary,
		Risk:     risk,
		Decision: "allow",
	}, true
}

// describeOptions renders a permission request's options for an escalation record.
func describeOptions(opts []acp.PermissionOption) string {
	parts := make([]string, 0, len(opts))
	for _, o := range opts {
		parts = append(parts, fmt.Sprintf("%s(%s)", o.Name, o.Kind))
	}
	return strings.Join(parts, ", ")
}

// autoSelectPermission picks the first option that grants the request — one whose
// kind/name/id reads as allow/approve/yes/once — while never selecting an option
// that reads as a rejection (so "reject_once" is not matched by "once"). Returns
// ("", false) when no allow-like option is offered (the handler then cancels).
func autoSelectPermission(opts []acp.PermissionOption) (string, bool) {
	for _, o := range opts {
		s := strings.ToLower(o.Kind + " " + o.Name + " " + o.OptionID)
		if strings.Contains(s, "reject") || strings.Contains(s, "deny") || strings.Contains(s, "cancel") {
			continue
		}
		if strings.Contains(s, "allow") || strings.Contains(s, "approve") || strings.Contains(s, "yes") || strings.Contains(s, "once") {
			return o.OptionID, true
		}
	}
	return "", false
}

// acpCommand maps a seat kind to its ACP adapter command + args (spec §A.2). The
// second return is false for a kind with no ACP transport (e.g. antigravity), which
// the caller reports with a --transport=cmd hint.
//
// acpCommand maps an agent kind to its ACP-transport launch command — always a
// binary invoked DIRECTLY (never `npx`), so exec.LookPath resolves it and a
// missing binary fails loudly instead of hanging.
//
// Architecture: kimi + gemini + opencode ship native ACP in their installed
// binary (`kimi acp` / `gemini --acp` / `opencode acp`). claude + codex are the
// deep-integration tier (their SDKs, not ACP); their ACP path is the generic
// bridge, run via the bridge's globally-installed bin (a shebang'd node script:
// `claude-agent-acp` / `codex-acp`) — NOT `npx -y`, whose first-run download
// blocks the stdio initialize handshake and made the ACP transport never
// handshake (the documented "hang"; node-direct returns capabilities instantly,
// verified 2026-07-23). Install the bridge with `npm i -g <pkg>`. For
// claude/codex the cmd transport (deep integration) is the default; the ACP path
// is opt-in via --transport <kind>=acp.
func acpCommand(kind string) (command string, args []string, ok bool) {
	switch kind {
	case "kimi-cli":
		return "kimi", []string{"acp"}, true
	case "gemini-cli":
		// Native ACP in the gemini binary — direct invocation, no npx cold-start
		// / cache fragility. Consistent with how the cmd transport already runs
		// gemini (Command "gemini").
		return "gemini", []string{"--acp"}, true
	case "opencode":
		// Native ACP in the opencode binary. Model is inherited from opencode's
		// global config (same pattern as kimi-cli); usage is parsed from the
		// session/prompt result, verified 2026-07-09.
		return "opencode", []string{"acp"}, true
	case "claude-code":
		// Run the globally-installed bridge bin DIRECTLY (a node script with a
		// shebang). NOT `npx -y`: npx's first-run download blocks the stdio
		// initialize handshake, so the ACP transport never handshaked (the
		// documented "hang"). node-direct returns capabilities instantly
		// (verified 2026-07-23). Install with `npm i -g
		// @agentclientprotocol/claude-agent-acp`; if absent, exec.LookPath fails
		// loudly instead of hanging. claude is deep-integration tier — cmd is the
		// default; this ACP path is opt-in via --transport claude-code=acp.
		return "claude-agent-acp", nil, true
	case "codex-cli":
		// Same as claude-code: run the global bin directly, not `npx -y`
		// (`npm i -g @zed-industries/codex-acp`). codex is deep-integration tier.
		return "codex-acp", nil, true
	default:
		return "", nil, false
	}
}

// acpEnv builds the child environment for an ACP stint: the same PACT_* audit +
// checkpoint-pinning vars CmdRunner injects (so a worker's checkpoint reaches the
// driver's worktree regardless of where its MCP resolves its root), plus a
// per-kind override. For claude-code the ANTHROPIC_API_KEY is stripped so the ACP
// adapter uses the interactive OAuth creds rather than metered API billing —
// os/exec dedups the environment keeping the LAST value, so the empty entry
// appended here overrides the inherited one.
func acpEnv(lc LaunchContext) []string {
	pactDir := filepath.Join(lc.RepoDir, ".pact")
	if abs, err := filepath.Abs(pactDir); err == nil {
		pactDir = abs
	}
	env := []string{
		"PACT_AGENT_ID=" + lc.Seat,
		"PACT_TASK_ID=" + lc.Task,
		"PACT_PROJECT=" + lc.Project,
		"PACT_DIR=" + pactDir,
	}
	// Isolate a single-vendor agent from sibling vendors' credentials: a kimi /
	// gemini / codex stint must not inherit the user's ANTHROPIC key (and so on).
	env = append(env, crossVendorStrip(lc.Kind)...)
	if lc.Kind == "claude-code" {
		// claude-code authenticates via interactive OAuth here, not the metered
		// API key — strip its own key too (crossVendorStrip keeps it).
		env = append(env, "ANTHROPIC_API_KEY=")
	}
	return env
}

// RoutedLocalRunner dispatches each stint to the ACP or the command transport by a
// per-kind mode map. An unset kind defaults to "cmd", so an empty Modes map is
// behavior-identical to a bare CmdRunner (the default: zero behavior change).
type RoutedLocalRunner struct {
	Modes map[string]string // kind → "acp" | "cmd" (default "cmd")
	Cmd   Runner
	Acp   Runner
}

// NewRoutedLocalRunner builds the production routed runner: a CmdRunner and an
// AcpRunner (idle watchdog + auto permission policy) selected per kind by modes.
func NewRoutedLocalRunner(modes map[string]string, idle time.Duration) RoutedLocalRunner {
	return RoutedLocalRunner{
		Modes: modes,
		Cmd:   NewCmdRunner(idle),
		Acp:   NewAcpRunner(idle, PermissionAuto),
	}
}

// Run routes lc to the ACP runner when its kind is mapped to "acp", else the
// command runner.
func (r RoutedLocalRunner) Run(ctx context.Context, lc LaunchContext) error {
	if r.Modes[lc.Kind] == "acp" {
		return r.Acp.Run(ctx, lc)
	}
	return r.Cmd.Run(ctx, lc)
}
