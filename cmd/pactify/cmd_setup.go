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

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "setup", Short: "guided onboarding (interactive)",
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			return runSetup(os.Stdin, c.OutOrStdout(), cwd, isTTY())
		}}
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
