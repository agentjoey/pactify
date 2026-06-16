package main

import (
	"io"
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
	cmd.AddCommand(newAuditHookCmd())
	return cmd
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
