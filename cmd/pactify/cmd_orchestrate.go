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
	var runTimeoutMin int
	var dryRun bool
	var seatKinds []string

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

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			opts := orchestrate.Options{
				Dir:        dir,
				Feature:    feature,
				Th:         orchestrate.Thresholds{MaxRework: maxRework, MaxFails: maxFails, MaxIters: maxIters},
				DryRun:     dryRun,
				Now:        func() string { return time.Now().Format("20060102-150405") },
				SeatKind:   func(seat string) string { return km[seat] },
				RunTimeout: time.Duration(runTimeoutMin) * time.Minute,
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
	cmd.Flags().IntVar(&runTimeoutMin, "run-timeout", 30, "minutes to wait for one agent run before killing it as a soft failure (0 = no timeout)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the next action and the command it would exec, without launching any agent")
	cmd.Flags().StringArrayVar(&seatKinds, "seat-kind", nil, "seat=kind for headless launch (repeatable), e.g. --seat-kind w=opencode --seat-kind orch=claude-code")
	return cmd
}
