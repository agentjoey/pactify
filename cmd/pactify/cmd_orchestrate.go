package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/spf13/cobra"
)

// parseCriticSeat normalizes the --critic flag value: it accepts either the
// documented `seat=<seat>` form or a bare `<seat>`, returning the seat id (""
// when unset). The seat= prefix is optional sugar so the flag reads naturally.
func parseCriticSeat(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(v, "seat="))
}

// transportModesFromFlags seeds the default transport routing and applies
// explicit --transport overrides. The default maps opencode to ACP (see
// orchestrate.DefaultTransportModes); --transport opencode=cmd still reverts it.
func transportModesFromFlags(transports []string) (map[string]string, error) {
	modes := orchestrate.DefaultTransportModes()
	for _, t := range transports {
		parts := strings.SplitN(t, "=", 2)
		if len(parts) != 2 || parts[0] == "" || (parts[1] != "acp" && parts[1] != "cmd") {
			return nil, fmt.Errorf("--transport must be kind=acp|cmd, got %q", t)
		}
		modes[parts[0]] = parts[1]
	}
	return modes, nil
}

// newOrchestrateCmd wires `pactify orchestrate`: the autonomous driver that walks
// the pact state machine in the current repo, launching the owner/reviewer agent
// at each transition and merging behind a hard test gate, until the work ships or
// it escalates to a human (writing .pact/orchestrate/escalation-*.md and pausing).
func newOrchestrateCmd() *cobra.Command {
	var feature string
	var resume bool
	var maxRework, maxFails, maxIters int
	var runTimeoutMin, idleTimeoutMin int
	var maxConc int
	var dryRun bool
	var seatKinds []string
	var seatHosts []string
	var transports []string
	var asSeat string
	var keepSessions bool
	var inPlace bool
	var sandbox bool // deprecated: sandbox is now the default; flag kept as a no-op alias
	var critic string

	cmd := &cobra.Command{
		Use:   "orchestrate",
		Short: "autonomously drive the pact state machine (launch agents → review → merge)",
		Long: `orchestrate reads the pact log, and at each state transition launches the
responsible seat's agent headlessly: a worker for assigned/changes_requested/
in_progress tasks, the reviewer for awaiting_review tasks, and merges a feature
once all its tasks are accepted AND an independent hard test gate passes.

It runs serially (one agent at a time) and pauses by escalating to a human when a
task can't converge (rework/fail limits) or the hard gate fails — writing a record
under .pact/orchestrate/ and notifying. Fix the cause and re-run to resume.

Each acting seat needs a headless runner: map it with --seat-kind seat=kind
(kinds with a runner: opencode, claude-code, gemini-cli). GUI/desktop agents
(antigravity, *-desktop) cannot be driven headlessly.`,
		RunE: func(c *cobra.Command, _ []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			km := map[string]string{}
			for _, sk := range seatKinds {
				parts := strings.SplitN(sk, "=", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					return fmt.Errorf("--seat-kind must be seat=kind, got %q", sk)
				}
				km[parts[0]] = parts[1]
			}
			// km carries ONLY the explicit --seat-kind flags (the highest-priority
			// override). Defaulting each seat's kind from the roster now happens LIVE in
			// the driver loop (opts.kind re-reads Agents[].Kind every iteration), so a
			// seat that joins mid-run — or re-declares its kind — is drivable next
			// iteration without a restart (spec §6 WS-K dynamic seats).

			// The driver needs an acting seat for its own merges. Resolve --as, else
			// PACT_AGENT_ID; fail fast (before any agent runs) when neither is set, so
			// a run can't do all the work and then die at the final merge.
			orchestrator := asSeat
			if orchestrator == "" {
				orchestrator = os.Getenv("PACT_AGENT_ID")
			}
			if orchestrator == "" && !dryRun {
				return fmt.Errorf("orchestrate needs an acting seat for merges: pass --as <seat> or set PACT_AGENT_ID")
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			// --seat-host routes those seats' stints to other machines (M3
			// distributed orchestrate): push → pact.stint rpc via the relay →
			// poll origin for the checkpoint → merge back. Everything else is
			// unchanged — a remote timeout is the same soft failure a local
			// agent crash is.
			hosts := map[string]string{}
			for _, sh := range seatHosts {
				parts := strings.SplitN(sh, "=", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					return fmt.Errorf("--seat-host must be seat=machineId, got %q", sh)
				}
				hosts[parts[0]] = parts[1]
			}

			// --transport kind=acp|cmd routes those kinds' stints over the Agent
			// Client Protocol (a persistent stdio JSON-RPC session) instead of a
			// one-shot headless command. Defaults are seeded from
			// orchestrate.DefaultTransportModes() and overridden by explicit flags.
			transportModes, err := transportModesFromFlags(transports)
			if err != nil {
				return err
			}

			idle := time.Duration(idleTimeoutMin) * time.Minute
			local := orchestrate.NewRoutedLocalRunner(transportModes, idle)

			runner := orchestrate.Runner(local)
			if len(hosts) > 0 {
				dispatch, closeDispatch, derr := newRelayStintDispatch(ctx)
				if derr != nil {
					return fmt.Errorf("--seat-host needs a relay session: %w", derr)
				}
				defer closeDispatch()
				runner = &orchestrate.RemoteRunner{
					Hosts:    hosts,
					Local:    local,
					Dispatch: dispatch,
					Timeout:  time.Duration(runTimeoutMin) * time.Minute,
				}
			}

			opts := orchestrate.Options{
				Run:             runner,
				Dir:             dir,
				Feature:         feature,
				Th:              orchestrate.Thresholds{MaxRework: maxRework, MaxFails: maxFails, MaxIters: maxIters},
				DryRun:          dryRun,
				Now:             func() string { return time.Now().Format("20060102-150405") },
				SeatKind:        func(seat string) string { return km[seat] },
				RunTimeout:      time.Duration(runTimeoutMin) * time.Minute,
				IdleTimeout:     time.Duration(idleTimeoutMin) * time.Minute,
				Orchestrator:    orchestrator,
				CleanupSessions: !keepSessions,
				Critic:          parseCriticSeat(critic),
			}
			// --max-concurrency > 1 drives independent features in parallel, each in
			// an isolated worktree, merges serialized onto base. Incompatible with
			// --feature (which limits to one feature) and --dry-run (single-step preview).
			if maxConc > 1 && !dryRun && feature == "" {
				if err := orchestrate.RunParallel(ctx, orchestrate.ParallelOptions{Options: opts, MaxConcurrency: maxConc}); err != nil {
					return err
				}
				fmt.Fprintln(c.OutOrStdout(), "orchestrate: stopped — work shipped, paused for escalation (see .pact/orchestrate/)")
				return nil
			}
			// The serial driver runs in an isolated worktree BY DEFAULT so workers
			// never touch the user's active tree (only the final merged result lands
			// on base; the .pact ledger is copied in/out and runtime status mirrors to
			// the main dir so the dashboard still sees live progress — spec
			// coordination-authority P0b). Opt out with --in-place. Always in-place for
			// --dry-run (no side effects); parallel already isolates per-feature.
			if !inPlace && !dryRun {
				if err := orchestrate.RunSandbox(ctx, opts); err != nil {
					return err
				}
				fmt.Fprintln(c.OutOrStdout(), "orchestrate: stopped (sandbox) — work shipped, paused for escalation (see .pact/orchestrate/)")
				return nil
			}
			if err := orchestrate.Run(ctx, opts); err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), "orchestrate: stopped — work shipped, paused for escalation (see .pact/orchestrate/), or dry-run preview")
			return nil
		},
	}
	cmd.Flags().StringVar(&feature, "feature", "", "limit the run to this feature id (default: all features)")
	// --resume is documentary: re-running orchestrate always continues from the
	// current (already-advanced) pact state. Escalations are not auto-cleared.
	cmd.Flags().BoolVar(&resume, "resume", false, "continue from the current state after fixing an escalation (re-running has the same effect)")
	cmd.Flags().IntVar(&maxRework, "max-rework", 3, "escalate after this many changes-requested rounds on a task")
	cmd.Flags().IntVar(&maxFails, "max-fails", 2, "escalate after this many failed agent runs on a task")
	cmd.Flags().IntVar(&maxIters, "max-iters", 50, "global iteration cap (backstop against a non-converging loop)")
	cmd.Flags().IntVar(&runTimeoutMin, "run-timeout", 30, "minutes for one agent run end-to-end before killing it as a soft failure (0 = no timeout)")
	cmd.Flags().IntVar(&idleTimeoutMin, "idle-timeout", 5, "patrol window: kill an agent as hung only after this many minutes of NO output AND NO working-tree changes (a quiet-but-writing agent keeps running); soft failure → retry; 0 = no idle watchdog")
	cmd.Flags().IntVar(&maxConc, "max-concurrency", 1, "drive up to N independent features in parallel (isolated worktrees, serialized merges); 1 = serial. Ignored with --feature/--dry-run")
	cmd.Flags().BoolVar(&keepSessions, "keep-sessions", false, "keep each agent's sessions after its task is accepted (default: close them — opencode only, matched by the pact:<seat> title the runner stamps)")
	cmd.Flags().BoolVar(&inPlace, "in-place", false, "run the driver directly in your active working tree instead of an isolated worktree (the default is sandboxed: workers never touch your tree, only the merged result lands on base)")
	cmd.Flags().BoolVar(&sandbox, "sandbox", false, "deprecated: sandbox is now the default (this flag is a no-op; use --in-place to opt out)")
	_ = cmd.Flags().MarkDeprecated("sandbox", "sandbox is now the default; use --in-place to opt out")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the next action and the command it would exec, without launching any agent")
	cmd.Flags().StringArrayVar(&seatKinds, "seat-kind", nil, "seat=kind for headless launch (repeatable), e.g. --seat-kind w=opencode --seat-kind orch=claude-code")
	cmd.Flags().StringArrayVar(&seatHosts, "seat-host", nil, "seat=machineId to run that seat's stints on another machine via the relay (repeatable; requires a cloud session + the project's git origin)")
	cmd.Flags().StringArrayVar(&transports, "transport", nil, "kind=acp|cmd to drive that kind over the Agent Client Protocol instead of a headless command (repeatable), e.g. --transport kimi-cli=acp; default: opencode=acp (all others cmd); --transport opencode=cmd to revert")
	cmd.Flags().StringVar(&asSeat, "as", "", "seat the driver acts as for its own merges (default $PACT_AGENT_ID)")
	cmd.Flags().StringVar(&critic, "critic", "", "run this seat as a read-only pre-review critic (accepts seat=<seat> or <seat>); overrides `pactify config critic`; default off — the critic scores the diff but has no gating power")
	return cmd
}
