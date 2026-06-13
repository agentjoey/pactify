package main

import (
	"fmt"

	"github.com/agentjoey/pactify/internal/recipe"
	"github.com/spf13/cobra"
)

func newRecipeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recipe",
		Short: "recipe task-graph templates",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "list available recipes",
		RunE: func(c *cobra.Command, _ []string) error {
			for _, name := range recipe.Names() {
				r, ok := recipe.Get(name)
				if !ok {
					continue
				}
				fmt.Fprintf(c.OutOrStdout(), "%-20s %s\n", r.Name, r.Description)
			}
			return nil
		},
	}

	var goal string
	showCmd := &cobra.Command{
		Use:   "show <name>",
		Args:  cobra.ExactArgs(1),
		Short: "show expanded recipe tasks",
		RunE: func(c *cobra.Command, args []string) error {
			if goal == "" {
				return fmt.Errorf("recipe: --goal is required for show")
			}
			r, ok := recipe.Get(args[0])
			if !ok {
				return fmt.Errorf("recipe: unknown recipe %q", args[0])
			}
			tasks, err := r.Expand(goal)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				fmt.Fprintf(c.OutOrStdout(), "--- task: %s\n", t.ID)
				if len(t.Deps) > 0 {
					fmt.Fprintf(c.OutOrStdout(), "deps: [%s]\n", joinDeps(t.Deps))
				}
				fmt.Fprintf(c.OutOrStdout(), "%s\n", t.Spec)
			}
			return nil
		},
	}
	showCmd.Flags().StringVar(&goal, "goal", "", "goal text to substitute into {{goal}}")

	cmd.AddCommand(listCmd, showCmd)
	return cmd
}

func joinDeps(deps []string) string {
	s := ""
	for i, d := range deps {
		if i > 0 {
			s += ", "
		}
		s += d
	}
	return s
}
