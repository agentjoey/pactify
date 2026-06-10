package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
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
		kind := ask(fmt.Sprintf("Agent kind %v (blank to skip wiring): ", agent.Kinds()))
		entry := "CLAUDE.md"
		if ad, ok := agent.Get(kind); ok && ad.DefaultEntry() != "" {
			entry = ad.DefaultEntry()
		}
		seatSpec := seat + ":worker:" + entry
		if kind != "" {
			seatSpec += ":" + kind
		}
		// pact.Init records the init event under PACT_AGENT_ID. The user is
		// running setup precisely because they haven't exported it yet, so it
		// would fail closed. The prompted seat IS their identity — set it for
		// this process before init; the printed export line makes it permanent.
		if paths.AgentID() == "" {
			os.Setenv("PACT_AGENT_ID", seat)
		}
		if err := pact.Init(project, []string{seatSpec}); err != nil {
			return err
		}
		if kind != "" {
			if err := agent.Wire(kind, seat, "worker", cwd); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "\n✅ Initialized .pact/ for %q.\n", project)
		fmt.Fprintf(out, "Set your seat for shell verbs (make it permanent in your shell rc):\n  export PACT_AGENT_ID=%s\n", seat)
	} else {
		kind := ask(fmt.Sprintf("Wire an agent — kind %v (blank to skip): ", agent.Kinds()))
		if kind != "" {
			seat := ask("Seat id: ")
			if err := agent.Wire(kind, seat, "worker", cwd); err != nil {
				return err
			}
			fmt.Fprintf(out, "\n✅ Wired %s.\n  export PACT_AGENT_ID=%s\n", kind, seat)
		}
	}
	fmt.Fprintln(out, "\nNext: run `pactify doctor` to verify, then `pactify status`.")
	return nil
}

func newSetupCmd() *cobra.Command {
	return &cobra.Command{Use: "setup", Short: "guided onboarding (interactive)",
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			return runSetup(os.Stdin, c.OutOrStdout(), cwd, isTTY())
		}}
}
