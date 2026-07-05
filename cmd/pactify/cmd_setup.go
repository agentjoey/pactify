package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentreg"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/wizard"
	"github.com/spf13/cobra"
)

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

// runSetup orchestrates guided onboarding. interactive=false (piped / agent shell)
// prints guidance and returns without prompting, so agents are never blocked.
func runSetup(in io.Reader, out io.Writer, cwd string, interactive bool) error {
	if !interactive {
		fmt.Fprintln(out, "pactify setup is interactive. For scripts/agents use:")
		fmt.Fprintln(out, "  pactify init --project <name> --seat 'id:roles:entry[:kind]'")
		fmt.Fprintln(out, "  pactify agent add <kind> --id <seat> --roles <roles>")
		return nil
	}
	r := bufio.NewReader(in)
	ask := func(prompt string) string {
		fmt.Fprint(out, prompt)
		s, _ := r.ReadString('\n')
		return strings.TrimSpace(s)
	}

	hasPact := false
	if _, err := os.Stat(filepath.Join(cwd, paths.Dir())); err == nil {
		hasPact = true
	}

	if !hasPact {
		project := ask("Project name: ")
		seat := ask("Your seat id (PACT_AGENT_ID): ")
		if seat == "" {
			return fmt.Errorf("seat id is required")
		}
		roles := ask("Roles (comma-separated) [worker]: ")
		if roles == "" {
			roles = "worker"
		}
		kind := ask(fmt.Sprintf("Agent kind %s (blank to skip wiring): ", strings.Join(agent.Kinds(), ", ")))
		entry := "CLAUDE.md"
		if kind != "" {
			// Fail closed before any writes, like the init command does.
			ad, ok := agent.Get(kind)
			if !ok {
				return fmt.Errorf("unknown agent kind %q (supported: %v)", kind, agent.Kinds())
			}
			if ad.DefaultEntry() != "" {
				entry = ad.DefaultEntry()
			}
		}
		seatSpec := seat + ":" + roles + ":" + entry
		if kind != "" {
			seatSpec += ":" + kind
		}
		// The prompted seat is this repo's identity — adopt it for this process even
		// if the shell carries a stale PACT_AGENT_ID from another repo. pact.Init
		// records the init event under PACT_AGENT_ID, so it must match the roster's
		// declared seat; the printed export line makes it permanent for the shell.
		os.Setenv("PACT_AGENT_ID", seat)
		if err := pact.Init(project, []string{seatSpec}); err != nil {
			return err
		}
		if kind != "" {
			if err := agent.Wire(kind, seat, roles, cwd); err != nil {
				return err
			}
			reportWire(out, kind, seat, roles, cwd)
		}
		fmt.Fprintf(out, "\n✅ Initialized .pact/ for %q.\n", project)
		fmt.Fprintf(out, "Set your seat for shell verbs (make it permanent in your shell rc):\n  export PACT_AGENT_ID=%s\n", seat)
	} else {
		kind := ask(fmt.Sprintf("Wire an agent — kind %s (blank to skip): ", strings.Join(agent.Kinds(), ", ")))
		if kind != "" {
			seat := ask("Seat id: ")
			if seat == "" {
				return fmt.Errorf("seat id is required")
			}
			roles := ask("Roles (comma-separated) [worker]: ")
			if roles == "" {
				roles = "worker"
			}
			if err := agent.Wire(kind, seat, roles, cwd); err != nil {
				return err
			}
			reportWire(out, kind, seat, roles, cwd)
			fmt.Fprintf(out, "\n✅ Wired %s.\n  export PACT_AGENT_ID=%s\n", kind, seat)
		}
	}
	fmt.Fprintln(out, "\nNext: run `pactify doctor` to verify, then `pactify status`.")
	return nil
}

// runSetupYes is the zero-interaction onboarding path (`pactify setup --yes`):
// it inits + wires a project from the registered-agent roster wizard.Suggest
// proposes, with no prompts — the other half of the "setup + run = two commands"
// story (backlog P0-2). It refuses on an already-initialized repo (nothing to
// seed) and when no agents are registered (nothing to staff), pointing at the
// fix in both cases. The project name defaults to the repo directory basename.
func runSetupYes(out io.Writer, cwd, project string) error {
	if _, err := os.Stat(filepath.Join(cwd, paths.Dir())); err == nil {
		return fmt.Errorf("setup --yes: %s already exists — this repo is already initialized (use `pactify agent add` to wire more seats)", paths.Dir())
	}
	reg, err := agentreg.Load()
	if err != nil {
		return err
	}
	var kinds []string
	for _, a := range reg.Agents {
		kinds = append(kinds, a.Kind)
	}
	bindings := wizard.Suggest(kinds)
	if len(bindings) == 0 {
		return fmt.Errorf("setup --yes: no registered agents to staff — run `pactify agent scan` then `pactify agent register <kind>` first")
	}
	if project == "" {
		project = filepath.Base(cwd)
	}

	// Build the full seat roster for a single init (every seat seeded at once),
	// then wire each drivable kind so it can join. Mirrors `setup suggest`'s
	// printed commands, executed instead of printed.
	lead := bindings[0].Seat // wizard.Suggest orders the claude-lead first
	var seatSpecs []string
	for _, b := range bindings {
		entry := "AGENTS.md"
		if ad, ok := agent.Get(b.Kind); ok && ad.DefaultEntry() != "" {
			entry = ad.DefaultEntry()
		}
		seatSpecs = append(seatSpecs, b.Seat+":"+strings.Join(b.Roles, ",")+":"+entry+":"+b.Kind)
	}
	// pact.Init records the init event under PACT_AGENT_ID; adopt the lead seat
	// so provenance matches the roster's declared lead.
	os.Setenv("PACT_AGENT_ID", lead)
	if err := pact.Init(project, seatSpecs); err != nil {
		return err
	}
	for _, b := range bindings {
		if err := agent.Wire(b.Kind, b.Seat, strings.Join(b.Roles, ","), cwd); err != nil {
			return err
		}
		reportWire(out, b.Kind, b.Seat, strings.Join(b.Roles, ","), cwd)
	}
	fmt.Fprintf(out, "\n✅ Initialized .pact/ for %q with %d seat(s).\n", project, len(bindings))
	if warns := wizard.Validate(bindings); len(warns) > 0 {
		fmt.Fprintln(out, "Gaps to resolve:")
		for _, w := range warns {
			fmt.Fprintf(out, "  ⚠ %s\n", w)
		}
	}
	fmt.Fprintf(out, "Set your seat for shell verbs:\n  export PACT_AGENT_ID=%s\n", lead)
	fmt.Fprintf(out, "Next: `pactify run \"<goal>\" --feature <id>` to go from a sentence to shipped.\n")
	return nil
}

func newSetupCmd() *cobra.Command {
	var yes bool
	var project string
	cmd := &cobra.Command{Use: "setup", Short: "guided onboarding (interactive, or --yes for zero-interaction)",
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			if yes {
				return runSetupYes(c.OutOrStdout(), cwd, project)
			}
			return runSetup(os.Stdin, c.OutOrStdout(), cwd, isTTY())
		}}
	cmd.Flags().BoolVar(&yes, "yes", false, "zero-interaction: init + wire from your registered agents (wizard.Suggest roster) with no prompts")
	cmd.Flags().StringVar(&project, "project", "", "project name for --yes (default: repo directory name)")
	cmd.AddCommand(newSetupSuggestCmd())
	return cmd
}

// newSetupSuggestCmd wires `pactify setup suggest`: the bridge from registered
// machine agents (pactify agent register) to a project's pact seat roster (#1).
// It reads the machine agent registry, proposes a seat roster (lead + workers),
// flags any gap that would block the pact loop, and prints the exact init +
// wiring commands to apply it — turning "I registered my agents" into "this
// project can do work".
func newSetupSuggestCmd() *cobra.Command {
	return &cobra.Command{Use: "suggest", Short: "propose a project seat roster from your registered agents",
		RunE: func(c *cobra.Command, _ []string) error {
			out := c.OutOrStdout()
			reg, err := agentreg.Load()
			if err != nil {
				return err
			}
			var kinds []string
			for _, a := range reg.Agents {
				kinds = append(kinds, a.Kind)
			}
			bindings := wizard.Suggest(kinds)
			if len(bindings) == 0 {
				fmt.Fprintln(out, "no registered agents — run `pactify agent register <kind>` first (see `pactify agent scan`)")
				return nil
			}

			fmt.Fprintln(out, "Proposed seat roster (from your registered agents):")
			for _, b := range bindings {
				drive := "manual"
				if b.Drivable {
					drive = "drivable"
				}
				fmt.Fprintf(out, "  %-12s %-13s %-9s roles: %s\n", b.Seat, b.Kind, drive, strings.Join(b.Roles, ","))
			}

			if warns := wizard.Validate(bindings); len(warns) > 0 {
				fmt.Fprintln(out, "\nGaps to resolve:")
				for _, w := range warns {
					fmt.Fprintf(out, "  ⚠ %s\n", w)
				}
			}

			// Actionable apply steps: a single `init` seeds every seat, then wire
			// each drivable kind so it can join. This is the "now make it work" bridge.
			fmt.Fprintln(out, "\nApply with:")
			var seatArgs []string
			for _, b := range bindings {
				entry := "AGENTS.md"
				if ad, ok := agent.Get(b.Kind); ok && ad.DefaultEntry() != "" {
					entry = ad.DefaultEntry()
				}
				seatArgs = append(seatArgs, fmt.Sprintf("--seat %q", b.Seat+":"+strings.Join(b.Roles, ",")+":"+entry+":"+b.Kind))
			}
			fmt.Fprintf(out, "  pactify init <project> %s\n", strings.Join(seatArgs, " "))
			for _, b := range bindings {
				fmt.Fprintf(out, "  pactify agent add %s --id %s --roles %s\n", b.Kind, b.Seat, strings.Join(b.Roles, ","))
			}
			return nil
		}}
}
