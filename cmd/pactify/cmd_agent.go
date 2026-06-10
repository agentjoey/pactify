package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/spf13/cobra"
)

// reportWire prints what Wire actually did for kind: the doc-only snippet for
// TOML kinds, or the config path that was written (flagging machine-global).
func reportWire(out io.Writer, kind, id, roles, repoAbs string) {
	ad, ok := agent.Get(kind)
	if !ok {
		return
	}
	cfg := ad.Config()
	switch {
	case cfg.Format == agent.TOML:
		_, snip, _ := agent.Render(kind, id, roles, repoAbs)
		fmt.Fprintf(out, "%s is doc-only — add this to %s:\n%s", kind, cfg.Path, snip)
	case cfg.Scope == agent.Global:
		fmt.Fprintf(out, "wrote %s (machine-global)\n", agent.ExpandPath(cfg.Path))
	default:
		fmt.Fprintf(out, "wrote %s\n", cfg.Path)
	}
}

func newAgentCmd() *cobra.Command {
	a := &cobra.Command{Use: "agent", Short: "wire agents (CLI + desktop) into this repo's pact"}

	var id, roles, project string
	var printOnly bool
	add := &cobra.Command{Use: "add <kind>", Args: cobra.ExactArgs(1),
		Short: "wire an agent kind (see docs/agent-onboarding.md for supported kinds)",
		RunE: func(c *cobra.Command, args []string) error {
			kind := args[0]
			if id == "" {
				return fmt.Errorf("--id is required (the seat id, sets PACT_AGENT_ID)")
			}
			if roles == "" {
				return fmt.Errorf("--roles is required (comma-separated, e.g. worker)")
			}
			repoAbs := project
			if repoAbs == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				repoAbs = wd
			}
			absRepo, err := filepath.Abs(repoAbs)
			if err != nil {
				return err
			}
			repoAbs = absRepo
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
			reportWire(c.OutOrStdout(), kind, id, roles, repoAbs)
			return nil
		}}
	add.Flags().StringVar(&id, "id", "", "seat id (PACT_AGENT_ID)")
	add.Flags().StringVar(&roles, "roles", "", "comma-separated roles")
	add.Flags().StringVar(&project, "project", "", "repo abs path for desktop apps (default: cwd)")
	add.Flags().BoolVar(&printOnly, "print", false, "print config + entry block, write nothing")

	docs := &cobra.Command{Use: "docs", Short: "regenerate docs/agent-onboarding.md (run from repo root)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return os.WriteFile("docs/agent-onboarding.md", []byte(agent.RenderDoc()), 0o644)
		}}
	a.AddCommand(add, docs)
	return a
}
