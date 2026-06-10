package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "pactify", Short: "pact protocol CLI", SilenceUsage: true, SilenceErrors: true}

	var project string
	var seats []string
	initCmd := &cobra.Command{Use: "init", Short: "scaffold .pact/ and bake entry files",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Parse + validate all seats up front so init fails closed (before any writes).
			parsed := make([]pact.Seat, 0, len(seats))
			for _, raw := range seats {
				s, err := pact.ParseSeat(raw)
				if err != nil {
					return err
				}
				if s.Kind != "" && s.Kind != "shell" {
					ad, ok := agent.Get(s.Kind)
					if !ok {
						return fmt.Errorf("unknown agent kind %q (supported: %v)", s.Kind, agent.Kinds())
					}
					if ad.DefaultEntry() == "" {
						return fmt.Errorf("kind %q has no project entry file; wire it with `pactify agent add %s` instead of init --seat", s.Kind, s.Kind)
					}
					if s.Entry != ad.DefaultEntry() {
						return fmt.Errorf("seat %q: kind %q expects entry %q, got %q", s.ID, s.Kind, ad.DefaultEntry(), s.Entry)
					}
				}
				parsed = append(parsed, s)
			}
			if err := pact.Init(project, seats); err != nil {
				return err
			}
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			for _, s := range parsed {
				if s.Kind == "" || s.Kind == "shell" {
					continue
				}
				if err := agent.Wire(s.Kind, s.ID, strings.Join(s.Roles, ","), wd); err != nil {
					return err
				}
			}
			return nil
		}}
	initCmd.Flags().StringVar(&project, "project", "", "project name")
	initCmd.Flags().StringArrayVar(&seats, "seat", nil, "seat 'id:roles:entry[:kind]' (repeatable)")

	var joinRoles string
	joinCmd := &cobra.Command{Use: "join <id>", Args: cobra.ExactArgs(1), Short: "worker cold-start",
		RunE: func(_ *cobra.Command, a []string) error { return pact.Join(a[0], joinRoles) }}
	joinCmd.Flags().StringVar(&joinRoles, "roles", "", "comma-separated roles")

	var feature, branch, owner, reviewer, spec string
	assignCmd := &cobra.Command{Use: "assign <task>", Args: cobra.ExactArgs(1), Short: "assign a task",
		RunE: func(_ *cobra.Command, a []string) error {
			return pact.Assign(a[0], feature, branch, owner, reviewer, spec)
		}}
	assignCmd.Flags().StringVar(&feature, "feature", "", "feature id")
	assignCmd.Flags().StringVar(&branch, "branch", "", "feature branch")
	assignCmd.Flags().StringVar(&owner, "owner", "", "owner seat")
	assignCmd.Flags().StringVar(&reviewer, "reviewer", "", "reviewer seat")
	assignCmd.Flags().StringVar(&spec, "spec", "", "task spec path")

	var evidence string
	cpCmd := &cobra.Command{Use: "checkpoint <task>", Args: cobra.ExactArgs(1), Short: "submit for review",
		RunE: func(_ *cobra.Command, a []string) error { return pact.Checkpoint(a[0], evidence) }}
	cpCmd.Flags().StringVar(&evidence, "evidence", "", "evidence text")

	acceptCmd := &cobra.Command{Use: "accept <task>", Args: cobra.ExactArgs(1), Short: "reviewer accepts",
		RunE: func(_ *cobra.Command, a []string) error { return pact.Accept(a[0]) }}

	var reason string
	changesCmd := &cobra.Command{Use: "changes <task>", Args: cobra.ExactArgs(1), Short: "request changes",
		RunE: func(_ *cobra.Command, a []string) error { return pact.Changes(a[0], reason) }}
	changesCmd.Flags().StringVar(&reason, "reason", "", "reason")

	mergeCmd := &cobra.Command{Use: "merge <feature>", Args: cobra.ExactArgs(1), Short: "merge a feature",
		RunE: func(_ *cobra.Command, a []string) error { return pact.Merge(a[0]) }}

	statusCmd := &cobra.Command{Use: "status", Short: "print STATE.yml",
		RunE: func(c *cobra.Command, _ []string) error {
			s, err := pact.Status()
			if err != nil {
				return err
			}
			fmt.Fprint(c.OutOrStdout(), s)
			return nil
		}}

	var replay bool
	logCmd := &cobra.Command{Use: "log", Short: "print log or rebuild STATE",
		RunE: func(c *cobra.Command, _ []string) error {
			if replay {
				return pact.LogReplay()
			}
			s, err := pact.LogText()
			if err != nil {
				return err
			}
			fmt.Fprint(c.OutOrStdout(), s)
			return nil
		}}
	logCmd.Flags().BoolVar(&replay, "replay", false, "rebuild STATE.yml from the log")

	validateCmd := &cobra.Command{Use: "validate", Short: "check v1 conformance",
		RunE: func(_ *cobra.Command, _ []string) error { return pact.Validate() }}

	root.AddCommand(initCmd, joinCmd, assignCmd, cpCmd, acceptCmd, changesCmd, mergeCmd, statusCmd, logCmd, validateCmd,
		newRegisterCmd(), newUnregisterCmd(), newListCmd(), newServeCmd(), newMCPCmd(), newAgentCmd())
	return root
}
