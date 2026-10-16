package sub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func costFixture(t *testing.T, store Store, id, harness, model, repo string, at time.Time, result string) State {
	t.Helper()
	if err := os.MkdirAll(store.Dir(id), 0700); err != nil {
		t.Fatal(err)
	}
	st := State{ID: id, Request: Request{Harness: harness, Model: model}, Repo: repo, CreatedAt: at, UpdatedAt: at, FinishedAt: at, Status: StatusSucceeded}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if result != "" {
		if err := os.WriteFile(store.ResultPath(id), []byte(result), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestCostReportDimensions(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	loc := time.FixedZone("SGT", 8*3600)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, loc)
	since, err := ParseCostSince("7d", now)
	if err != nil {
		t.Fatal(err)
	}
	costFixture(t, store, "c1", "codex", "requested", "/repo/a", since, `{"status":"succeeded","modelActual":"gpt","inputTokens":100,"outputTokens":10,"rateLimit":"week 20%"}`)
	costFixture(t, store, "o1", "opencode", "flash", "/repo/a", now.AddDate(0, 0, -1), `{"status":"failed","inputTokens":200,"outputTokens":20,"costUSD":0.25,"usagePartial":true}`)
	st := costFixture(t, store, "a1", "agy", "argon", "/wrong", now, `{"status":"canceled","inputTokens":300,"outputTokens":30,"rateLimit":"quota exhausted"}`)
	st.Worktree = &Worktree{OriginalRoot: "/repo/b"}
	st.ResumeOf = "c1" // Manual resume is not the reserved automatic retry count.
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	costFixture(t, store, "c2", "codex", "gpt", "/repo/b", now.Add(-10*time.Hour).UTC(), `{"status":"succeeded","inputTokens":50,"outputTokens":5}`)
	costFixture(t, store, "o2", "opencode", "flash", "/repo/b", now, `{"status":"succeeded","costUSD":0}`)
	costFixture(t, store, "o3", "opencode", "", "", now, `{"status":"succeeded","inputTokens":5}`)
	old := costFixture(t, store, "old", "codex", "gpt", "/repo/a", since.Add(-time.Nanosecond), `{"status":"succeeded","inputTokens":9999,"rateLimit":"week 70%"}`)
	old.FinishedAt = now.Add(-time.Minute)
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	costFixture(t, store, "future", "codex", "gpt", "/repo/a", now.Add(time.Hour), `{"status":"succeeded","inputTokens":9999}`)
	wantKeys := map[string][]string{
		"harness": {"agy", "codex", "opencode"},
		"model":   {"", "argon", "flash", "gpt"},
		"repo":    {"", "/repo/a", "/repo/b"},
		"day":     {"2026-10-10", "2026-10-15", "2026-10-16"},
	}
	wantInput := map[string][]int64{
		"harness": {300, 150, 205},
		"model":   {5, 300, 200, 150},
		"repo":    {5, 300, 350},
		"day":     {100, 200, 355},
	}
	for by, keys := range wantKeys {
		t.Run(by, func(t *testing.T) {
			report, err := store.CostReport(since, by, now)
			if err != nil {
				t.Fatal(err)
			}
			total := report.Total
			if total.Jobs != 6 || total.Succeeded != 4 || total.Failed != 1 || total.Canceled != 1 || total.Retries != 0 || total.Fallbacks != 0 || total.InputTokens != 655 || total.OutputTokens != 65 || total.UsagePartialJobs != 1 || total.CostUSD == nil || *total.CostUSD != 0.25 || total.CostReportedJobs != 2 || total.SubscriptionJobs != 3 || total.UnpricedJobs != 1 {
				t.Fatalf("total: %+v", total)
			}
			var gotKeys []string
			var jobs, input, output, partial int64
			for i, group := range report.Groups {
				gotKeys = append(gotKeys, group.Key)
				if i >= len(wantInput[by]) || group.InputTokens != wantInput[by][i] {
					t.Fatalf("group input: %+v", report.Groups)
				}
				jobs += group.Jobs
				input += group.InputTokens
				output += group.OutputTokens
				partial += group.UsagePartialJobs
				if by == "harness" && group.Key == "codex" && (group.CostUSD != nil || group.SubscriptionJobs != 2 || group.InputTokens != 150) {
					t.Fatalf("subscription: %+v", group)
				}
			}
			if !reflect.DeepEqual(gotKeys, keys) || jobs != total.Jobs || input != total.InputTokens || output != total.OutputTokens || partial != total.UsagePartialJobs {
				t.Fatalf("groups: %+v", report.Groups)
			}
			if len(report.QuotaSnapshots) != 2 || report.QuotaSnapshots[0].Harness != "agy" || report.QuotaSnapshots[1].JobID != "old" || report.QuotaSnapshots[1].RateLimit != "week 70%" {
				t.Fatalf("snapshots: %+v", report.QuotaSnapshots)
			}
		})
	}
	// The date filter is inclusive at local midnight and independent of finish time.
	date, _ := ParseCostSince("2026-10-16", now)
	report, err := store.CostReport(date, "day", now)
	if err != nil || report.Total.Jobs != 4 || report.Total.InputTokens != 355 {
		t.Fatalf("date filter: %+v %v", report, err)
	}
}

func TestCostPricingAndModelFallbacks(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	now := time.Now()
	for _, fixture := range []struct{ id, harness, requested, result string }{
		{"actual", "codex", "requested", `{"status":"succeeded","modelActual":"actual","modelRequested":"reported"}`},
		{"reported", "kimi", "requested", `{"status":"failed","modelRequested":"reported"}`},
		{"requested", "agy", "requested", `{"status":"canceled"}`},
		{"zero", "opencode", "free", `{"status":"succeeded","costUSD":0}`},
		{"unknown", "opencode", "unknown", `{"status":"succeeded","costUSD":null}`},
	} {
		costFixture(t, store, fixture.id, fixture.harness, fixture.requested, "", now, fixture.result)
	}
	report, err := store.CostReport(now, "model", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total.CostUSD == nil || *report.Total.CostUSD != 0 || report.Total.CostReportedJobs != 1 || report.Total.SubscriptionJobs != 3 || report.Total.UnpricedJobs != 1 {
		t.Fatalf("pricing: %+v", report.Total)
	}
	var keys []string
	for _, group := range report.Groups {
		keys = append(keys, group.Key)
		if group.Key == "free" && (group.CostUSD == nil || *group.CostUSD != 0) {
			t.Fatal("lost explicitly reported zero")
		}
		if group.Key != "free" && group.CostUSD != nil {
			t.Fatalf("invented cost: %+v", group)
		}
	}
	if !reflect.DeepEqual(keys, []string{"actual", "free", "reported", "requested", "unknown"}) {
		t.Fatalf("model precedence: %v", keys)
	}
}

func TestCostSkipsBadFilesAndNeverWrites(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	now := time.Now()
	since := now.Add(-time.Hour)
	for id, data := range map[string]string{
		"missing": "", "broken": "{", "null": "null", "empty": "{}", "negative": `{"status":"succeeded","costUSD":-1}`,
		"mismatch": `{"jobId":"different","status":"succeeded"}`, "valid": `{"status":"succeeded","inputTokens":10}`,
	} {
		st := costFixture(t, store, id, "codex", "", "", now, data)
		// Cost must not recover a dead worker or require terminal state publication.
		st.Status, st.FinishedAt = StatusRunning, time.Time{}
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	costFixture(t, store, "bad-state", "agy", "", "", now, `{"status":"failed"}`)
	if err := os.WriteFile(filepath.Join(store.Dir("bad-state"), "state.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.Dir("no-state"), 0700); err != nil {
		t.Fatal(err)
	}
	// os.Root prevents following result links outside this job into other files.
	outside := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(outside, []byte(`{"status":"succeeded","inputTokens":9999}`), 0600); err != nil {
		t.Fatal(err)
	}
	costFixture(t, store, "linked", "codex", "", "", now, "")
	if err := os.Symlink(outside, store.ResultPath("linked")); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		files := map[string]string{}
		err := filepath.WalkDir(store.Root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				files[path] = string(data)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	before := snapshot()
	report, err := store.CostReport(since, "harness", now)
	if err != nil || report.Total.Jobs != 1 || report.Total.InputTokens != 10 || report.Skipped.MissingResult != 1 || report.Skipped.InvalidResult != 6 || report.Skipped.InvalidState != 2 {
		t.Fatalf("skips: %+v %v", report, err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("cost changed job files")
	}
}

func TestCostEmptyAndValidation(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "absent")}
	loc := time.FixedZone("SGT", 8*3600)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, loc)
	for value, want := range map[string]string{"7d": "2026-10-10T00:00:00+08:00", "2026-10-01": "2026-10-01T00:00:00+08:00"} {
		got, err := ParseCostSince(value, now)
		if err != nil || got.Format(time.RFC3339) != want {
			t.Fatalf("since %q: %v %v", value, got, err)
		}
	}
	for _, value := range []string{"", "yesterday", "-7d", "2026-02-30", "2026-1-1", "2026-10-17"} {
		if _, err := ParseCostSince(value, now); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if _, err := store.CostReport(now, "bogus", now); err == nil {
		t.Fatal("accepted invalid dimension")
	}
	report, err := store.CostReport(now, "harness", now)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["groups"]) != "[]" || string(fields["quotaSnapshots"]) != "[]" || report.Total.CostUSD != nil {
		t.Fatalf("empty JSON: %s", data)
	}
	if _, err := os.Stat(store.Root); !os.IsNotExist(err) {
		t.Fatalf("created store: %v", err)
	}
	if err := os.WriteFile(store.Root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CostReport(now, "harness", now); err == nil {
		t.Fatal("ignored store read failure")
	}
}

func TestParseCostSinceFormats(t *testing.T) {
	loc := time.FixedZone("SGT", 8*3600)
	now := time.Date(2026, 10, 16, 12, 34, 56, 123456789, loc)
	for _, tt := range []struct {
		value string
		want  time.Time
	}{
		{"1d", time.Date(2026, 10, 16, 0, 0, 0, 0, loc)},
		{"3d", time.Date(2026, 10, 14, 0, 0, 0, 0, loc)},
		{"7d", time.Date(2026, 10, 10, 0, 0, 0, 0, loc)},
		{"30d", time.Date(2026, 9, 17, 0, 0, 0, 0, loc)},
		{"003d", time.Date(2026, 10, 14, 0, 0, 0, 0, loc)},
		{"24h", now.Add(-24 * time.Hour)},
		{"12h", now.Add(-12 * time.Hour)},
		{"1h", now.Add(-time.Hour)},
		{"2629752h", time.Date(1726, 10, 16, 12, 34, 56, 123456789, loc)},
		{"2026-10-01", time.Date(2026, 10, 1, 0, 0, 0, 0, loc)},
		{"2026-10-16", time.Date(2026, 10, 16, 0, 0, 0, 0, loc)},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseCostSince(tt.value, now)
			if err != nil || !got.Equal(tt.want) || got.Location() != loc {
				t.Fatalf("since %q: %v %v, want %v", tt.value, got, err, tt.want)
			}
		})
	}
	for _, value := range []string{
		"", "0d", "00d", "0h", "-1d", "-24h", "+1d", "1w", "24H", "1m", "1.5h", "1d2h", "d", "h", "1", " 3d", "3d ",
		"999999999d", "999999999h", "18446744073709551616d", "18446744073709551616h", "2026-02-30", "2026-10-17",
	} {
		t.Run("invalid/"+value, func(t *testing.T) {
			if _, err := ParseCostSince(value, now); err == nil {
				t.Fatalf("accepted %q", value)
			} else {
				for _, format := range []string{"<N>d", "<N>h", "YYYY-MM-DD", "positive integer"} {
					if !strings.Contains(err.Error(), format) {
						t.Errorf("error missing %q: %v", format, err)
					}
				}
			}
		})
	}
}

func TestParseCostSinceDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name      string
		now       time.Time
		day, hour string
	}{
		{"spring", time.Date(2026, 3, 9, 1, 30, 0, 0, loc), "2026-03-08T00:00:00-05:00", "2026-03-08T00:30:00-05:00"},
		{"fall", time.Date(2026, 11, 2, 1, 30, 0, 0, loc), "2026-11-01T00:00:00-04:00", "2026-11-01T01:30:00-05:00"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			day, err := ParseCostSince("2d", tt.now)
			if err != nil || day.Format(time.RFC3339) != tt.day {
				t.Fatalf("calendar days: %v %v", day, err)
			}
			hour, err := ParseCostSince("24h", tt.now)
			if err != nil || hour.Format(time.RFC3339) != tt.hour || tt.now.Sub(hour) != 24*time.Hour {
				t.Fatalf("elapsed hours: %v %v", hour, err)
			}
		})
	}
}

func TestCostRelativeTimeFiltering(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	loc := time.FixedZone("SGT", 8*3600)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, loc)
	for id, at := range map[string]time.Time{
		"month": now.AddDate(0, 0, -20), "days": now.AddDate(0, 0, -2),
		"before-hours": now.Add(-24*time.Hour - time.Nanosecond), "hours": now.Add(-24 * time.Hour),
		"recent": now.Add(-6 * time.Hour), "future": now.Add(time.Hour),
	} {
		costFixture(t, store, id, "codex", "", "", at, `{"status":"succeeded","inputTokens":1}`)
	}
	for value, jobs := range map[string]int64{"1d": 1, "3d": 4, "7d": 4, "30d": 5, "24h": 2, "12h": 1, "2026-10-15": 3} {
		t.Run(value, func(t *testing.T) {
			since, err := ParseCostSince(value, now)
			if err != nil {
				t.Fatal(err)
			}
			report, err := store.CostReport(since, "harness", now)
			if err != nil || report.Total.Jobs != jobs || report.Total.InputTokens != jobs {
				t.Fatalf("filter %s: %+v %v", value, report.Total, err)
			}
		})
	}
}
