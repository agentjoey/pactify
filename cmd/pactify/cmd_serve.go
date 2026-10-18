package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/doctor"
	"github.com/agentjoey/pactify/internal/registry"
	"github.com/agentjoey/pactify/internal/serve"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var addr string
	var extra []string
	var seat string
	cmd := &cobra.Command{Use: "serve", Short: "run the harness dashboard HTTP server",
		RunE: func(c *cobra.Command, _ []string) error {
			for _, name := range []string{"relay-url", "relay-token", "remote-control"} {
				if c.Flags().Changed(name) {
					fmt.Fprintf(c.ErrOrStderr(), "--%s is ignored: the cloud relay was removed; remove it from your launch config\n", name)
				}
			}
			// Project discovery remains only for scheduled orchestrate runs.
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
			// Vendor CLI preflight: log any red per-vendor checks to stderr so a
			// misconfigured host (missing binary / unauthenticated CLI) is visible
			// at startup. Non-blocking — never aborts serve.
			home, _ := os.UserHomeDir()
			for _, ck := range doctor.VendorChecks(home, os.Getenv("PATH")) {
				if !ck.OK {
					fmt.Fprintf(os.Stderr, "pactify serve: preflight ✗ %s — %s\n", ck.Name, ck.Detail)
				}
			}

			srv := serve.New(projects)
			srv.SetSeat(seat)
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			// Scheduled/recurring orchestrate: a minute-granularity ticker reads
			// ~/.pactify/schedules.json and spawns orchestrate for due schedules
			// (conflict guard skips a project already running).
			go srv.StartScheduler(ctx)
			fmt.Fprintf(c.OutOrStdout(), "pactify serve on http://%s (%d project(s))\n", addr, len(projects))
			return srv.Run(ctx, addr)
		}}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:7777", "listen address")
	cmd.Flags().StringArrayVar(&extra, "project", nil, "extra project path for scheduled orchestrate runs (repeatable, not persisted)")
	cmd.Flags().StringVar(&seat, "seat", os.Getenv("PACT_AGENT_ID"), "acting seat for scheduled orchestrate runs (default $PACT_AGENT_ID)")
	// Keep retired launchd arguments accepted for one compatibility release.
	cmd.Flags().String("relay-url", "", "ignored: the cloud relay was removed")
	cmd.Flags().String("relay-token", "", "ignored: the cloud relay was removed")
	cmd.Flags().Bool("remote-control", false, "ignored: the cloud relay was removed")
	for _, name := range []string{"relay-url", "relay-token", "remote-control"} {
		_ = cmd.Flags().MarkHidden(name)
	}
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
