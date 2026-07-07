package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/agentjoey/pactify/internal/audit"
	"github.com/agentjoey/pactify/internal/sarif"
	"github.com/spf13/cobra"
)

func newAuditSarifCmd() *cobra.Command {
	var project, seat, task, session, risk, since, out string
	c := &cobra.Command{
		Use:   "sarif",
		Short: "export audit records as SARIF 2.1.0 JSON",
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

			findings := make([]sarif.Finding, 0, len(recs))
			for _, r := range recs {
				findings = append(findings, recordToFinding(r))
			}

			log := sarif.Build("pactify", version, findings)
			buf, err := json.MarshalIndent(log, "", "  ")
			if err != nil {
				return err
			}
			buf = append(buf, '\n')

			w := cmd.OutOrStdout()
			if out != "" {
				if err := os.WriteFile(out, buf, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "wrote %d results to %s\n", len(findings), out)
				return nil
			}
			_, err = w.Write(buf)
			return err
		},
	}
	c.Flags().StringVar(&project, "project", "", "filter by project")
	c.Flags().StringVar(&seat, "seat", "", "filter by seat")
	c.Flags().StringVar(&task, "task", "", "filter by task")
	c.Flags().StringVar(&session, "session", "", "filter by session id")
	c.Flags().StringVar(&risk, "risk", "", "filter by risk (read|write|exec|mcp)")
	c.Flags().StringVar(&since, "since", "", "only records newer than this (e.g. 24h)")
	c.Flags().StringVar(&out, "out", "", "write SARIF to this file instead of stdout")
	return c
}

// recordToFinding maps an audit record to a SARIF finding.
func recordToFinding(r audit.Record) sarif.Finding {
	ruleID := "pact.audit." + r.Risk
	if r.Risk == "" {
		ruleID = "pact.audit.unknown"
	}
	msg := r.Summary
	if msg == "" {
		msg = r.Tool
	}
	return sarif.Finding{
		RuleID:  ruleID,
		Level:   riskLevel(r.Risk),
		Message: msg,
		Seat:    r.Seat,
		Task:    r.Task,
		Project: r.Project,
		TS:      r.TS,
	}
}

func riskLevel(risk string) string {
	switch risk {
	case "exec", "mcp":
		return "warning"
	default:
		return "note"
	}
}
