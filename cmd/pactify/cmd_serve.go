package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/registry"
	"github.com/agentjoey/pactify/internal/serve"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var addr string
	var extra []string
	var seat string
	var relayURL string
	var relayToken string
	cmd := &cobra.Command{Use: "serve", Short: "run the multi-project dashboard HTTP server",
		RunE: func(c *cobra.Command, _ []string) error {
			reg, err := registry.Load()
			if err != nil {
				return err
			}
			projects := reg.Projects
			if wd, err := os.Getwd(); err == nil {
				if _, err := os.Stat(filepath.Join(wd, ".pact")); err == nil {
					projects = appendIfNew(projects, registry.Project{Name: registry.Slug(filepath.Base(wd)), Path: wd})
				}
			}
			for _, p := range extra {
				abs, _ := filepath.Abs(p)
				projects = appendIfNew(projects, registry.Project{Name: registry.Slug(filepath.Base(abs)), Path: abs})
			}
			srv := serve.New(projects)
			srv.SetSeat(seat)
			srv.SetRelay(relayURL, relayToken)
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			if relayURL != "" {
				fmt.Fprintf(c.OutOrStdout(), "pactify serve on http://%s (%d project(s)) relay → %s\n", addr, len(projects), relayURL)
			} else {
				fmt.Fprintf(c.OutOrStdout(), "pactify serve on http://%s (%d project(s))\n", addr, len(projects))
			}
			return srv.Run(ctx, addr)
		}}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:7777", "listen address")
	cmd.Flags().StringArrayVar(&extra, "project", nil, "extra project path (repeatable, not persisted)")
	cmd.Flags().StringVar(&seat, "seat", os.Getenv("PACT_AGENT_ID"), "acting seat for author (write) endpoints (default $PACT_AGENT_ID)")
	cmd.Flags().StringVar(&relayURL, "relay-url", "", "best-effort relay POST endpoint")
	cmd.Flags().StringVar(&relayToken, "relay-token", os.Getenv("PACT_RELAY_TOKEN"), "bearer token for relay endpoint (default $PACT_RELAY_TOKEN)")
	return cmd
}

func appendIfNew(ps []registry.Project, p registry.Project) []registry.Project {
	for _, e := range ps {
		if e.Name == p.Name || e.Path == p.Path {
			return ps
		}
	}
	return append(ps, p)
}
