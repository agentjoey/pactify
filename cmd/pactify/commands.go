package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "pactify", Short: "pact protocol CLI", SilenceUsage: true, SilenceErrors: true}
	root.Version = versionString(version, commit, date)
	root.SetVersionTemplate("{{.Version}}\n")

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

	var joinRoles, joinKind, joinTask string
	joinCmd := &cobra.Command{Use: "join <id>", Args: cobra.ExactArgs(1), Short: "worker cold-start",
		RunE: func(_ *cobra.Command, a []string) error {
			return pact.At(".").JoinWithClientKindTask(a[0], joinRoles, "pactify-cli", pact.ClientVersion, joinKind, joinTask)
		}}
	joinCmd.Flags().StringVar(&joinRoles, "roles", "", "comma-separated roles")
	joinCmd.Flags().StringVar(&joinKind, "kind", "", "declared agent kind recorded on the roster (orchestrate resolves seat→kind from it; dynamic seats)")
	joinCmd.Flags().StringVar(&joinTask, "task", "", "target task id: lift exactly this task (refused with a task-specific error when unknown, not owned, or dep-blocked)")

	var feature, branch, owner, reviewer, spec string
	var reviewers []string
	var quorum int
	var deps []string
	assignCmd := &cobra.Command{Use: "assign <task>", Args: cobra.ExactArgs(1), Short: "assign a task",
		RunE: func(_ *cobra.Command, a []string) error {
			// Quorum multi-reviewer is strictly opt-in and mutually exclusive with the
			// single --reviewer: --reviewers names the reviewer set, --quorum how many
			// must accept. When neither quorum flag is used the call is byte-identical
			// to the historical single-reviewer assign.
			if len(reviewers) > 0 || quorum > 0 {
				if reviewer != "" {
					return fmt.Errorf("pactify assign: --reviewer is mutually exclusive with --reviewers/--quorum")
				}
				if len(reviewers) == 0 {
					return fmt.Errorf("pactify assign: --quorum requires --reviewers")
				}
				q := quorum
				if q == 0 {
					q = len(reviewers) // default to unanimous when --reviewers given without --quorum
				}
				return pact.AssignQuorum(a[0], feature, branch, owner, reviewers, q, spec, deps)
			}
			return pact.Assign(a[0], feature, branch, owner, reviewer, spec, deps)
		}}
	assignCmd.Flags().StringVar(&feature, "feature", "", "feature id")
	assignCmd.Flags().StringVar(&branch, "branch", "", "feature branch")
	assignCmd.Flags().StringVar(&owner, "owner", "", "owner seat")
	assignCmd.Flags().StringVar(&reviewer, "reviewer", "", "reviewer seat")
	assignCmd.Flags().StringSliceVar(&reviewers, "reviewers", nil, "comma-separated reviewer seats (quorum review; mutually exclusive with --reviewer)")
	assignCmd.Flags().IntVar(&quorum, "quorum", 0, "number of distinct reviewers that must accept (requires --reviewers; defaults to unanimous)")
	assignCmd.Flags().StringVar(&spec, "spec", "", "task spec path")
	assignCmd.Flags().StringSliceVar(&deps, "deps", nil, "comma-separated dep task ids (same feature)")

	var evidence string
	cpCmd := &cobra.Command{Use: "checkpoint <task>", Args: cobra.ExactArgs(1), Short: "submit for review",
		RunE: func(_ *cobra.Command, a []string) error { return pact.Checkpoint(a[0], evidence) }}
	cpCmd.Flags().StringVar(&evidence, "evidence", "", "evidence text")

	var acceptEvidence string
	acceptCmd := &cobra.Command{Use: "accept <task>", Args: cobra.ExactArgs(1), Short: "reviewer accepts",
		RunE: func(_ *cobra.Command, a []string) error { return pact.AcceptEvidence(a[0], acceptEvidence) }}
	acceptCmd.Flags().StringVar(&acceptEvidence, "evidence", "", "reviewer evidence backing the verdict (e.g. verify output summary); recorded on the accept event")

	var reason string
	changesCmd := &cobra.Command{Use: "changes <task>", Args: cobra.ExactArgs(1), Short: "request changes",
		RunE: func(_ *cobra.Command, a []string) error { return pact.Changes(a[0], reason) }}
	changesCmd.Flags().StringVar(&reason, "reason", "", "reason")

	var mergePush bool
	mergeCmd := &cobra.Command{Use: "merge <feature>", Args: cobra.ExactArgs(1), Short: "merge a feature",
		RunE: func(c *cobra.Command, a []string) error {
			if err := pact.Merge(a[0]); err != nil {
				return err
			}
			// Default: local merge only — origin advances only when the orchestrator
			// decides (spec coordination-authority P3-4). --push opts into pushing the
			// base branch (HEAD after a merge) to origin as part of this command.
			if mergePush {
				dir, err := os.Getwd()
				if err != nil {
					return err
				}
				base, err := gitx.CurrentBranch(dir)
				if err != nil || base == "" {
					return fmt.Errorf("merge --push: cannot resolve base branch to push: %w", err)
				}
				if err := gitx.Push(dir, "origin", base); err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "pushed %s to origin\n", base)
			}
			return nil
		}}
	mergeCmd.Flags().BoolVar(&mergePush, "push", false, "after a successful local merge, push the base branch to origin (default: local only — push when you decide)")

	cancelCmd := &cobra.Command{Use: "cancel <task>", Args: cobra.ExactArgs(1),
		Short: "retire a task (excluded from state; git untouched)",
		RunE:  func(_ *cobra.Command, a []string) error { return pact.Cancel(a[0]) }}

	withdrawCmd := &cobra.Command{Use: "withdraw <feature>", Args: cobra.ExactArgs(1),
		Short: "retire a whole feature (excluded from state; git untouched)",
		RunE:  func(_ *cobra.Command, a []string) error { return pact.Withdraw(a[0]) }}

	configCmd := &cobra.Command{Use: "config", Short: "project configuration"}
	configCmd.AddCommand(&cobra.Command{Use: "base-branch <branch>", Args: cobra.ExactArgs(1),
		Short: "set the integration base branch (corrects an init that captured a feature branch)",
		RunE:  func(_ *cobra.Command, a []string) error { return pact.ConfigBaseBranch(a[0]) }})
	configCmd.AddCommand(&cobra.Command{Use: "gate <command>", Args: cobra.ExactArgs(1),
		Short: "set the hard-gate command orchestrate runs before every merge (default: inferred from project type — pnpm/npm/cargo/go)",
		Long: `Set the project's hard test gate — the command orchestrate runs independently
before merging a feature whose tasks declare no per-task ` + "`verify:`" + ` line.

Without this, the gate defaults to one inferred from the project type:
  pnpm-lock.yaml → pnpm build && pnpm test
  package.json   → npm run build && npm test
  Cargo.toml     → cargo build && cargo test
  go.mod         → go build ./... && go test ./...

Example (build-first JS gate):
  pactify config gate "pnpm build && pnpm typecheck && pnpm lint && pnpm format:check && pnpm test"`,
		RunE: func(_ *cobra.Command, a []string) error { return pact.ConfigGate(a[0]) }})
	configCmd.AddCommand(&cobra.Command{Use: "critic <seat>", Args: cobra.ExactArgs(1),
		Short: "set the seat orchestrate runs as a read-only pre-review critic (default: off)",
		Long: `Set the project's pre-review critic seat — the seat orchestrate runs read-only
AFTER a task's verify gate is green and BEFORE its reviewer. The critic scores the
diff vs the spec (a trailing CRITIC_SCORE: 0.0-1.0 line); the score is injected
into the reviewer's briefing to steer attention.

The score has NO gating power: a low score never auto-bounces a task (that is the
verify gate's job). Off by default — set this (or pass orchestrate --critic) to
enable. Override per-run with: pactify orchestrate --critic seat=<seat>`,
		RunE: func(_ *cobra.Command, a []string) error { return pact.ConfigCritic(a[0]) }})

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

	var seatRoles, seatEntry, seatKind string
	seatAddCmd := &cobra.Command{Use: "add <id>", Args: cobra.ExactArgs(1), Short: "add a seat to the roster (orchestrator only)",
		RunE: func(c *cobra.Command, a []string) error {
			s := a[0] + ":" + seatRoles + ":" + seatEntry
			if seatKind != "" {
				s += ":" + seatKind
			}
			if err := pact.AddSeat(s); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "seat add: %q added to roster\n", a[0])
			return nil
		}}
	seatAddCmd.Flags().StringVar(&seatRoles, "roles", "", "comma-separated roles (orchestrator/reviewer/worker)")
	seatAddCmd.Flags().StringVar(&seatEntry, "entry", "AGENTS.md", "entry file for the seat's agent")
	seatAddCmd.Flags().StringVar(&seatKind, "kind", "", "agent kind (wiring + orchestrate seat-kind inference)")
	seatUseCmd := &cobra.Command{Use: "use <id>", Args: cobra.ExactArgs(1), Short: "bind this working copy's default seat",
		Long: "Write this checkout's default seat id to the untracked .pact/seat file\n" +
			"(excluded from git). The identity chain falls back to it when\n" +
			"PACT_AGENT_ID is unset, so a bare agent launch in this working copy acts\n" +
			"as that seat. Use a separate git worktree per concurrent seat.",
		RunE: func(c *cobra.Command, a []string) error {
			if err := pact.UseSeat(a[0]); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "seat: this working copy now defaults to %q (.pact/seat, git-excluded)\n", a[0])
			return nil
		}}
	seatCmd := &cobra.Command{Use: "seat", Short: "manage roster seats and this working copy's identity",
		RunE: func(c *cobra.Command, _ []string) error {
			id, source, err := pact.At(".").ResolveSeat()
			if err != nil {
				fmt.Fprintf(c.OutOrStdout(), "seat: unresolved — set PACT_AGENT_ID or run `pactify seat use <id>`\n")
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "seat: %s (source: %s)\n", id, source)
			return nil
		}}
	seatCmd.AddCommand(seatAddCmd, seatUseCmd)

	root.AddCommand(initCmd, joinCmd, assignCmd, cpCmd, acceptCmd, changesCmd, mergeCmd, cancelCmd, withdrawCmd, configCmd, statusCmd, logCmd, validateCmd, seatCmd,
		newRegisterCmd(), newUnregisterCmd(), newListCmd(), newServeCmd(), newMCPCmd(), newAgentCmd(), newVersionCmd(), newDoctorCmd(), newSetupCmd(), newRunCmd(), newOrchestrateCmd(), newPlanCmd(), newFinishCmd(), newSessionsCmd(), newRecipeCmd(), newAuditCmd(), newAccountCmd(), newScheduleCmd())
	return root
}
