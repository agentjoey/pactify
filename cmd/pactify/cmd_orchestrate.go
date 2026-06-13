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
	var asSeat string

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

			opts := orchestrate.Options{
				Dir:          dir,
				Feature:      feature,
				Th:           orchestrate.Thresholds{MaxRework: maxRework, MaxFails: maxFails, MaxIters: maxIters},
				DryRun:       dryRun,
				Now:          func() string { return time.Now().Format("20060102-150405") },
				SeatKind:     func(seat string) string { return km[seat] },
				RunTimeout:   time.Duration(runTimeoutMin) * time.Minute,
				IdleTimeout:  time.Duration(idleTimeoutMin) * time.Minute,
				Orchestrator: orchestrator,
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
	cmd.Flags().IntVar(&idleTimeoutMin, "idle-timeout", 5, "minutes of NO output before killing an agent as hung (soft failure → retry); 0 = no idle watchdog")
	cmd.Flags().IntVar(&maxConc, "max-concurrency", 1, "drive up to N independent features in parallel (isolated worktrees, serialized merges); 1 = serial. Ignored with --feature/--dry-run")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the next action and the command it would exec, without launching any agent")
	cmd.Flags().StringArrayVar(&seatKinds, "seat-kind", nil, "seat=kind for headless launch (repeatable), e.g. --seat-kind w=opencode --seat-kind orch=claude-code")
	cmd.Flags().StringVar(&asSeat, "as", "", "seat the driver acts as for its own merges (default $PACT_AGENT_ID)")
	return cmd
}
