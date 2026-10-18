package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/agentjoey/pactify/internal/serve"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{Use: "serve", Short: "run the harness dashboard HTTP server",
		RunE: func(c *cobra.Command, _ []string) error {
			srv := serve.New()
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			fmt.Fprintf(c.OutOrStdout(), "pactify serve on http://%s\n", addr)
			return srv.Run(ctx, addr)
		}}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:7777", "listen address")
	return cmd
}
