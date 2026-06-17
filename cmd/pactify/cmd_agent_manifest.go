package main

import (
	"fmt"
	"os"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentmanifest"
	"github.com/spf13/cobra"
)

func newAgentManifestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "manifest", Short: "manage custom-agent manifests (~/.pactify/agents/*.toml)"}
	cmd.AddCommand(newManifestValidateCmd(), newManifestListCmd(), newManifestShowCmd(),
		newManifestAddCmd(), newManifestRemoveCmd())
	return cmd
}

func newManifestAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <file.toml>",
		Short: "validate + install a manifest into ~/.pactify/agents/",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, a []string) error {
			b, err := os.ReadFile(a[0])
			if err != nil {
				return err
			}
			kind, err := agentmanifest.Install(b)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "installed custom agent %q\n", kind)
			return nil
		},
	}
}

func newManifestRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <kind>",
		Short: "delete a custom manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, a []string) error {
			if err := agentmanifest.Remove(a[0]); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "removed custom agent %q\n", a[0])
			return nil
		},
	}
}

func newManifestValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file.toml>",
		Short: "parse + validate a manifest file",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, a []string) error {
			b, err := os.ReadFile(a[0])
			if err != nil {
				return err
			}
			m, err := agentmanifest.Parse(b)
			if err != nil {
				return err
			}
			if errs := agentmanifest.Validate(m); len(errs) != 0 {
				for _, e := range errs {
					fmt.Fprintln(c.OutOrStdout(), "  ✗ "+e)
				}
				return fmt.Errorf("manifest invalid (%d issue(s))", len(errs))
			}
			fmt.Fprintf(c.OutOrStdout(), "OK — %s (%s)\n", m.Kind, m.Binary)
			return nil
		},
	}
}

func newManifestListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list all kinds (built-in + custom)",
		RunE: func(c *cobra.Command, _ []string) error {
			ms, _ := agentmanifest.Load()
			custom := map[string]bool{}
			for _, m := range ms {
				custom[m.Kind] = true
			}
			for _, k := range agent.Kinds() {
				src := "built-in"
				if custom[k] {
					src = "custom"
				}
				fmt.Fprintf(c.OutOrStdout(), "%-16s %s\n", k, src)
			}
			return nil
		},
	}
}

func newManifestShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <kind>",
		Short: "print a custom manifest's TOML",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, a []string) error {
			p, err := agentmanifest.PathFor(a[0])
			if err != nil {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("no custom manifest for %q", a[0])
			}
			fmt.Fprint(c.OutOrStdout(), string(b))
			return nil
		},
	}
}
