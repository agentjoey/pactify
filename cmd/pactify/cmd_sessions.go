package main

import (
	"fmt"
	"os/exec"

	"github.com/agentjoey/pactify/internal/sessions"
	"github.com/spf13/cobra"
)

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "manage agent session cleanup",
	}

	runner := func(dir, name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	listCmd := &cobra.Command{
		Use:   "list <kind>",
		Args:  cobra.ExactArgs(1),
		Short: "list agent sessions",
		RunE: func(c *cobra.Command, args []string) error {
			kind := args[0]
			m := sessions.Manager{Run: runner}
			out, err := m.List(kind)
			if err != nil {
				return fmt.Errorf("%v", err)
			}
			fmt.Fprint(c.OutOrStdout(), out)
			return nil
		},
	}

	pruneCmd := &cobra.Command{
		Use:   "prune <kind>",
		Args:  cobra.ExactArgs(1),
		Short: "prune agent sessions",
		RunE: func(c *cobra.Command, args []string) error {
			kind := args[0]
			m := sessions.Manager{Run: runner}
			out, skipped, err := m.Prune(kind)
			if err != nil {
				return err
			}
			if skipped {
				fmt.Fprintf(c.OutOrStdout(), "kind %s: session cleanup not supported (no verified command)\n", kind)
				return nil
			}
			fmt.Fprint(c.OutOrStdout(), out)
			return nil
		},
	}

	cmd.AddCommand(listCmd, pruneCmd)
	return cmd
}
