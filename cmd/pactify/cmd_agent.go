package main

import (
	"fmt"
	"os"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	a := &cobra.Command{Use: "agent", Short: "wire agents (CLI + desktop) into this repo's pact"}

	var id, roles, project string
	var printOnly bool
	add := &cobra.Command{Use: "add <kind>", Args: cobra.ExactArgs(1),
		Short: "wire an agent kind (see docs/agent-onboarding.md for supported kinds)",
		RunE: func(c *cobra.Command, args []string) error {
			kind := args[0]
			repoAbs := project
			if repoAbs == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				repoAbs = wd
			}
			if printOnly {
				entry, cfg, err := agent.Render(kind, id, roles, repoAbs)
				if err != nil {
					return err
				}
				if entry != "" {
					fmt.Fprintf(c.OutOrStdout(), "# entry block (%s)\n%s\n\n", kind, entry)
				}
				fmt.Fprintf(c.OutOrStdout(), "# config snippet (%s)\n%s\n", kind, cfg)
				return nil
			}
			if err := agent.Wire(kind, id, roles, repoAbs); err != nil {
				return err
			}
			if ad, ok := agent.Get(kind); ok && ad.Config().Format == agent.TOML {
				_, cfg, _ := agent.Render(kind, id, roles, repoAbs)
				fmt.Fprintf(c.OutOrStdout(), "%s is doc-only — add this to %s:\n%s", kind, ad.Config().Path, cfg)
			}
			return nil
		}}
	add.Flags().StringVar(&id, "id", "", "seat id (PACT_AGENT_ID)")
	add.Flags().StringVar(&roles, "roles", "", "comma-separated roles")
	add.Flags().StringVar(&project, "project", "", "repo abs path for desktop apps (default: cwd)")
	add.Flags().BoolVar(&printOnly, "print", false, "print config + entry block, write nothing")

	a.AddCommand(add)
	return a
}
