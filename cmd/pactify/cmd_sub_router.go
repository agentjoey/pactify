package main

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func newSubRouterCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{Use: "router", Short: "Show router configuration and exhausted routes", Args: subArgs(0), Run: func(c *cobra.Command, _ []string) { code, err := subRouter(c, jsonOut); subExit(c, code, err) }}
	c.Flags().BoolVar(&jsonOut, "json", false, "Print JSON")
	return c
}
func subRouter(c *cobra.Command, jsonOut bool) (int, error) {
	store, err := sub.DefaultStore()
	if err != nil {
		return 1, err
	}
	router, err := store.LoadRouter()
	if err != nil {
		return subRouterError(err)
	}
	now := time.Now()
	quotas, err := store.ExhaustedRoutes(now)
	if err != nil {
		return 1, err
	}
	resolved := map[string][]sub.Route{}
	if router != nil {
		for name := range router.Fallback {
			resolved[name] = router.Chain(sub.Route{Harness: name})[1:]
		}
	}
	out := struct {
		Path             string                 `json:"path"`
		Config           *sub.Router            `json:"config"`
		ResolvedFallback map[string][]sub.Route `json:"resolvedFallback"`
		ExhaustedRoutes  []sub.QuotaRoute       `json:"exhaustedRoutes"`
		Warnings         []string               `json:"warnings"`
	}{filepath.Join(filepath.Dir(store.Root), "router.json"), router, resolved, quotas, router.IgnoredFallbacks()}
	if jsonOut {
		return 0, subJSON(c, out)
	}
	_, err = fmt.Fprint(c.OutOrStdout(), formatSubRouter(out.Path, router, quotas, now))
	return 0, err
}

func subRouterError(err error) (int, error) {
	var config *sub.RouterConfigError
	if errors.As(err, &config) {
		return 2, err
	}
	return 1, err
}

func formatSubRouter(path string, router *sub.Router, quotas []sub.QuotaRoute, now time.Time) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Config path: %s\n", path)
	if router == nil {
		out.WriteString("  (not configured; routing disabled)\n")
		router = &sub.Router{}
	}
	out.WriteString("\nDefaults:\n")
	if len(router.Defaults) == 0 {
		out.WriteString("  (none)\n")
	}
	for _, name := range slices.Sorted(maps.Keys(router.Defaults)) {
		d := router.Defaults[name]
		fmt.Fprintf(&out, "  %s: model=%s effort=%s\n", name, d.Model, d.Effort)
	}
	out.WriteString("\nAliases:\n")
	if len(router.Aliases) == 0 {
		out.WriteString("  (none)\n")
	}
	for _, name := range slices.Sorted(maps.Keys(router.Aliases)) {
		route := router.Aliases[name]
		fmt.Fprintf(&out, "  @%s → %s", name, route.Harness)
		if route.Model != "" {
			fmt.Fprintf(&out, " %s", route.Model)
		}
		if route.Effort != "" {
			fmt.Fprintf(&out, " [%s]", route.Effort)
		}
		out.WriteByte('\n')
	}
	out.WriteString("\nFallback:\n")
	if len(router.Fallback) == 0 {
		out.WriteString("  (none)\n")
	}
	for _, name := range slices.Sorted(maps.Keys(router.Fallback)) {
		routes := strings.Join(router.Fallback[name], ", ")
		if routes == "" {
			routes = "(none)"
		}
		fmt.Fprintf(&out, "  %s → %s\n", name, routes)
	}
	out.WriteString("\nWarnings:\n")
	warnings := router.IgnoredFallbacks()
	if len(warnings) == 0 {
		out.WriteString("  (none)\n")
	}
	for _, warning := range warnings {
		fmt.Fprintf(&out, "  %s\n", warning)
	}
	out.WriteString("\nQuota groups:\n")
	if len(router.QuotaGroups) == 0 {
		out.WriteString("  (none)\n")
	}
	for _, name := range slices.Sorted(maps.Keys(router.QuotaGroups)) {
		group := router.QuotaGroups[name]
		fmt.Fprintf(&out, "  %s → %s: %s\n", name, group.Harness, strings.Join(group.Models, ", "))
	}
	out.WriteString("\nExhausted routes:\n")
	if len(quotas) == 0 {
		out.WriteString("  (none)\n")
	}
	for _, quota := range quotas {
		route := sub.Route{Harness: quota.Harness, Model: quota.Model}
		if quota.ResetAt != nil {
			fmt.Fprintf(&out, "  %s (%s; reset at %s)\n", route, sub.FormatQuotaReset(quota.ResetAt, now), quota.ResetAt.UTC().Format(time.RFC3339))
		} else {
			fmt.Fprintf(&out, "  %s (reset time unknown; memory expires at %s)\n", route, quota.RecordedAt.Add(24*time.Hour).UTC().Format(time.RFC3339))
		}
	}
	return out.String()
}
