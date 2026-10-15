package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

// A missing state file is only an unknown job if the job directory is absent.
// Existing jobs can have damaged or unreadable state; preserve those errors.
func subJobError(store sub.Store, id string, err error) (int, error) {
	if os.IsNotExist(err) {
		if _, statErr := os.Stat(store.Dir(id)); os.IsNotExist(statErr) {
			return 2, fmt.Errorf("unknown job %q: %w", id, err)
		}
	}
	return 1, err
}

type subStatusRow struct {
	sub.State
	Duration string `json:"duration"`
}

type subStatusListRow struct {
	JobID      string     `json:"jobId"`
	Status     sub.Status `json:"status"`
	Harness    string     `json:"harness"`
	Cwd        string     `json:"cwd"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

func subStatus(c *cobra.Command, ids []string, jsonOut bool) (int, error) {
	store, err := sub.DefaultStore()
	if err != nil {
		return 1, err
	}
	states := []sub.State{}
	if len(ids) > 0 {
		st, err := store.Recover(ids[0])
		if err != nil {
			return subJobError(store, ids[0], err)
		}
		states = append(states, st)
	} else {
		entries, err := os.ReadDir(store.Root)
		if err != nil && !os.IsNotExist(err) {
			return 1, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			st, err := store.Recover(entry.Name())
			if err != nil {
				return 1, err
			}
			states = append(states, st)
		}
		sort.Slice(states, func(i, j int) bool {
			if states[i].CreatedAt.Equal(states[j].CreatedAt) {
				return states[i].ID > states[j].ID
			}
			return states[i].CreatedAt.After(states[j].CreatedAt)
		})
	}
	now := time.Now()
	visible := states
	if len(visible) > 10 {
		visible = visible[:10]
	}
	rows := make([]subStatusRow, 0, len(visible))
	for _, st := range visible {
		end := st.FinishedAt
		if end.IsZero() {
			end = now
		}
		rows = append(rows, subStatusRow{st, end.Sub(st.CreatedAt).Round(time.Second).String()})
	}
	if jsonOut {
		if len(ids) > 0 {
			return 0, subJSON(c, rows[0])
		}
		list := make([]subStatusListRow, 0, len(rows))
		for _, row := range rows {
			item := subStatusListRow{JobID: row.ID, Status: row.Status, Harness: row.Request.Harness, Cwd: row.Request.Cwd, CreatedAt: row.CreatedAt}
			if !row.FinishedAt.IsZero() {
				finished := row.FinishedAt
				item.FinishedAt = &finished
			}
			list = append(list, item)
		}
		return 0, subJSON(c, list)
	}
	summary := ""
	if len(ids) == 0 {
		summary, err = subUsageSummary(store, states, now)
		if err != nil {
			return 1, err
		}
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "%s  %s  %s  %s\n", row.ID, row.Status, row.Request.Harness, row.Duration); err != nil {
			return 1, err
		}
	}
	if summary != "" {
		if _, err := fmt.Fprintln(c.OutOrStdout(), summary); err != nil {
			return 1, err
		}
	}
	return 0, nil
}

type subTokenTotal struct{ input, output int64 }

// Usage belongs to the job's creation day in local time. The seven-day window
// includes today and the preceding six calendar days, independent of the ten
// rows displayed by status. Results are atomically published before terminal state.
func subUsageSummary(store sub.Store, states []sub.State, now time.Time) (string, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := today.AddDate(0, 0, -6)
	tomorrow := today.AddDate(0, 0, 1)
	daily, weekly := map[string]subTokenTotal{}, map[string]subTokenTotal{}
	var latestCodex time.Time
	var quota string
	for _, st := range states {
		data, err := os.ReadFile(store.ResultPath(st.ID))
		if os.IsNotExist(err) && !sub.Terminal(st.Status) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("job %s result: %w", st.ID, err)
		}
		var result sub.Result
		if err := json.Unmarshal(data, &result); err != nil {
			return "", fmt.Errorf("job %s result: %w", st.ID, err)
		}
		harness := st.Request.Harness
		if harness == "codex" && (latestCodex.IsZero() || st.CreatedAt.After(latestCodex)) {
			latestCodex, quota = st.CreatedAt, result.RateLimit
		}
		if st.CreatedAt.Before(week) || !st.CreatedAt.Before(tomorrow) {
			continue
		}
		total := weekly[harness]
		total.input += int64(result.InputTokens)
		total.output += int64(result.OutputTokens)
		weekly[harness] = total
		if !st.CreatedAt.Before(today) {
			total = daily[harness]
			total.input += int64(result.InputTokens)
			total.output += int64(result.OutputTokens)
			daily[harness] = total
		}
	}
	format := func(totals map[string]subTokenTotal) string {
		names := make([]string, 0, len(totals))
		for name := range totals {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			total := totals[name]
			parts = append(parts, fmt.Sprintf("%s in %d / out %d", name, total.input, total.output))
		}
		if len(parts) == 0 {
			return "no usage"
		}
		return strings.Join(parts, "; ")
	}
	summary := "Tokens today: " + format(daily) + " · last 7 days: " + format(weekly)
	if quota != "" {
		summary += " · codex quota: " + strings.Join(strings.Fields(quota), " ")
	}
	return summary, nil
}
