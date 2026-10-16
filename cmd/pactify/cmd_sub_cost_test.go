package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func executeCost(args ...string) (string, error) {
	c := newSubCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append([]string{"cost"}, args...))
	err := c.Execute()
	return out.String(), err
}

func TestSubCostCLI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codex", "opencode", "agy"} {
		if err := os.MkdirAll(store.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		st := sub.State{ID: name, Request: sub.Request{Harness: name, Model: "model"}, Status: sub.StatusRunning, CreatedAt: time.Now().Add(-time.Minute)}
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}
		data := `{"status":"succeeded","inputTokens":100,"outputTokens":10,"rateLimit":"week 30%"}`
		if name == "opencode" {
			data = `{"status":"failed","inputTokens":20,"outputTokens":2,"costUSD":0.125,"usagePartial":true}`
		}
		if err := os.WriteFile(store.ResultPath(name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := executeCost()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"harness", "codex", "opencode", "agy", "TOTAL", "subscription", "$0.125000", "PARTIAL", "week 30%", "Skipped"} {
		if !strings.Contains(out, text) {
			t.Errorf("missing %q: %s", text, out)
		}
	}
	if strings.Contains(out, "$0.000000") {
		t.Fatalf("subscription shown as zero USD: %s", out)
	}
	if strings.Contains(out, "订阅") {
		t.Fatalf("non-English subscription label: %s", out)
	}
	out, err = executeCost("--json")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	keys := func(fields map[string]json.RawMessage) []string {
		var names []string
		for name := range fields {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}
	if !reflect.DeepEqual(keys(report), []string{"by", "groups", "quotaSnapshots", "since", "skipped", "total", "until"}) {
		t.Fatalf("report contract: %s", out)
	}
	if string(report["by"]) != `"harness"` {
		t.Fatalf("default grouping enum: %s", report["by"])
	}
	var total map[string]json.RawMessage
	if err := json.Unmarshal(report["total"], &total); err != nil {
		t.Fatal(err)
	}
	want := []string{"canceled", "costReportedJobs", "costUSD", "failed", "fallbacks", "inputTokens", "jobs", "outputTokens", "retries", "subscriptionJobs", "succeeded", "unpricedJobs", "usagePartialJobs"}
	if !reflect.DeepEqual(keys(total), want) || string(total["jobs"]) != "3" || string(total["usagePartialJobs"]) != "1" {
		t.Fatalf("total contract: %s", report["total"])
	}
	var groups []map[string]json.RawMessage
	if err := json.Unmarshal(report["groups"], &groups); err != nil {
		t.Fatal(err)
	}
	groupKeys := append(append([]string{}, want...), "key")
	sort.Strings(groupKeys)
	if len(groups) != 3 || !reflect.DeepEqual(keys(groups[0]), groupKeys) {
		t.Fatalf("group contract: %s", report["groups"])
	}
	var skipped map[string]json.RawMessage
	if err := json.Unmarshal(report["skipped"], &skipped); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keys(skipped), []string{"invalidResult", "invalidState", "missingResult"}) {
		t.Fatalf("skipped contract: %s", report["skipped"])
	}
	var snapshots []map[string]json.RawMessage
	if err := json.Unmarshal(report["quotaSnapshots"], &snapshots); err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || !reflect.DeepEqual(keys(snapshots[0]), []string{"harness", "jobId", "observedAt", "rateLimit"}) {
		t.Fatalf("quota contract: %s", report["quotaSnapshots"])
	}
	for _, by := range []string{"model", "repo", "day"} {
		out, err := executeCost("--by", by, "--since", time.Now().Format("2006-01-02"), "--json")
		if err != nil || !strings.Contains(out, `"by": "`+by+`"`) {
			t.Fatalf("flags: %s %v", out, err)
		}
	}
	// The command must leave the dead running jobs untouched and create no locks.
	for _, name := range []string{"codex", "opencode", "agy"} {
		st, err := store.Load(name)
		entries, readErr := os.ReadDir(store.Dir(name))
		if err != nil || readErr != nil || st.Status != sub.StatusRunning || len(entries) != 2 {
			t.Fatalf("mutated %s: %+v %v %v", name, st, err, readErr)
		}
	}
}

func TestSubCostTable(t *testing.T) {
	zero := 0.0
	for _, tt := range []struct {
		totals sub.CostTotals
		want   string
	}{
		{sub.CostTotals{SubscriptionJobs: 1}, "subscription"},
		{sub.CostTotals{CostUSD: &zero}, "$0.000000"},
		{sub.CostTotals{UnpricedJobs: 2}, "unknown (2)"},
		{sub.CostTotals{SubscriptionJobs: 1, UnpricedJobs: 2}, "subscription + unknown (2)"},
		{sub.CostTotals{CostUSD: &zero, SubscriptionJobs: 1, UnpricedJobs: 2}, "$0.000000 + subscription + unknown (2)"},
		{sub.CostTotals{}, "—"},
	} {
		if got := costCell(tt.totals); got != tt.want {
			t.Errorf("cost cell: %q, want %q", got, tt.want)
		}
	}
	var out bytes.Buffer
	total := sub.CostTotals{Jobs: 1, InputTokens: 12345, UsagePartialJobs: 1, SubscriptionJobs: 1}
	report := sub.CostReport{By: "model", Groups: []sub.CostGroup{{Key: "multi\nline\tmodel", CostTotals: total}, {Key: "", CostTotals: total}}, Total: total}
	if err := writeSubCost(&out, report); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	if !strings.HasPrefix(lines[1], "multi line model") || !strings.HasPrefix(lines[2], "(unknown)") || !strings.HasPrefix(lines[3], "TOTAL") {
		t.Fatalf("table rows: %s", out.String())
	}
	column := strings.Index(lines[0], "INPUT")
	for _, row := range lines[1:4] {
		if strings.Index(row, "12345") != column {
			t.Fatalf("unaligned table: %s", out.String())
		}
		if !strings.HasSuffix(row, "subscription") {
			t.Fatalf("non-English plan in table row: %s", row)
		}
	}
	if !strings.Contains(out.String(), "Quota snapshots: none recorded") {
		t.Fatalf("empty quota: %s", out.String())
	}
}

func TestSubCostErrorsAndEmptyStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	for _, args := range [][]string{{"--by", "invalid"}, {"--since", "invalid"}, {"unexpected"}} {
		if out, err := executeCost(args...); err == nil {
			t.Errorf("accepted %v: %s", args, out)
		}
	}
	out, err := executeCost("--json")
	if err != nil || !strings.Contains(out, `"groups": []`) {
		t.Fatalf("empty: %s %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(home, "jobs")); !os.IsNotExist(err) {
		t.Fatalf("created store: %v", err)
	}
}

func TestSubCostSinceFormatsAndHelp(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for id, at := range map[string]time.Time{"old": now.AddDate(0, 0, -20), "recent": now} {
		if err := os.MkdirAll(store.Dir(id), 0700); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(sub.State{ID: id, CreatedAt: at, Request: sub.Request{Harness: "codex"}}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.ResultPath(id), []byte(`{"status":"succeeded"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for value, jobs := range map[string]int64{"1d": 1, "3d": 1, "30d": 2, "24h": 1, "12h": 1, now.Format("2006-01-02"): 1} {
		t.Run(value, func(t *testing.T) {
			out, err := executeCost("--since", value, "--json")
			if err != nil {
				t.Fatal(err)
			}
			var report sub.CostReport
			if err := json.Unmarshal([]byte(out), &report); err != nil {
				t.Fatal(err)
			}
			if report.Total.Jobs != jobs {
				t.Fatalf("since %s: %s", value, out)
			}
		})
	}
	for _, value := range []string{"0d", "0h", "-1d", "-1h", "1w"} {
		out, err := executeCost("--since", value)
		if err == nil {
			t.Fatalf("accepted %q: %s", value, out)
		}
		for _, format := range []string{"<N>d", "<N>h", "YYYY-MM-DD"} {
			if !strings.Contains(err.Error(), format) {
				t.Errorf("error missing %q: %v", format, err)
			}
		}
	}
	out, err := executeCost("--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"<N>d", "<N>h", "YYYY-MM-DD", "positive integer", "7d"} {
		if !strings.Contains(out, text) {
			t.Errorf("help missing %q: %s", text, out)
		}
	}
}
