package main

import (
	"fmt"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func newSubLogsCmd() *cobra.Command {
	var tail int
	var follow, raw bool
	command := &cobra.Command{Use: "logs <job>", Short: "Show readable job events and harness stderr", Args: subArgs(1), Run: func(c *cobra.Command, args []string) {
		if tail < 0 {
			subExit(c, 2, fmt.Errorf("--tail must not be negative"))
			return
		}
		store, err := sub.DefaultStore()
		if err != nil {
			subExit(c, 1, err)
			return
		}
		options := sub.LogOptions{Tail: -1, Follow: follow, Raw: raw}
		if c.Flags().Changed("tail") {
			options.Tail = tail
		}
		if err := store.Logs(c.Context(), args[0], c.OutOrStdout(), options); err != nil {
			code, err := subJobError(store, args[0], err)
			subExit(c, code, err)
		}
	}}
	command.Flags().IntVar(&tail, "tail", 0, "Show only the last N display lines (default: all)")
	command.Flags().BoolVar(&follow, "follow", false, "Follow new lines until the job finishes")
	command.Flags().BoolVar(&raw, "raw", false, "Show each event separately, including message fragments and tool updates")
	return command
}
