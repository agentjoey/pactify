package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/planner"
	"github.com/agentjoey/pactify/internal/projection"
	"github.com/spf13/cobra"
)

// newPlanCmd wires `pactify plan`: it gathers the repo tree + roster, builds the
// planner prompt, and launches a planner agent that writes the per-task specs and
// the .pact/plan-<feature>.json manifest. By default it stops after generation so
// a human can review; --auto applies immediately, --run additionally chains
// orchestrate. The `apply` subcommand re-runs just the apply path on a manifest.
func newPlanCmd() *cobra.Command {
	var feature, plannerKind string
	var auto, run bool

	cmd := &cobra.Command{
		Use:   "plan \"<goal>\"",
		Args:  cobra.ExactArgs(1),
		Short: "decompose a goal into pact tasks via a planner agent",
		Long: `plan gathers the repo structure and seat roster, hands them to a planner
agent (default kind claude-code, model pinned), and the agent writes one spec per
task to .pact/tasks/<feature>-*.md plus the manifest .pact/plan-<feature>.json.

By default plan stops after generation so you can review the manifest, then run
` + "`pactify plan apply <feature>`" + `. --auto applies the plan immediately
(assigns the tasks); --run additionally points you at orchestrate to drive it.`,
		RunE: func(c *cobra.Command, a []string) error {
			if feature == "" {
				return fmt.Errorf("--feature is required")
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}

			st, err := pact.At(dir).StateProjection()
			if err != nil {
				return err
			}
			seats, roster := rosterFromState(st)

			tree, err := repoTree(dir)
			if err != nil {
				return err
			}

			prompt := planner.BuildPrompt(planner.PromptInput{
				Goal:     a[0],
				Feature:  feature,
				RepoTree: tree,
				Seats:    seats,
			})

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			out := c.OutOrStdout()
			fmt.Fprintf(out, "plan: launching planner (%s) for feature %q…\n", plannerKind, feature)
			if err := orchestrate.NewCmdRunner(0).Run(ctx, orchestrate.LaunchContext{
				Seat: "planner", Kind: plannerKind, Briefing: prompt, RepoDir: dir,
			}); err != nil {
				return fmt.Errorf("plan: planner agent failed: %w", err)
			}

			if !auto && !run {
				fmt.Fprintf(out, "plan: generated .pact/plan-%s.json + specs. Review, then run `pactify plan apply %s`.\n", feature, feature)
				return nil
			}

			n, err := applyPlan(dir, feature, roster)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "plan: applied — assigned %d task(s) for feature %q.\n", n, feature)
			if run {
				fmt.Fprintf(out, "plan: drive with `pactify orchestrate --feature %s --seat-kind <seat>=<kind> …`\n", feature)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&feature, "feature", "", "feature id the plan targets (required)")
	cmd.Flags().BoolVar(&auto, "auto", false, "apply the generated plan immediately (assign tasks) without manual review")
	cmd.Flags().BoolVar(&run, "run", false, "after applying, chain orchestrate to drive the work")
	cmd.Flags().StringVar(&plannerKind, "planner-kind", "claude-code", "agent kind to run as the planner")

	cmd.AddCommand(newPlanApplyCmd())
	return cmd
}

// newPlanApplyCmd wires `pactify plan apply <feature>`: read the manifest, parse
// it, and assign its tasks against the current roster.
func newPlanApplyCmd() *cobra.Command {
	var run bool
	cmd := &cobra.Command{
		Use:   "apply <feature>",
		Args:  cobra.ExactArgs(1),
		Short: "assign the tasks from .pact/plan-<feature>.json",
		RunE: func(c *cobra.Command, a []string) error {
			feature := a[0]
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			st, err := pact.At(dir).StateProjection()
			if err != nil {
				return err
			}
			_, roster := rosterFromState(st)

			n, err := applyPlan(dir, feature, roster)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "plan apply: assigned %d task(s) for feature %q.\n", n, feature)
			if run {
				fmt.Fprintf(c.OutOrStdout(), "plan apply: drive with `pactify orchestrate --feature %s --seat-kind <seat>=<kind> …`\n", feature)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&run, "run", false, "after applying, chain orchestrate to drive the work")
	return cmd
}

// applyPlan reads .pact/plan-<feature>.json, parses it, and assigns its tasks.
func applyPlan(dir, feature string, roster []string) (int, error) {
	path := filepath.Join(dir, ".pact", "plan-"+feature+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("plan apply: read %s: %w", filepath.Join(".pact", "plan-"+feature+".json"), err)
	}
	plan, err := planner.Parse(b)
	if err != nil {
		return 0, err
	}
	return planner.Apply(dir, plan, roster)
}

// rosterFromState turns the projected seats into planner SeatInfo (MVP: every
// seat is Drivable=true; precise GUI detection is backlog) and the flat seat-id
// roster Apply validates owners/reviewers against.
func rosterFromState(st projection.State) ([]planner.SeatInfo, []string) {
	seats := make([]planner.SeatInfo, 0, len(st.Agents))
	roster := make([]string, 0, len(st.Agents))
	for _, ag := range st.Agents {
		seats = append(seats, planner.SeatInfo{ID: ag.ID, Roles: ag.Roles, Drivable: true})
		roster = append(roster, ag.ID)
	}
	return seats, roster
}

// repoTree collects a shallow, deterministic view of the repo — top-level files
// plus one level of directories — from `git ls-files`, falling back to a
// top-level directory listing when the dir is not a git repo.
func repoTree(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "ls-files").Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return shallowWalk(dir)
	}
	set := map[string]struct{}{}
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f == "" {
			continue
		}
		seg := strings.Split(f, "/")
		switch {
		case len(seg) == 1:
			set[seg[0]] = struct{}{}
		case len(seg) == 2:
			set[seg[0]+"/"] = struct{}{}
			set[seg[0]+"/"+seg[1]] = struct{}{}
		default:
			set[seg[0]+"/"] = struct{}{}
			set[seg[0]+"/"+seg[1]+"/"] = struct{}{}
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n"), nil
}

// shallowWalk lists the top-level entries of a non-git directory, skipping the
// .git metadata, so plan still has some structure to feed the planner.
func shallowWalk(dir string) (string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".git") {
			continue
		}
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}
