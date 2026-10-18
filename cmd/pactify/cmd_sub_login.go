package main

import (
	"fmt"
	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func newSubLoginCmd() *cobra.Command {
	return &cobra.Command{Use: "login opencode [target] [-- extra args]", Short: "Sign in to pactify's private opencode 2.x home", Long: "Sign in with opencode auth login --standalone. Opencode 2.x credentials and delegated sessions are stored in PACTIFY_HOME/harness-homes/opencode2/share/opencode/opencode.db. New jobs wait for this login to finish (up to two minutes). Opencode 1.x uses your own auth.json.", Args: func(_ *cobra.Command, args []string) error {
		if len(args) == 0 || args[0] != "opencode" {
			return fmt.Errorf("login supports opencode only")
		}
		return nil
	}, RunE: func(c *cobra.Command, args []string) error {
		return sub.OpencodeLogin(c.Context(), args[1:], c.InOrStdin(), c.OutOrStdout(), c.ErrOrStderr())
	}}
}
