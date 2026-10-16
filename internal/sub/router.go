package sub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

type Route struct {
	Harness string `json:"harness"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
}

func (r Route) String() string {
	if r.Model != "" {
		return r.Harness + "/" + r.Model
	}
	return r.Harness
}
func (r Route) apply(req Request) Request {
	req.Harness, req.Model, req.Effort = r.Harness, r.Model, r.Effort
	return req
}

type QuotaGroup struct {
	Harness string   `json:"harness"`
	Models  []string `json:"models"`
}
type Router struct {
	Aliases     map[string]Route      `json:"aliases"`
	Fallback    map[string][]string   `json:"fallback"`
	QuotaGroups map[string]QuotaGroup `json:"quotaGroups"`
}

// Routing freezes the ordered remaining routes and group rules for a running job.
// A fallback list is one ordered chain, not a recursive graph of other lists.
type Routing struct {
	Remaining   []Route               `json:"remaining"`
	QuotaGroups map[string]QuotaGroup `json:"quotaGroups,omitempty"`
}

// RouterConfigError distinguishes invalid configuration from filesystem/store errors.
type RouterConfigError struct {
	Path, Field string
	Err         error
}

func (e *RouterConfigError) Error() string { return fmt.Sprintf("%s: %s: %v", e.Path, e.Field, e.Err) }
func (e *RouterConfigError) Unwrap() error { return e.Err }

var aliasName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func (s Store) LoadRouter() (*Router, error) {
	path := s.RouterPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseRouter(path, data)
}

// ParseRouter is the shared CLI and dashboard configuration validator.
func ParseRouter(path string, data []byte) (*Router, error) {
	fail := func(field string, err error) (*Router, error) {
		return nil, &RouterConfigError{Path: path, Field: field, Err: err}
	}
	// Decode each level separately so type and unknown-field errors name their field.
	decode := func(data []byte, v any) error {
		if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return fmt.Errorf("must not be null")
		}
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if err := d.Decode(v); err != nil {
			return err
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("expected a single JSON value")
		}
		return nil
	}
	var fields map[string]json.RawMessage
	if err := decode(data, &fields); err != nil {
		return fail("config", err)
	}
	r := &Router{Aliases: map[string]Route{}, Fallback: map[string][]string{}, QuotaGroups: map[string]QuotaGroup{}}
	for field, data := range fields {
		var entries map[string]json.RawMessage
		if field != "aliases" && field != "fallback" && field != "quotaGroups" {
			return fail(field, fmt.Errorf("unknown field"))
		}
		if err := decode(data, &entries); err != nil {
			return fail(field, err)
		}
		for name, raw := range entries {
			f := field + "." + name
			switch field {
			case "aliases":
				if !aliasName.MatchString(name) {
					return fail(f, fmt.Errorf("invalid alias name (omit @)"))
				}
				var route Route
				if err := decode(raw, &route); err != nil {
					return fail(f, err)
				}
				if _, ok := Lookup(route.Harness); !ok {
					return fail(f+".harness", fmt.Errorf("unknown harness %q", route.Harness))
				}
				r.Aliases[name] = route
			case "fallback":
				if _, ok := Lookup(name); !ok {
					return fail(f, fmt.Errorf("unknown harness %q", name))
				}
				var routes []string
				if err := decode(raw, &routes); err != nil {
					return fail(f, err)
				}
				r.Fallback[name] = routes
			case "quotaGroups":
				var group QuotaGroup
				if err := decode(raw, &group); err != nil {
					return fail(f, err)
				}
				if _, ok := Lookup(group.Harness); !ok {
					return fail(f+".harness", fmt.Errorf("unknown harness %q", group.Harness))
				}
				if len(group.Models) == 0 {
					return fail(f+".models", fmt.Errorf("must contain at least one model"))
				}
				for _, model := range group.Models {
					if strings.TrimSpace(model) == "" {
						return fail(f+".models", fmt.Errorf("model must not be empty"))
					}
				}
				r.QuotaGroups[name] = group
			}
		}
	}
	for name, list := range r.Fallback {
		for i, label := range list {
			if _, err := r.Resolve(label); err != nil {
				return fail(fmt.Sprintf("fallback.%s[%d]", name, i), err)
			}
		}
	}
	return r, nil
}
func (r *Router) Resolve(name string) (Route, error) {
	if strings.HasPrefix(name, "@") {
		if r != nil {
			if route, ok := r.Aliases[strings.TrimPrefix(name, "@")]; ok {
				return route, nil
			}
		}
		names := []string{}
		if r != nil {
			for name := range r.Aliases {
				names = append(names, "@"+name)
			}
		}
		sort.Strings(names)
		return Route{}, fmt.Errorf("unknown alias %q; available aliases: %s", name, strings.Join(names, ", "))
	}
	if _, ok := Lookup(name); !ok {
		return Route{}, fmt.Errorf("unknown harness %q; available: %s", name, strings.Join(Names(), ", "))
	}
	return Route{Harness: name}, nil
}
func (r *Router) Chain(first Route) []Route {
	routes := []Route{first}
	if r == nil {
		return routes
	}
	for _, label := range r.Fallback[first.Harness] {
		route, _ := r.Resolve(label)
		if !slices.Contains(routes, route) {
			routes = append(routes, route)
		}
	}
	return routes
}
func (r *Router) matchesQuota(route Route, q QuotaRoute) bool {
	if q.Harness != route.Harness {
		return false
	}
	if q.Model == "" || q.Model == route.Model {
		return true
	}
	if r != nil {
		for _, group := range r.QuotaGroups {
			if group.Harness == route.Harness && slices.Contains(group.Models, route.Model) && slices.Contains(group.Models, q.Model) {
				return true
			}
		}
	}
	return false
}

func (r *Router) exhausted(route Route, quotas []QuotaRoute, now time.Time) (bool, time.Time) {
	var reset time.Time
	for _, q := range quotas {
		if !q.active(now) || !r.matchesQuota(route, q) {
			continue
		}
		at := q.RecordedAt.Add(24 * time.Hour)
		if q.ResetAt != nil {
			at = *q.ResetAt
		}
		// All matching quota constraints must expire before this route is usable.
		if at.After(reset) {
			reset = at
		}
	}
	return !reset.IsZero(), reset
}

// A memory expiry is not a provider reset time. Report a countdown only when
// every active constraint on the route has a known reset.
func (r *Router) knownResetAt(route Route, quotas []QuotaRoute, now time.Time) *time.Time {
	var reset *time.Time
	for _, q := range quotas {
		if !q.active(now) || !r.matchesQuota(route, q) {
			continue
		}
		if q.ResetAt == nil {
			return nil
		}
		if reset == nil || q.ResetAt.After(*reset) {
			reset = q.ResetAt
		}
	}
	return reset
}

// FormatQuotaReset describes a known future reset without predicting unknown quotas.
func FormatQuotaReset(resetAt *time.Time, now time.Time) string {
	if resetAt == nil || !resetAt.After(now) {
		return ""
	}
	left := resetAt.Sub(now)
	hours := int(left.Hours())
	if hours >= 24 {
		return fmt.Sprintf("resets in %dd %dh", hours/24, hours%24)
	}
	if hours > 0 {
		return fmt.Sprintf("resets in %dh %dm", hours, int(left.Minutes())%60)
	}
	return fmt.Sprintf("resets in %dm", int(left.Minutes()))
}

func quotaFallbackWarning(from, to Route, resetAt *time.Time, now time.Time) string {
	warning := fmt.Sprintf("quota exhausted for %s; using %s", from, to)
	if reset := FormatQuotaReset(resetAt, now); reset != "" {
		warning += " (" + reset + ")"
	}
	return warning
}

func (r *Router) Select(routes []Route, quotas []QuotaRoute, access string, now time.Time) (Route, []Route, []string, error) {
	var earliest time.Time
	var warnings []string
	for i, route := range routes {
		if access == "read-only" && route.Harness != "codex" {
			continue
		}
		exhausted, reset := r.exhausted(route, quotas, now)
		if !exhausted {
			if i > 0 {
				warnings = append(warnings, quotaFallbackWarning(routes[0], route, r.knownResetAt(routes[0], quotas, now), now))
			}
			remaining := []Route{}
			for _, next := range routes[i+1:] {
				if access != "read-only" || next.Harness == "codex" {
					remaining = append(remaining, next)
				}
			}
			return route, remaining, warnings, nil
		}
		if earliest.IsZero() || reset.Before(earliest) {
			earliest = reset
		}
	}
	if earliest.IsZero() {
		return Route{}, nil, nil, fmt.Errorf("no routes support %s access", access)
	}
	return Route{}, nil, nil, fmt.Errorf("all routes exhausted; earliest reset: %s", earliest.UTC().Format(time.RFC3339))
}
