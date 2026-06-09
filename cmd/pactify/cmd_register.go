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
			if err := r.Add(name, a[0]); err != nil {
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
			for _, p := range r.Projects {
				fmt.Fprintf(c.OutOrStdout(), "%s\t%s\n", p.Name, p.Path)
			}
			return nil
		}}
}
