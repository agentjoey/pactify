package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/finish"
	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/planner"
	"github.com/spf13/cobra"
)

// newRunCmd wires `pactify run "<goal>"` — the end-to-end "one command" that
// collapses the plan → apply → orchestrate (→ finish) chain a user otherwise
// runs by hand (backlog P0-2 UX 收敛). It is the second half of the two-command
// onboarding story: `pactify setup` makes a project, `pactify run "<goal>"`
// takes it from a sentence to shipped, with the user never touching seat
// formats, --seat-kind maps, or the individual verbs.
//
// Seat→kind is resolved LIVE from the roster inside the driver (opts has no
// SeatKind override), so a `pactify setup`-wired project just works with no
// --seat-kind flags — the exact friction P0-2 targets.
//
// Steps: (1) launch the planner to decompose the goal into a task graph; (2)
// unless --yes, print the plan and ask for confirmation (a plain preview→confirm
// gate; agents/scripts pass --yes or pipe non-interactively); (3) apply (assign
// the tasks); (4) orchestrate (sandbox by default) to drive to shipped; (5) with
// --finish, push the merged result to the remote. Pushing is opt-in because it
// is outward-facing — `run` stops at "shipped locally" otherwise and prints the
// finish hint.
func newRunCmd() *cobra.Command {
	var feature, plannerKind string
	var yes, doFinish, inPlace bool
	var remote, branch string
	var maxRework, maxFails, maxIters, runTimeoutMin, idleTimeoutMin int

	cmd := &cobra.Command{
		Use:   "run \"<goal>\"",
		Args:  cobra.ExactArgs(1),
		Short: "plan → apply → orchestrate a goal end-to-end (the one-command driver)",
		Long: `run turns a one-sentence goal into shipped work in a single command:
it launches a planner to decompose the goal into a pact task graph, applies it
(assigns the tasks), then orchestrates the work to shipped behind the hard test
gate — pausing to escalate if a task can't converge.

Seat→kind is inferred from the project roster, so a project made with
` + "`pactify setup`" + ` needs no --seat-kind flags. By default run shows the
generated plan and asks before applying (skip with --yes); it stops at "shipped
locally" and leaves pushing to ` + "`pactify finish`" + ` unless --finish is set.`,
		RunE: func(c *cobra.Command, a []string) error {
			goal := a[0]
			if feature == "" {
				return fmt.Errorf("--feature is required (the feature id this goal maps to)")
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			out := c.OutOrStdout()

			// The driver needs an acting seat for its own merges — fail fast before
			// launching the planner so a run can't do all the work then die at merge.
			orchestrator := os.Getenv("PACT_AGENT_ID")
			if orchestrator == "" {
				return fmt.Errorf("run needs an acting seat: set PACT_AGENT_ID (e.g. via `pactify setup`)")
			}

			st, err := pact.At(dir).StateProjection()
			if err != nil {
				return err
			}
			seats, roster := rosterFromState(st)
			if len(roster) == 0 {
				return fmt.Errorf("run: no seats in the roster — run `pactify setup` first")
			}

			tree, err := repoTree(dir)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			// 1. Plan — launch the planner agent to write the manifest + specs.
			fmt.Fprintf(out, "run: planning %q (feature %q)…\n", goal, feature)
			prompt := planner.BuildPrompt(planner.PromptInput{
				Goal: goal, Feature: feature, RepoTree: tree, Seats: seats,
			})
			if err := orchestrate.NewCmdRunner(0).Run(ctx, orchestrate.LaunchContext{
				Seat: "planner", Kind: plannerKind, Briefing: prompt, RepoDir: dir,
			}); err != nil {
				return fmt.Errorf("run: planner agent failed: %w", err)
			}

			// 2. Preview → confirm (unless --yes or non-interactive).
			if !yes && isTTY() {
				summary, perr := planSummary(dir, feature)
				if perr != nil {
					return perr
				}
				fmt.Fprintf(out, "\n%s\n", summary)
				if !confirm(os.Stdin, out, "Apply this plan and drive it? [y/N]: ") {
					fmt.Fprintf(out, "run: aborted — review .pact/plan-%s.json, then `pactify plan apply %s`.\n", feature, feature)
					return nil
				}
			}

			// 3. Apply — assign the tasks.
			n, err := applyPlan(dir, feature, roster)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "run: applied — assigned %d task(s).\n", n)

			// 4. Orchestrate — drive to shipped. Sandbox by default so workers never
			// touch the user's active tree; --in-place opts out.
			opts := orchestrate.Options{
				Dir:             dir,
				Feature:         feature,
				Th:              orchestrate.Thresholds{MaxRework: maxRework, MaxFails: maxFails, MaxIters: maxIters},
				Now:             func() string { return time.Now().Format("20060102-150405") },
				RunTimeout:      time.Duration(runTimeoutMin) * time.Minute,
				IdleTimeout:     time.Duration(idleTimeoutMin) * time.Minute,
				Orchestrator:    orchestrator,
				CleanupSessions: true,
			}
			fmt.Fprintln(out, "run: orchestrating…")
			if inPlace {
				err = orchestrate.Run(ctx, opts)
			} else {
				err = orchestrate.RunSandbox(ctx, opts)
			}
			if err != nil {
				return err
			}

			// 5. Finish — push, opt-in (outward-facing).
			if doFinish {
				fmt.Fprintf(out, "run: finishing — pushing %s to %s…\n", branch, remote)
				f := finish.Finisher{
					Run: func(dir, name string, args ...string) (string, error) {
						cm := exec.Command(name, args...)
						cm.Dir = dir
						o, e := cm.CombinedOutput()
						return string(o), e
					},
					HasGH: func() bool { _, e := exec.LookPath("gh"); return e == nil },
				}
				fo, ferr := f.Push(dir, remote, branch)
				if ferr != nil {
					return ferr
				}
				fmt.Fprint(out, fo)
				return nil
			}
			fmt.Fprintf(out, "run: stopped — work shipped locally (or paused, see .pact/orchestrate/). Deliver with `pactify finish`.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&feature, "feature", "", "feature id this goal maps to (required)")
	cmd.Flags().StringVar(&plannerKind, "planner-kind", "claude-code", "agent kind to run as the planner")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the plan preview/confirm and apply immediately")
	cmd.Flags().BoolVar(&doFinish, "finish", false, "after shipping, push to the remote (outward-facing; off by default)")
	cmd.Flags().BoolVar(&inPlace, "in-place", false, "orchestrate in the active tree instead of an isolated sandbox worktree")
	cmd.Flags().StringVar(&remote, "remote", "origin", "git remote for --finish")
	cmd.Flags().StringVar(&branch, "branch", "main", "branch to push for --finish")
	cmd.Flags().IntVar(&maxRework, "max-rework", 3, "escalate after this many changes-requested rounds on a task")
	cmd.Flags().IntVar(&maxFails, "max-fails", 2, "escalate after this many failed agent runs on a task")
	cmd.Flags().IntVar(&maxIters, "max-iters", 50, "global iteration cap")
	cmd.Flags().IntVar(&runTimeoutMin, "run-timeout", 30, "minutes for one agent run before killing it as a soft failure (0 = none)")
	cmd.Flags().IntVar(&idleTimeoutMin, "idle-timeout", 5, "patrol window in minutes of no output AND no tree change before killing a hung agent (0 = none)")
	return cmd
}

// planSummary renders a short human-readable view of a generated plan manifest
// for the preview→confirm gate: the task ids, their owners/reviewers, and deps.
func planSummary(dir, feature string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, ".pact", "plan-"+feature+".json"))
	if err != nil {
		return "", fmt.Errorf("run: read plan manifest: %w", err)
	}
	plan, err := planner.Parse(b)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Planned %d task(s) for feature %q:", len(plan.Tasks), feature)
	for _, t := range plan.Tasks {
		fmt.Fprintf(&sb, "\n  • %-16s owner=%s reviewer=%s", t.ID, t.Owner, t.Reviewer)
		if len(t.Deps) > 0 {
			fmt.Fprintf(&sb, " deps=%s", strings.Join(t.Deps, ","))
		}
	}
	return sb.String(), nil
}

// confirm reads a y/N answer from in, defaulting to No on anything but y/yes.
func confirm(in io.Reader, out io.Writer, prompt string) bool {
	fmt.Fprint(out, prompt)
	line, _ := bufio.NewReader(in).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
