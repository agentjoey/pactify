package sub

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"
)

// CostTotals distinguishes reported dollar costs from subscription and unknown
// pricing. A nil CostUSD means no job reported a dollar cost, rather than $0.
type CostTotals struct {
	Jobs             int64    `json:"jobs"`
	Succeeded        int64    `json:"succeeded"`
	Failed           int64    `json:"failed"`
	Canceled         int64    `json:"canceled"`
	Retries          int64    `json:"retries"`
	Fallbacks        int64    `json:"fallbacks"`
	InputTokens      int64    `json:"inputTokens"`
	OutputTokens     int64    `json:"outputTokens"`
	CostUSD          *float64 `json:"costUSD"`
	CostReportedJobs int64    `json:"costReportedJobs"`
	SubscriptionJobs int64    `json:"subscriptionJobs"`
	UnpricedJobs     int64    `json:"unpricedJobs"`
	UsagePartialJobs int64    `json:"usagePartialJobs"`
}

type CostGroup struct {
	Key string `json:"key"`
	CostTotals
}

type CostSkipped struct {
	MissingResult int64 `json:"missingResult"`
	InvalidResult int64 `json:"invalidResult"`
	InvalidState  int64 `json:"invalidState"`
}

type QuotaSnapshot struct {
	Harness    string    `json:"harness"`
	JobID      string    `json:"jobId"`
	ObservedAt time.Time `json:"observedAt"`
	RateLimit  string    `json:"rateLimit"`
}

// CostReport is the stable, read-only cost contract documented in sub-cost.md.
type CostReport struct {
	Since          time.Time       `json:"since"`
	Until          time.Time       `json:"until"`
	By             string          `json:"by"`
	Groups         []CostGroup     `json:"groups"`
	Total          CostTotals      `json:"total"`
	Skipped        CostSkipped     `json:"skipped"`
	QuotaSnapshots []QuotaSnapshot `json:"quotaSnapshots"`
}

// ParseCostSince uses inclusive local calendar days for Nd and elapsed hours for
// Nh. An explicit date starts at local midnight; 7d retains status's convention.
func ParseCostSince(value string, now time.Time) (time.Time, error) {
	invalid := fmt.Errorf("--since must be <N>d (calendar days), <N>h (elapsed hours), or YYYY-MM-DD on or before today; N must be a positive integer and the start date must fit the RFC3339 year range 0000-9999")
	if len(value) > 1 && (value[len(value)-1] == 'd' || value[len(value)-1] == 'h') {
		// All windows within the RFC3339 year range fit in 31 bits. This also
		// keeps day-count conversion safe on platforms with a 32-bit int.
		count, err := strconv.ParseUint(value[:len(value)-1], 10, 31)
		if err != nil || count == 0 || value[0] < '0' || value[0] > '9' {
			return time.Time{}, invalid
		}
		var since time.Time
		if value[len(value)-1] == 'd' {
			since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -int(count-1))
		} else {
			// Unix seconds avoid time.Duration overflow for long hour windows.
			since = time.Unix(now.Unix()-int64(count)*3600, int64(now.Nanosecond())).In(now.Location())
		}
		if since.Year() < 0 || since.Year() > 9999 {
			return time.Time{}, invalid
		}
		return since, nil
	}
	since, err := time.ParseInLocation("2006-01-02", value, now.Location())
	if err != nil || since.After(now) {
		return time.Time{}, invalid
	}
	return since, nil
}

// costResult preserves presence of costUSD without changing Result's historical
// float64/omitempty encoding, which is shared by workers, status and result.
type costResult struct {
	Result
	CostUSD *float64 `json:"costUSD"`
}

func (t *CostTotals) add(r costResult, harness string) {
	t.Jobs++
	if len(r.Retries) > 0 {
		t.Retries++
	}
	switch r.Status {
	case StatusSucceeded:
		t.Succeeded++
	case StatusFailed:
		t.Failed++
	case StatusCanceled:
		t.Canceled++
	}
	t.InputTokens += int64(r.InputTokens)
	t.OutputTokens += int64(r.OutputTokens)
	if r.UsagePartial {
		t.UsagePartialJobs++
	}
	if r.CostUSD != nil {
		if t.CostUSD == nil {
			t.CostUSD = new(float64)
		}
		*t.CostUSD += *r.CostUSD
		t.CostReportedJobs++
	} else {
		switch harness {
		case "codex", "kimi", "agy":
			t.SubscriptionJobs++
		default:
			t.UnpricedJobs++
		}
	}
}

// CostReport scans only state/result files. It deliberately does not Recover,
// acquire lifecycle locks, read logs, or create the store when it is absent.
// Quota snapshots span all history; cost totals and result skips use CreatedAt.
func (s Store) CostReport(since time.Time, by string, now time.Time) (CostReport, error) {
	report := CostReport{Since: since, Until: now, By: by, Groups: []CostGroup{}, QuotaSnapshots: []QuotaSnapshot{}}
	switch by {
	case "harness", "model", "repo", "day":
	default:
		return report, fmt.Errorf("--by must be harness, model, repo or day")
	}
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	groups := map[string]*CostTotals{}
	quotas := map[string]QuotaSnapshot{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		st, result, kind := s.readCostJob(entry.Name())
		if kind == "state" {
			report.Skipped.InvalidState++
			continue
		}
		inWindow := !st.CreatedAt.Before(since) && !st.CreatedAt.After(now)
		if kind != "" {
			if inWindow {
				if kind == "missing" {
					report.Skipped.MissingResult++
				} else {
					report.Skipped.InvalidResult++
				}
			}
			continue
		}
		harness := st.Request.Harness
		if harness == "" {
			harness = result.Harness
		}
		observed := st.FinishedAt
		if observed.IsZero() {
			observed = st.UpdatedAt
		}
		if observed.IsZero() {
			observed = st.CreatedAt
		}
		if result.RateLimit != "" && !observed.After(now) && !st.CreatedAt.After(now) {
			previous, found := quotas[harness]
			if !found || observed.After(previous.ObservedAt) || (observed.Equal(previous.ObservedAt) && st.ID > previous.JobID) {
				quotas[harness] = QuotaSnapshot{Harness: harness, JobID: st.ID, ObservedAt: observed, RateLimit: result.RateLimit}
			}
		}
		if !inWindow {
			continue
		}
		key := harness
		switch by {
		case "model":
			key = result.ModelActual
			if key == "" {
				key = result.ModelRequested
			}
			if key == "" {
				key = st.Request.Model
			}
		case "repo":
			key = JobRepo(st)
		case "day":
			key = st.CreatedAt.In(now.Location()).Format("2006-01-02")
		}
		if groups[key] == nil {
			groups[key] = &CostTotals{}
		}
		groups[key].add(result, harness)
		report.Total.add(result, harness)
	}
	for key, total := range groups {
		report.Groups = append(report.Groups, CostGroup{Key: key, CostTotals: *total})
	}
	sort.Slice(report.Groups, func(i, j int) bool { return report.Groups[i].Key < report.Groups[j].Key })
	for _, quota := range quotas {
		report.QuotaSnapshots = append(report.QuotaSnapshots, quota)
	}
	sort.Slice(report.QuotaSnapshots, func(i, j int) bool { return report.QuotaSnapshots[i].Harness < report.QuotaSnapshots[j].Harness })
	return report, nil
}

// Keep reads confined to the job directory, including inner symlinks. Atomic
// publication writes result before terminal state, so result owns final status.
func (s Store) readCostJob(id string) (State, costResult, string) {
	var st State
	var result costResult
	root, err := s.openJobRoot(id)
	if err != nil {
		return st, result, "state"
	}
	defer root.Close()
	data, err := root.ReadFile("state.json")
	if err != nil || json.Unmarshal(data, &st) != nil || st.ID != id || st.CreatedAt.IsZero() {
		return st, result, "state"
	}
	data, err = root.ReadFile("result.json")
	if os.IsNotExist(err) {
		return st, result, "missing"
	}
	if err != nil || json.Unmarshal(data, &result) != nil || !Terminal(result.Status) || (result.JobID != "" && result.JobID != id) || result.InputTokens < 0 || result.OutputTokens < 0 || (result.CostUSD != nil && *result.CostUSD < 0) {
		return st, result, "result"
	}
	return st, result, ""
}
