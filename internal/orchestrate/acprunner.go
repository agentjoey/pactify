package orchestrate

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
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

	conn, err := r.Spawn(ctx, command, args, acpEnv(lc), lc.RepoDir)
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
			r.OnUsage(lc.RepoDir, lc.Seat, lc.Task, u)
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

	if r.Idle <= 0 {
		return (<-done).err
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
			conn.Close() // kill → the pending Prompt unblocks with an error
			<-done       // reap the prompt goroutine
			return fmt.Errorf("%w: no ACP session update for %s — killed", errIdle, r.Idle)
		case <-ctx.Done():
			conn.Close()
			<-done
			return ctx.Err()
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
// second return is false for a kind with no ACP transport (e.g. opencode), which
// the caller reports with a --transport=cmd hint.
//
// Bridge package versions are pinned to avoid latest-tag drift; verified with
// `npm view` on 2026-07-07. Upgrading is intentional: change the pinned version
// here only after verifying the new release works.
// acpCommand maps an agent kind to its ACP-transport launch command.
//
// Architecture (2026-07-08 decision): the ACP tier is kimi + gemini — both ship
// native ACP in their installed binary (`kimi acp` / `gemini --acp`), so we
// invoke the binary DIRECTLY (no npx). claude + codex are the deep-integration
// tier (their SDKs, not ACP); their npx-bridge entries below are the LEGACY
// generic-ACP path, kept only as a fallback — the claude bridge
// (@agentclientprotocol/claude-agent-acp) additionally hangs the stdio handshake
// under npx (verified 2026-07-08), so the deep-integration path is the real one
// for claude/codex.
func acpCommand(kind string) (command string, args []string, ok bool) {
	switch kind {
	case "kimi-cli":
		return "kimi", []string{"acp"}, true
	case "gemini-cli":
		// Native ACP in the gemini binary — direct invocation, no npx cold-start
		// / cache fragility. Consistent with how the cmd transport already runs
		// gemini (Command "gemini").
		return "gemini", []string{"--acp"}, true
	case "claude-code":
		// LEGACY/fallback — claude is deep-integration tier; this npx bridge also
		// hangs the handshake (see doc above). Prefer the deep-integration path.
		return "npx", []string{"-y", "@agentclientprotocol/claude-agent-acp@0.57.0"}, true
	case "codex-cli":
		// LEGACY/fallback — codex is deep-integration tier.
		return "npx", []string{"-y", "@zed-industries/codex-acp@0.16.0"}, true
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
	if lc.Kind == "claude-code" {
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
