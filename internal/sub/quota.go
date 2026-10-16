package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/lockx"
)

// QuotaRoute is evidence of an exhausted route, not a quota prediction.
// A nil ResetAt expires 24 hours after RecordedAt.
type QuotaRoute struct {
	Harness    string     `json:"harness"`
	Model      string     `json:"model,omitempty"`
	ResetAt    *time.Time `json:"resetAt"`
	JobID      string     `json:"jobId"`
	RecordedAt time.Time  `json:"recordedAt"`
	Message    string     `json:"message"`
}

func (q QuotaRoute) key() string {
	if q.Model == "" {
		return q.Harness
	}
	return q.Harness + "/" + q.Model
}
func (q QuotaRoute) active(now time.Time) bool {
	expires := q.RecordedAt.Add(24 * time.Hour)
	if q.ResetAt != nil {
		expires = *q.ResetAt
	}
	return now.Before(expires)
}
func (s Store) quotaPath() string { return filepath.Join(filepath.Dir(s.Root), "quota.json") }
func (s Store) readQuota(now time.Time) (map[string]QuotaRoute, error) {
	entries := map[string]QuotaRoute{}
	data, err := os.ReadFile(s.quotaPath())
	if os.IsNotExist(err) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("read quota memory: %w", err)
	}
	if entries == nil {
		entries = map[string]QuotaRoute{}
	}
	for key, q := range entries {
		if !q.active(now) {
			delete(entries, key)
		}
	}
	return entries, nil
}

// ExhaustedRoutes reads an atomic snapshot and ignores expired entries. It does
// not mutate disk; writers prune expired entries under the quota file lock.
func (s Store) ExhaustedRoutes(now time.Time) ([]QuotaRoute, error) {
	entries, err := s.readQuota(now)
	if err != nil {
		return nil, err
	}
	routes := make([]QuotaRoute, 0, len(entries))
	for _, q := range entries {
		routes = append(routes, q)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].key() < routes[j].key() })
	return routes, nil
}

func (s Store) rememberQuota(q QuotaRoute) error {
	release, err := lockx.Acquire(context.Background(), s.quotaPath()+".lock")
	if err != nil {
		return err
	}
	defer release()
	entries, err := s.readQuota(q.RecordedAt)
	if err != nil {
		return err
	}
	q.Message = strings.Join(strings.Fields(q.Message), " ")
	runes := []rune(q.Message)
	if len(runes) > 512 {
		q.Message = string(runes[:511]) + "…"
	}
	if previous, ok := entries[q.key()]; !ok || !previous.RecordedAt.After(q.RecordedAt) {
		if q.active(q.RecordedAt) {
			entries[q.key()] = q
		}
	}
	return atomicJSON(s.quotaPath(), entries)
}

// FailureStatus is shared by CLI status and result rendering.
func FailureStatus(status Status, class string, reset *time.Time, now time.Time) string {
	if class == "" || status == StatusSucceeded || !Terminal(status) {
		return string(status)
	}
	detail := class
	if class == "quota" && reset != nil && reset.After(now) {
		hours := int(reset.Sub(now).Hours())
		if hours >= 24 {
			detail += fmt.Sprintf(", resets in %dd %dh", hours/24, hours%24)
		} else if hours > 0 {
			detail += fmt.Sprintf(", resets in %dh %dm", hours, int(reset.Sub(now).Minutes())%60)
		} else {
			detail += fmt.Sprintf(", resets in %dm", int(reset.Sub(now).Minutes()))
		}
	}
	return string(status) + " (" + detail + ")"
}

func FormatExhaustedRoutes(routes []QuotaRoute, now time.Time) string {
	parts := make([]string, 0, len(routes))
	for _, q := range routes {
		label := q.key()
		if q.ResetAt != nil {
			label += " · " + FailureStatus(StatusFailed, "quota", q.ResetAt, now)
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "Exhausted routes: none"
	}
	return "Exhausted routes: " + strings.Join(parts, "; ")
}
