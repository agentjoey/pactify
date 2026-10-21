package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func newSubPruneCmd() *cobra.Command {
	var yes, jsonOut, include, verbose bool
	var older, cap string
	c := &cobra.Command{Use: "prune", Short: "Plan storage cleanup; --yes deletes eligible data", Args: subArgs(0), Run: func(c *cobra.Command, _ []string) {
		opts := sub.PruneOptions{IncludeWorktrees: include}
		if older != "" {
			d, err := time.ParseDuration(older)
			if err != nil || d <= 0 {
				subExit(c, 2, fmt.Errorf("--older-than must be a positive duration (for example 336h)"))
				return
			}
			opts.OlderThan = d
		}
		if cap != "" {
			n, err := pruneSize(cap)
			if err != nil {
				subExit(c, 2, err)
				return
			}
			opts.CacheMaxBytes = n
		}
		store, err := sub.DefaultStore()
		if err != nil {
			subExit(c, 1, err)
			return
		}
		if !yes {
			plan, err := store.PlanStorage(c.Context(), opts)
			if err != nil {
				subExit(c, 1, err)
				return
			}
			if jsonOut {
				err = subJSON(c, plan)
			} else {
				fmt.Fprintln(c.OutOrStdout(), "Storage cleanup plan (no changes; use --yes to execute)")
				for _, l := range plan.Locations {
					count := 0
					for _, i := range l.Items {
						if !i.Protected {
							count++
						}
					}
					fmt.Fprintf(c.OutOrStdout(), "%s: %s to delete, %s reclaimable (%s total)\n", l.Name, pruneItems(count), pruneBytes(l.Reclaimable), pruneBytes(l.Bytes))
					if !verbose {
						for _, g := range sub.SummarizeStorage(l.Items) {
							fmt.Fprintf(c.OutOrStdout(), "  %s: %s, %s — %s\n", g.Action, pruneItems(g.Count), pruneBytes(g.Bytes), g.Reason)
							for _, p := range g.Examples {
								fmt.Fprintf(c.OutOrStdout(), "    %s\n", p)
							}
						}
					} else {
						for _, i := range l.Items {
							action := "REMOVE"
							if i.Protected {
								action = "KEEP"
							} else if i.Action == "sweep-sessions" {
								action = "SWEEP SESSIONS"
							}
							fmt.Fprintf(c.OutOrStdout(), "  %s %s (%s): %s\n", action, i.Path, pruneBytes(i.Bytes), i.Reason)
						}
					}
				}
				for _, e := range plan.Failures {
					fmt.Fprintf(c.OutOrStdout(), "Failure: %s\n", e)
				}
			}
			if err != nil {
				subExit(c, 1, err)
			} else if len(plan.Failures) > 0 {
				subExit(c, 1, nil)
			}
			return
		}
		run, err := store.PruneStorage(c.Context(), opts)
		if jsonOut {
			if e := subJSON(c, run); err == nil {
				err = e
			}
		} else {
			for _, l := range run.Removed {
				fmt.Fprintf(c.OutOrStdout(), "%s: removed %s, freed %s%s\n", l.Location, pruneItems(l.Items), pruneBytes(l.Bytes), storageRemovalDetail(l))
			}
			for _, e := range run.Failures {
				fmt.Fprintf(c.OutOrStdout(), "Failure: %s\n", e)
			}
		}
		if err != nil {
			subExit(c, 1, err)
		} else if len(run.Failures) > 0 {
			subExit(c, 1, nil)
		}
	}}
	c.Flags().BoolVar(&verbose, "verbose", false, "List every planned item and protection reason")
	c.Flags().BoolVar(&yes, "yes", false, "Execute the cleanup plan (default is dry run)")
	c.Flags().StringVar(&older, "older-than", "", "Override retention duration, e.g. 336h")
	c.Flags().StringVar(&cap, "cache-max", "", "Override cache cap, e.g. 5GB or 5GiB")
	c.Flags().BoolVar(&include, "include-worktrees", false, "Discard eligible stale worktrees using the existing discard path")
	c.Flags().BoolVar(&jsonOut, "json", false, "Print JSON")
	return c
}
func newSubAutoPruneCmd() *cobra.Command {
	return &cobra.Command{Use: "__prune <job>", Hidden: true, Args: subArgs(1), Run: func(c *cobra.Command, args []string) {
		store, err := sub.DefaultStore()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Minute)
		defer cancel()
		_, _ = store.PruneStorageAfterJob(ctx, args[0])
	}}
}
func pruneSize(s string) (int64, error) {
	for _, u := range []struct {
		suffix string
		factor float64
	}{{"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}, {"B", 1}} {
		if v, ok := strings.CutSuffix(strings.ToUpper(strings.TrimSpace(s)), u.suffix); ok {
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil && n > 0 && !math.IsNaN(n) && !math.IsInf(n, 0) && n*u.factor < float64(math.MaxInt64) && n*u.factor >= 1 {
				return int64(n * u.factor), nil
			}
			return 0, fmt.Errorf("invalid --cache-max size %q", s)
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid --cache-max size %q", s)
	}
	return n, nil
}

func pruneItems(n int) string {
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	if n == 1 {
		return b.String() + " item"
	}
	return b.String() + " items"
}

func pruneBytes(n int64) string {
	if n < 0 {
		return "-" + pruneBytes(-n)
	}
	for _, u := range []struct {
		name string
		size int64
	}{{"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}} {
		if n >= u.size {
			return fmt.Sprintf("%.1f %s", float64(n)/float64(u.size), u.name)
		}
	}
	return fmt.Sprintf("%d B", n)
}

func storageRemovalDetail(l sub.StorageRemoved) string {
	detail := ""
	if l.Sessions > 0 || l.DatabaseMeasured {
		detail = fmt.Sprintf("; %d sessions deleted", l.Sessions)
	}
	if l.DatabaseMeasured {
		detail += "; database size delta: " + pruneBytes(l.DatabaseBytes)
	}
	return detail
}
