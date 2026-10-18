package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/agentjoey/pactify/internal/doctor"
	"github.com/agentjoey/pactify/internal/serve"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{Use: "serve", Short: "run the harness dashboard HTTP server",
		RunE: func(c *cobra.Command, _ []string) error {
			for _, name := range []string{"relay-url", "relay-token", "remote-control", "seat", "project"} {
				if c.Flags().Changed(name) {
					removed := "cloud relay"
					if name == "seat" || name == "project" {
						removed = "orchestrator"
					}
					fmt.Fprintf(c.ErrOrStderr(), "--%s is ignored: the %s was removed; remove it from your launch config\n", name, removed)
				}
			}
			// Vendor CLI preflight: log any red per-vendor checks to stderr so a
			// misconfigured host (missing binary / unauthenticated CLI) is visible
			// at startup. Non-blocking — never aborts serve.
			home, _ := os.UserHomeDir()
			for _, ck := range doctor.VendorChecks(home, os.Getenv("PATH")) {
				if !ck.OK {
					fmt.Fprintf(os.Stderr, "pactify serve: preflight ✗ %s — %s\n", ck.Name, ck.Detail)
				}
			}

			srv := serve.New()
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			fmt.Fprintf(c.OutOrStdout(), "pactify serve on http://%s\n", addr)
			return srv.Run(ctx, addr)
		}}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:7777", "listen address")
	cmd.Flags().StringArray("project", nil, "ignored: the orchestrator was removed")
	cmd.Flags().String("seat", "", "ignored: the orchestrator was removed")
	// Keep retired launchd arguments accepted for one compatibility release.
	cmd.Flags().String("relay-url", "", "ignored: the cloud relay was removed")
	cmd.Flags().String("relay-token", "", "ignored: the cloud relay was removed")
	cmd.Flags().Bool("remote-control", false, "ignored: the cloud relay was removed")
	for _, name := range []string{"relay-url", "relay-token", "remote-control", "seat", "project"} {
		_ = cmd.Flags().MarkHidden(name)
	}
	return cmd
}
