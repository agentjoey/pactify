package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/agentjoey/pactify/internal/audit"
	"github.com/spf13/cobra"
)

func todayUTC() string { return time.Now().UTC().Format("2006-01-02") }

// newAuditCmd is the `pactify audit` group: the permission audit log — capture
// (hook), query (log/summary), retention (prune), and client wiring (install).
func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "permission audit log — capture, query, and manage per-seat tool-call records",
	}
	cmd.AddCommand(newAuditHookCmd(), newAuditLogCmd(), newAuditSummaryCmd(), newAuditPruneCmd(),
		newAuditInstallCmd(), newAuditUninstallCmd())
	return cmd
}

func newAuditInstallCmd() *cobra.Command {
	var claudeCode, opencode, detect bool
	c := &cobra.Command{
		Use:   "install [--claude-code] [--opencode] [--detect]",
		Short: "wire the audit PreToolUse hook into a client's project settings (.claude/settings.json)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if detect {
				for _, s := range audit.Detect(dir) {
					mark := "—"
					if s.Installed {
						mark = "installed"
					}
					fmt.Fprintf(out, "%-12s %s\n", s.Kind, mark)
				}
				return nil
			}
			did := false
			for kind, on := range map[string]bool{"claude-code": claudeCode, "opencode": opencode} {
				if !on {
					continue
				}
				if err := audit.Install(kind, dir); err != nil {
					return err
				}
				fmt.Fprintf(out, "installed audit hook for %s (%s/.claude/settings.json)\n", kind, dir)
				did = true
			}
			if !did {
				return fmt.Errorf("specify a client: --claude-code and/or --opencode (or --detect)")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&claudeCode, "claude-code", false, "install the hook for Claude Code")
	c.Flags().BoolVar(&opencode, "opencode", false, "install the hook for opencode")
	c.Flags().BoolVar(&detect, "detect", false, "show per-client install status instead of installing")
	return c
}

func newAuditUninstallCmd() *cobra.Command {
	var claudeCode, opencode bool
	c := &cobra.Command{
		Use:   "uninstall [--claude-code] [--opencode]",
		Short: "remove the audit PreToolUse hook from a client's project settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			for kind, on := range map[string]bool{"claude-code": claudeCode, "opencode": opencode} {
				if !on {
					continue
				}
				if err := audit.Uninstall(kind, dir); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "removed audit hook for %s\n", kind)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&claudeCode, "claude-code", false, "uninstall the hook for Claude Code")
	c.Flags().BoolVar(&opencode, "opencode", false, "uninstall the hook for opencode")
	return c
}

func newAuditLogCmd() *cobra.Command {
	var project, seat, task, session, risk, since string
	var asJSON bool
	var limit int
	c := &cobra.Command{
		Use:   "log",
		Short: "print recent audit records (newest first)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := audit.Filter{Project: project, Seat: seat, Task: task, Session: session, Risk: risk}
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("--since: %w", err)
				}
				f.Since = time.Now().Add(-d)
			}
			recs, err := audit.Query(f)
			if err != nil {
				return err
			}
			if limit > 0 && len(recs) > limit {
				recs = recs[:limit]
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				for _, r := range recs {
					_ = enc.Encode(r)
				}
				return nil
			}
			for _, r := range recs {
				fmt.Fprintf(out, "%s  %-12s %-9s %-6s %s/%s  %s\n", r.TS, r.Seat, r.Tool, r.Risk, r.Project, r.Task, r.Summary)
			}
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "filter by project")
	c.Flags().StringVar(&seat, "seat", "", "filter by seat")
	c.Flags().StringVar(&task, "task", "", "filter by task")
	c.Flags().StringVar(&session, "session", "", "filter by session id")
	c.Flags().StringVar(&risk, "risk", "", "filter by risk (read|write|exec|mcp)")
	c.Flags().StringVar(&since, "since", "", "only records newer than this (e.g. 24h)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit raw JSON lines")
	c.Flags().IntVar(&limit, "limit", 0, "cap the number of records (0 = all)")
	return c
}

func newAuditSummaryCmd() *cobra.Command {
	var project, since string
	c := &cobra.Command{
		Use:   "summary",
		Short: "counts by tool / risk / seat",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := audit.Filter{Project: project}
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("--since: %w", err)
				}
				f.Since = time.Now().Add(-d)
			}
			recs, err := audit.Query(f)
			if err != nil {
				return err
			}
			s := audit.Summarize(recs)
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "total %d\n", s.Total)
			fmt.Fprintf(out, "by risk: %v\n", s.ByRisk)
			fmt.Fprintf(out, "by tool: %v\n", s.ByTool)
			fmt.Fprintf(out, "by seat: %v\n", s.BySeat)
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "filter by project")
	c.Flags().StringVar(&since, "since", "", "window (e.g. 24h)")
	return c
}

func newAuditPruneCmd() *cobra.Command {
	var olderThan string
	c := &cobra.Command{
		Use:   "prune --older-than <dur>",
		Short: "delete audit day-files older than the given age (e.g. 720h)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if olderThan == "" {
				return fmt.Errorf("--older-than is required (e.g. 720h for 30 days)")
			}
			d, err := time.ParseDuration(olderThan)
			if err != nil {
				return fmt.Errorf("--older-than: %w", err)
			}
			n, err := audit.Prune(d)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pruned %d day-file(s)\n", n)
			return nil
		},
	}
	c.Flags().StringVar(&olderThan, "older-than", "", "age cutoff as a Go duration (e.g. 720h)")
	return c
}

// newAuditHookCmd is the PreToolUse hook entry a client invokes per tool call. It
// is best-effort and MUST NOT block the agent: any failure still exits 0.
func newAuditHookCmd() *cobra.Command {
	var kind string
	c := &cobra.Command{
		Use:    "hook --kind <kind>",
		Short:  "PreToolUse hook entry: read a tool call on stdin, record it, allow (exit 0)",
		Hidden: true, // wired by clients, not run by humans
		RunE: func(cmd *cobra.Command, _ []string) error {
			stdin, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return nil // never block the agent
			}
			rec, ok := audit.FromHook(kind, stdin, audit.EnvFromOS(), time.Now())
			if !ok {
				return nil
			}
			_ = audit.Append(rec) // swallow store errors — never block the agent
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "client kind firing the hook (claude-code | opencode)")
	return c
}
