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

// Keep list jobId/cwd and the single-job State envelope for existing callers.
type subStatusListRow struct {
	sub.JobSummary
	JobID  string `json:"jobId"`
	Cwd    string `json:"cwd"`
	RunCwd string `json:"runCwd"`
}

func subStatusSingle(st sub.State, summary sub.JobSummary) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	data, err = json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(data, &extra); err != nil {
		return nil, err
	}
	for key, value := range extra {
		fields[key] = value
	}
	// The shared model uses actual cwd; CLI compatibility exposes it as runCwd.
	fields["cwd"], _ = json.Marshal(st.Request.Cwd)
	fields["runCwd"], _ = json.Marshal(summary.Cwd)
	return fields, nil
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
			if os.IsNotExist(err) {
				continue
			}
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
	peers := states
	if len(ids) > 0 {
		// Reverse links include every recorded child, even when only one job is shown.
		peers = append([]sub.State(nil), states...)
		entries, readErr := os.ReadDir(store.Root)
		if readErr != nil {
			return 1, readErr
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == ids[0] {
				continue
			}
			if st, loadErr := store.Load(entry.Name()); loadErr == nil {
				peers = append(peers, st)
			}
		}
	}
	repo, _ := c.Flags().GetString("repo")
	if repo != "" {
		target := sub.NormalizeRepo(repo)
		filtered := make([]sub.State, 0, len(states))
		for _, st := range states {
			if root := sub.JobRepo(st); root != "" && sub.NormalizeRepo(root) == target {
				filtered = append(filtered, st)
			}
		}
		states = filtered
		if len(ids) > 0 && len(states) == 0 {
			return 2, fmt.Errorf("job %s does not belong to repository %q", ids[0], repo)
		}
	}
	all, _ := c.Flags().GetBool("all")
	visible := states
	if !all && len(visible) > 10 {
		visible = visible[:10]
	}
	rows := make([]subStatusListRow, 0, len(visible))
	for _, st := range visible {
		summary := store.Summarize(st, peers, now)
		rows = append(rows, subStatusListRow{JobSummary: summary, JobID: st.ID, Cwd: st.Request.Cwd, RunCwd: summary.Cwd})
	}
	if jsonOut {
		if len(ids) > 0 {
			single, err := subStatusSingle(visible[0], rows[0].JobSummary)
			if err != nil {
				return 1, err
			}
			return 0, subJSON(c, single)
		}
		return 0, subJSON(c, rows)
	}

	summary := ""
	if len(ids) == 0 {
		summary, err = subUsageSummary(store, states, now)
		if err != nil {
			return 1, err
		}
	}
	for _, row := range rows {
		marker := ""
		if len(row.Attention) > 0 {
			marker = "  ⚠"
		}
		if _, err := fmt.Fprintf(c.OutOrStdout(), "%s  %s  %s  %s  %s%s\n", row.ID, row.Status, row.Harness, row.Duration, row.Title, marker); err != nil {
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
