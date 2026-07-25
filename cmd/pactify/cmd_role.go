package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/roles"
	"github.com/spf13/cobra"
)

// newRoleCmd manages role profiles — named (agent kind, model) combinations —
// and the seat→role bindings the orchestrate driver resolves at launch. The
// config is machine-level (~/.pactify/roles.json): it names locally installed
// agents and is advisory guidance for task assignment, never protocol state.
func newRoleCmd() *cobra.Command {
	root := &cobra.Command{Use: "role", Short: "manage role profiles (agent+model) and seat bindings"}

	var kind, model string
	var fallback []string
	set := &cobra.Command{Use: "set <name>", Args: cobra.ExactArgs(1), Short: "create or replace a role profile",
		RunE: func(c *cobra.Command, a []string) error {
			cfg, err := roles.Load()
			if err != nil {
				return err
			}
			if err := cfg.SetProfile(a[0], roles.Profile{Kind: kind, Model: model, Fallback: fallback}); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "role %q = %s/%s\n", a[0], kind, model)
			return nil
		}}
	set.Flags().StringVar(&kind, "kind", "", "agent kind (claude-code, opencode, kimi-cli, ...)")
	set.Flags().StringVar(&model, "model", "", "model id (defaults to the kind's built-in model)")
	set.Flags().StringSliceVar(&fallback, "fallback", nil, "comma-separated role names to fall back to when this profile cannot work")

	bind := &cobra.Command{Use: "bind <seat> <role>", Args: cobra.ExactArgs(2), Short: "bind a seat to a role",
		RunE: func(c *cobra.Command, a []string) error {
			cfg, err := roles.Load()
			if err != nil {
				return err
			}
			if err := cfg.Bind(a[0], a[1]); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "seat %q plays role %q\n", a[0], a[1])
			return nil
		}}

	unbind := &cobra.Command{Use: "unbind <seat>", Args: cobra.ExactArgs(1), Short: "drop a seat's role binding",
		RunE: func(c *cobra.Command, a []string) error {
			cfg, err := roles.Load()
			if err != nil {
				return err
			}
			if err := cfg.Unbind(a[0]); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "seat %q unbound\n", a[0])
			return nil
		}}

	list := &cobra.Command{Use: "list", Short: "list role profiles and seat bindings",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := roles.Load()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(cfg.Profiles))
			for n := range cfg.Profiles {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				p := cfg.Profiles[n]
				line := fmt.Sprintf("%s\t%s\t%s", n, p.Kind, p.Model)
				if len(p.Fallback) > 0 {
					line += "\tfallback: " + strings.Join(p.Fallback, ",")
				}
				fmt.Fprintln(c.OutOrStdout(), line)
			}
			seats := make([]string, 0, len(cfg.Bindings))
			for s := range cfg.Bindings {
				seats = append(seats, s)
			}
			sort.Strings(seats)
			for _, s := range seats {
				fmt.Fprintf(c.OutOrStdout(), "seat %s -> %s\n", s, cfg.Bindings[s])
			}
			return nil
		}}

	root.AddCommand(set, bind, unbind, list)
	return root
}
