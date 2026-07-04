package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/agentjoey/pactify/internal/schedule"
	"github.com/spf13/cobra"
)

func newScheduleCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "schedule", Short: "scheduled/recurring orchestrate runs (machine-level, read by pactify serve)"}

	var at, feature string
	addCmd := &cobra.Command{Use: "add <project>", Args: cobra.ExactArgs(1),
		Short: "add a schedule (--at 'daily@HH:MM' | 'every:Nh' | 'every:Nm')",
		RunE: func(c *cobra.Command, a []string) error {
			if at == "" {
				return fmt.Errorf("--at is required (e.g. --at daily@03:00 or --at every:6h)")
			}
			// Reject an invalid expression immediately — before touching the store.
			if err := schedule.Validate(at); err != nil {
				return err
			}
			path := schedule.DefaultPath()
			scheds, err := schedule.Load(path)
			if err != nil {
				return err
			}
			id := nextScheduleID(scheds)
			scheds = append(scheds, schedule.Schedule{
				ID:      id,
				Project: a[0],
				Feature: feature,
				Expr:    at,
				Enabled: true,
			})
			if err := schedule.Save(path, scheds); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "schedule add: %s → %s %q\n", id, a[0], at)
			return nil
		}}
	addCmd.Flags().StringVar(&at, "at", "", "expression: daily@HH:MM (local) | every:Nh | every:Nm")
	addCmd.Flags().StringVar(&feature, "feature", "", "feature id to orchestrate (default: driver picks)")

	listCmd := &cobra.Command{Use: "list", Short: "list schedules",
		RunE: func(c *cobra.Command, _ []string) error {
			scheds, err := schedule.Load(schedule.DefaultPath())
			if err != nil {
				return err
			}
			if len(scheds) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "no schedules")
				return nil
			}
			now := time.Now()
			for _, s := range scheds {
				state := "enabled"
				if !s.Enabled {
					state = "disabled"
				}
				next := ""
				if nf, err := schedule.NextFire(now, s.Expr); err == nil {
					next = "  next=" + nf.Format(time.RFC3339)
				}
				feat := ""
				if s.Feature != "" {
					feat = "  feature=" + s.Feature
				}
				fmt.Fprintf(c.OutOrStdout(), "%s  %s  %s  [%s]%s%s\n", s.ID, s.Project, s.Expr, state, feat, next)
			}
			return nil
		}}

	removeCmd := &cobra.Command{Use: "remove <id>", Args: cobra.ExactArgs(1), Short: "remove a schedule by id",
		RunE: func(c *cobra.Command, a []string) error {
			path := schedule.DefaultPath()
			scheds, err := schedule.Load(path)
			if err != nil {
				return err
			}
			out := make([]schedule.Schedule, 0, len(scheds))
			found := false
			for _, s := range scheds {
				if s.ID == a[0] {
					found = true
					continue
				}
				out = append(out, s)
			}
			if !found {
				return fmt.Errorf("no schedule with id %q", a[0])
			}
			if err := schedule.Save(path, out); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "schedule remove: %s\n", a[0])
			return nil
		}}

	cmd.AddCommand(addCmd, listCmd, removeCmd)
	return cmd
}

// nextScheduleID returns the lowest unused "sN" id given the existing schedules.
func nextScheduleID(scheds []schedule.Schedule) string {
	used := map[string]bool{}
	for _, s := range scheds {
		used[s.ID] = true
	}
	for i := 1; ; i++ {
		id := "s" + strconv.Itoa(i)
		if !used[id] {
			return id
		}
	}
}
