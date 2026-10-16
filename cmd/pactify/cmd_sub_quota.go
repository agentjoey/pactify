package main

import (
	"fmt"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func newSubQuotaCmd() *cobra.Command {
	var jsonOut bool
	command := &cobra.Command{Use: "quota", Short: "Show currently exhausted harness/model routes", Args: subArgs(0), Run: func(c *cobra.Command, _ []string) { code, err := subQuota(c, jsonOut); subExit(c, code, err) }}
	command.Flags().BoolVar(&jsonOut, "json", false, "Print JSON")
	return command
}

func subQuota(c *cobra.Command, jsonOut bool) (int, error) {
	store, err := sub.DefaultStore()
	if err != nil {
		return 1, err
	}
	now := time.Now()
	routes, err := store.ExhaustedRoutes(now)
	if err != nil {
		return 1, err
	}
	if jsonOut {
		return 0, subJSON(c, routes)
	}
	_, err = fmt.Fprintln(c.OutOrStdout(), sub.FormatExhaustedRoutes(routes, now))
	return 0, err
}
