package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type QuotaBucket struct {
	ID                string    `json:"id"`
	Name              string    `json:"name,omitempty"`
	Group             string    `json:"group,omitempty"`
	Window            string    `json:"window"`
	RemainingFraction float64   `json:"remainingFraction"`
	ResetAt           time.Time `json:"resetAt"`
}
type QuotaObservation struct {
	ObservedAt time.Time     `json:"observedAt"`
	Buckets    []QuotaBucket `json:"buckets"`
}

func (q QuotaObservation) Summary() string {
	var parts []string
	for _, b := range q.Buckets {
		parts = append(parts, fmt.Sprintf("%s: %.0f%% remaining, resets %s", b.ID, b.RemainingFraction*100, b.ResetAt.UTC().Format(time.RFC3339)))
	}
	return strings.Join(parts, "; ")
}
func (q QuotaObservation) ResetForModel(model string) *time.Time {
	var reset *time.Time
	for _, b := range q.Buckets {
		id := strings.ToLower(b.ID + " " + b.Group)
		matches := strings.Contains(model, "gemini") && strings.Contains(id, "gemini") && strings.Contains(model, "argon") == strings.Contains(id, "argon") ||
			(strings.HasPrefix(model, "claude") || strings.HasPrefix(model, "gpt")) && (strings.Contains(id, "claude") || strings.Contains(id, "gpt"))
		if matches && b.RemainingFraction <= 0 && !b.ResetAt.IsZero() && (reset == nil || b.ResetAt.After(*reset)) {
			r := b.ResetAt
			reset = &r
		}
	}
	return reset
}

// Quota never submits a model prompt: /usage is an agy built-in command.
func (b *agyBackend) Quota(ctx context.Context, o StartOpts) (QuotaObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := b.command(ctx, "-p", "/usage", "--model", AgyDefaultModel, "--output-format", "json")
	cmd.WaitDelay = time.Second
	cmd.Dir = o.RepoDir
	cmd.Env = append(os.Environ(), o.Env...)
	out, err := cmd.Output()
	if err != nil {
		return QuotaObservation{}, fmt.Errorf("agy /usage: %w", err)
	}
	var wire struct {
		Status  string `json:"status"`
		Turns   int    `json:"num_turns"`
		Command struct {
			Name string `json:"name"`
			Data struct {
				Groups []struct {
					Name    string            `json:"name"`
					Buckets []json.RawMessage `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if err = json.Unmarshal(out, &wire); err != nil {
		// agy may print an informational line before its structured command result.
		for _, line := range strings.Split(string(out), "\n") {
			if json.Unmarshal([]byte(line), &wire) == nil && wire.Command.Name == "usage" {
				err = nil
				break
			}
		}
		if err != nil {
			return QuotaObservation{}, fmt.Errorf("agy /usage JSON: %w", err)
		}
	}
	q := QuotaObservation{ObservedAt: time.Now().UTC(), Buckets: []QuotaBucket{}}
	if wire.Status != "SUCCESS" || wire.Turns != 0 || wire.Command.Name != "usage" {
		return q, fmt.Errorf("agy /usage returned an unexpected command result")
	}
	for _, g := range wire.Command.Data.Groups {
		for _, raw := range g.Buckets {
			var v struct {
				ID        string   `json:"id"`
				Name      string   `json:"name"`
				Window    string   `json:"window"`
				Remaining *float64 `json:"remaining_fraction"`
				Reset     string   `json:"reset_time"`
			}
			if json.Unmarshal(raw, &v) != nil {
				continue
			}
			reset, parseErr := time.Parse(time.RFC3339Nano, v.Reset)
			if v.ID == "" || v.Remaining == nil || *v.Remaining < 0 || *v.Remaining > 1 || parseErr != nil {
				continue
			}
			q.Buckets = append(q.Buckets, QuotaBucket{ID: v.ID, Name: v.Name, Group: g.Name, Window: v.Window, RemainingFraction: *v.Remaining, ResetAt: reset})
		}
	}
	if len(q.Buckets) == 0 {
		return q, fmt.Errorf("agy /usage returned no buckets")
	}
	return q, nil
}
