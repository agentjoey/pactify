package main

import (
	"fmt"

	"github.com/agentjoey/pactify/internal/registry"
	"github.com/spf13/cobra"
)

func newRegisterCmd() *cobra.Command {
	var name string
	reg := &cobra.Command{Use: "register <path>", Args: cobra.ExactArgs(1), Short: "register a .pact/ project for serve",
		RunE: func(_ *cobra.Command, a []string) error {
			r, err := registry.Load()
			if err != nil {
				return err
			}
			if err := r.Add(name, a[0], ""); err != nil {
				return err
			}
			return r.Save()
		}}
	reg.Flags().StringVar(&name, "name", "", "project name (default: basename slug)")
	return reg
}

func newUnregisterCmd() *cobra.Command {
	return &cobra.Command{Use: "unregister <name>", Args: cobra.ExactArgs(1), Short: "unregister a project",
		RunE: func(_ *cobra.Command, a []string) error {
			r, err := registry.Load()
			if err != nil {
				return err
			}
			if err := r.Remove(a[0]); err != nil {
				return err
			}
			return r.Save()
		}}
}

func newListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "list registered projects",
		RunE: func(c *cobra.Command, _ []string) error {
			r, err := registry.Load()
			if err != nil {
				return err
			}
			// Mark dead registrations inline. Without this the only signal that a
			// project's path is gone is an empty board in the dashboard, which reads
			// as a broken tool rather than a stale entry (see registry.Missing).
			for _, p := range r.Projects {
				suffix := ""
				if registry.Missing(p.Path) {
					suffix = "\t(missing — no .pact/ at this path; `pactify unregister " + p.Name + "` to remove)"
				}
				fmt.Fprintf(c.OutOrStdout(), "%s\t%s%s\n", p.Name, p.Path, suffix)
			}
			return nil
		}}
}
