package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentcfg"
	"github.com/agentjoey/pactify/internal/agentreg"
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
	scan := &cobra.Command{Use: "scan", Short: "detect installed agents and show registration status",
		RunE: func(c *cobra.Command, args []string) error {
			reg, err := agentreg.Load()
			if err != nil {
				return err
			}
			for _, r := range agent.Scan() {
				mark := "—"
				if r.Installed {
					mark = "installed"
				}
				// Drivability (#8): can orchestrate launch this kind headlessly?
				drive := "manual"
				if agent.Drivable(r.Kind) {
					drive = "drivable"
				}
				tag := ""
				if reg.Has(r.Kind) {
					tag = "  [registered]"
					if eff, ok := agentcfg.Resolve(r.Kind); ok {
						scoped := ""
						if eff.Scoped {
							scoped = " scoped"
						}
						tag += fmt.Sprintf(" model=%s%s", eff.Model, scoped)
					}
				}
				fmt.Fprintf(c.OutOrStdout(), "%-15s %-10s %-9s %-40s%s\n", r.Kind, mark, drive, r.Detail, tag)
			}
			return nil
		}}

	var label string
	register := &cobra.Command{Use: "register <kind>", Args: cobra.ExactArgs(1),
		Short: "register an installed agent kind for orchestration",
		RunE: func(c *cobra.Command, args []string) error {
			kind := args[0]
			if _, ok := agent.Get(kind); !ok {
				return fmt.Errorf("unknown agent kind %q — known kinds: %s", kind, strings.Join(agent.Kinds(), ", "))
			}
			reg, err := agentreg.Load()
			if err != nil {
				return err
			}
			ts := time.Now().Format("2006-01-02T15:04:05Z07:00")
			if err := reg.Register(kind, label, ts); err != nil {
				return err
			}
			return reg.Save()
		}}
	register.Flags().StringVar(&label, "label", "", "human-readable label for this agent")

	unregister := &cobra.Command{Use: "unregister <kind>", Args: cobra.ExactArgs(1),
		Short: "unregister an agent kind from orchestration",
		RunE: func(c *cobra.Command, args []string) error {
			reg, err := agentreg.Load()
			if err != nil {
				return err
			}
			if err := reg.Unregister(args[0]); err != nil {
				return err
			}
			return reg.Save()
		}}

	var cfgModel, cfgTools string
	var cfgRestricted, cfgClear bool
	config := &cobra.Command{Use: "config <kind>", Args: cobra.ExactArgs(1),
		Short: "set per-agent launch overrides (model pin, scoped permissions)",
		Long: `config sets per-agent launch overrides used by orchestrate when it drives this
kind headlessly:

  --model         override the kind's default model pin (#10 per-agent model)
  --restricted    use scoped permissions: pass --allowed-tools instead of the
                  blanket auto-approve posture (#4/#9 scoped permissions)
  --allowed-tools comma-separated tool allowlist (effective with --restricted)
  --clear         remove all overrides for this kind

Only flags you pass are changed; others keep their current value. The agent must
be registered first (pactify agent register <kind>).`,
		RunE: func(c *cobra.Command, args []string) error {
			kind := args[0]
			reg, err := agentreg.Load()
			if err != nil {
				return err
			}
			if !reg.Has(kind) {
				return fmt.Errorf("%q is not registered — run `pactify agent register %s` first", kind, kind)
			}
			if cfgClear {
				if err := reg.SetConfig(kind, "", nil, false); err != nil {
					return err
				}
				if err := reg.Save(); err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "cleared overrides for %s\n", kind)
				return nil
			}
			// Only mutate fields whose flags were explicitly set, so a partial
			// `config --model X` doesn't wipe an existing scoped-permissions setting.
			curModel, curTools, curRestricted := reg.Config(kind)
			model, tools, restricted := curModel, curTools, curRestricted
			if c.Flags().Changed("model") {
				model = cfgModel
			}
			if c.Flags().Changed("allowed-tools") {
				tools = splitCSV(cfgTools)
			}
			if c.Flags().Changed("restricted") {
				restricted = cfgRestricted
			}
			if err := reg.SetConfig(kind, model, tools, restricted); err != nil {
				return err
			}
			if err := reg.Save(); err != nil {
				return err
			}
			if eff, ok := agentcfg.Resolve(kind); ok {
				fmt.Fprintf(c.OutOrStdout(), "%s → model=%s scoped=%v\n", kind, eff.Model, eff.Scoped)
			} else {
				fmt.Fprintf(c.OutOrStdout(), "%s configured (not headless-drivable; overrides stored)\n", kind)
			}
			return nil
		}}
	config.Flags().StringVar(&cfgModel, "model", "", "override the model pin")
	config.Flags().StringVar(&cfgTools, "allowed-tools", "", "comma-separated tool allowlist (with --restricted)")
	config.Flags().BoolVar(&cfgRestricted, "restricted", false, "scoped permissions (allowed-tools) instead of blanket auto-approve")
	config.Flags().BoolVar(&cfgClear, "clear", false, "clear all overrides for this kind")

	a.AddCommand(add, docs, scan, register, unregister, config)
	return a
}

// splitCSV splits a comma-separated list, trimming spaces and dropping empties.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
