package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func newSubCostCmd() *cobra.Command {
	var since, by string
	var jsonOut bool
	command := &cobra.Command{Use: "cost", Short: "Summarize job tokens, reported costs and quota snapshots (read-only)", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		now := time.Now()
		start, err := sub.ParseCostSince(since, now)
		if err != nil {
			return err
		}
		store, err := sub.DefaultStore()
		if err != nil {
			return err
		}
		report, err := store.CostReport(start, by, now)
		if err != nil {
			return err
		}
		if jsonOut {
			return subJSON(c, report)
		}
		return writeSubCost(c.OutOrStdout(), report)
	}}
	command.Flags().StringVar(&since, "since", "7d", "Include jobs created since <N>d (calendar days including today), <N>h (elapsed hours), or YYYY-MM-DD; N is a positive integer")
	command.Flags().StringVar(&by, "by", "harness", "Group by harness, model, repo or day")
	command.Flags().BoolVar(&jsonOut, "json", false, "Print JSON")
	return command
}

func costCell(total sub.CostTotals) string {
	var parts []string
	if total.CostUSD != nil {
		parts = append(parts, fmt.Sprintf("$%.6f", *total.CostUSD))
	}
	if total.SubscriptionJobs > 0 {
		parts = append(parts, "subscription")
	}
	if total.UnpricedJobs > 0 {
		parts = append(parts, fmt.Sprintf("unknown (%d)", total.UnpricedJobs))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " + ")
}

// Keep the subscription label in the final column, and collapse newlines/tabs
// from persisted metadata so each group occupies exactly one table row.
func writeSubCost(out io.Writer, report sub.CostReport) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(w, "%s\tJOBS\tOK\tFAILED\tCANCELED\tRETRIES\tFALLBACKS\tINPUT\tOUTPUT\tPARTIAL\tUSD / PLAN\n", report.By); err != nil {
		return err
	}
	row := func(key string, total sub.CostTotals) error {
		_, err := fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n", strings.Join(strings.Fields(key), " "), total.Jobs, total.Succeeded, total.Failed, total.Canceled, total.Retries, total.Fallbacks, total.InputTokens, total.OutputTokens, total.UsagePartialJobs, costCell(total))
		return err
	}
	for _, group := range report.Groups {
		key := group.Key
		if key == "" {
			key = "(unknown)"
		}
		if err := row(key, group.CostTotals); err != nil {
			return err
		}
	}
	if err := row("TOTAL", report.Total); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Since %s through %s (job creation time)\nSkipped: missing result %d, invalid/unreadable result %d, invalid/missing state %d\nPARTIAL counts jobs with incomplete usage; tokens and USD include only recorded usage.\n", report.Since.Format(time.RFC3339), report.Until.Format(time.RFC3339), report.Skipped.MissingResult, report.Skipped.InvalidResult, report.Skipped.InvalidState); err != nil {
		return err
	}
	if len(report.QuotaSnapshots) == 0 {
		_, err := fmt.Fprintln(out, "Quota snapshots: none recorded")
		return err
	}
	for _, quota := range report.QuotaSnapshots {
		if _, err := fmt.Fprintf(out, "Quota %s [%s, %s]: %s\n", strings.Join(strings.Fields(quota.Harness), " "), strings.Join(strings.Fields(quota.JobID), " "), quota.ObservedAt.Format(time.RFC3339), strings.Join(strings.Fields(quota.RateLimit), " ")); err != nil {
			return err
		}
	}
	return nil
}
