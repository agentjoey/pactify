package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func statusFixture(t *testing.T, id, harness string, created time.Time, result *sub.Result) (sub.Store, sub.State) {
	t.Helper()
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Create(id, sub.Request{Harness: harness, Cwd: "/workspace/" + id, Access: "write"})
	if err != nil {
		t.Fatal(err)
	}
	err = store.WithState(context.Background(), id, func(current *sub.State) error {
		current.CreatedAt = created
		st = *current
		return store.Save(*current)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		result.JobID, result.Harness = id, harness
		if err := store.Finish(id, result, "", created.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	return store, st
}

func captureStatus(t *testing.T, ids []string, asJSON bool) (string, int, error) {
	t.Helper()
	c := &cobra.Command{}
	c.SetContext(context.Background())
	var out bytes.Buffer
	c.SetOut(&out)
	code, err := subStatus(c, ids, asJSON)
	return out.String(), code, err
}

func TestSubStatusListJSONContract(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	now := time.Now()
	statusFixture(t, "older", "kimi", now.Add(-time.Hour), &sub.Result{Status: sub.StatusSucceeded})
	statusFixture(t, "newer", "codex", now, nil)
	out, code, err := captureStatus(t, nil, true)
	if code != 0 || err != nil {
		t.Fatalf("status: %d %v %s", code, err, out)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || string(rows[0]["jobId"]) != `"newer"` || string(rows[1]["jobId"]) != `"older"` {
		t.Fatalf("wrong order/ids: %s", out)
	}
	for _, key := range []string{"jobId", "status", "harness", "cwd", "createdAt"} {
		if _, ok := rows[0][key]; !ok {
			t.Errorf("missing %s: %s", key, out)
		}
	}
	if len(rows[0]) != 5 || len(rows[1]) != 6 {
		t.Errorf("unexpected list fields: %s", out)
	}
	if _, ok := rows[0]["finishedAt"]; ok {
		t.Errorf("unfinished job has finishedAt: %s", out)
	}
	// Existing object shape and fields must remain available to callers.
	out, code, err = captureStatus(t, []string{"newer"}, true)
	var single map[string]json.RawMessage
	if code != 0 || err != nil || json.Unmarshal([]byte(out), &single) != nil {
		t.Fatalf("single: %d %v %s", code, err, out)
	}
	for _, key := range []string{"id", "request", "createdAt", "updatedAt", "finishedAt", "duration"} {
		if _, ok := single[key]; !ok {
			t.Errorf("single lost %s: %s", key, out)
		}
	}
	if _, ok := single["jobId"]; ok {
		t.Errorf("changed single shape: %s", out)
	}
}

func TestSubStatusUsageIncludesJobsOutsideDisplayLimit(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	for i := 0; i < 12; i++ {
		statusFixture(t, fmt.Sprintf("job-%02d", i), "codex", time.Now().Add(-time.Duration(i)*time.Second), &sub.Result{Status: sub.StatusSucceeded, InputTokens: 10, OutputTokens: 2, RateLimit: fmt.Sprintf("quota-%02d", i)})
	}
	statusFixture(t, "kimi", "kimi", time.Now(), &sub.Result{Status: sub.StatusFailed, InputTokens: 7, OutputTokens: 3})
	out, code, err := captureStatus(t, nil, false)
	if code != 0 || err != nil {
		t.Fatalf("status: %d %v %s", code, err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 11 {
		t.Errorf("want 10 jobs + summary: %s", out)
	}
	summary := lines[len(lines)-1]
	for _, want := range []string{"今天", "近 7 天", "codex 输入 120 / 输出 24", "kimi 输入 7 / 输出 3", "codex 额度: quota-00"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q: %s", want, summary)
		}
	}
	if strings.Contains(summary, "quota-01") {
		t.Errorf("stale quota: %s", summary)
	}
	out, _, _ = captureStatus(t, []string{"kimi"}, false)
	if strings.Contains(out, "今天") {
		t.Errorf("single status gained summary: %s", out)
	}
}

func TestSubStatusEmptyList(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	out, code, err := captureStatus(t, nil, true)
	if code != 0 || err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty: %d %v %q", code, err, out)
	}
	out, code, err = captureStatus(t, nil, false)
	if code != 0 || err != nil || !strings.Contains(out, "今天") || !strings.Contains(out, "近 7 天") {
		t.Fatalf("empty summary: %d %v %q", code, err, out)
	}
}

func TestSubJobReadExitCodes(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := buildPactify(t)
	store, _ := sub.DefaultStore()
	for _, tc := range []struct {
		name, content string
		mode          os.FileMode
	}{
		{"corrupt", "{broken", 0600}, {"unreadable", "{}", 0000},
	} {
		st, err := store.Create(tc.name, sub.Request{Harness: "codex"})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(store.Dir(st.ID), "state.json")
		if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, tc.mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0600) })
	}
	if err := os.Mkdir(store.Dir("no-state"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, reader := range []string{"status", "logs", "wait", "result", "cancel"} {
		for _, id := range []string{"missing", "corrupt", "unreadable", "no-state"} {
			t.Run(reader+"/"+id, func(t *testing.T) {
				out, code := runCLI(t, bin, t.TempDir(), "", "sub", reader, id)
				want := 1
				if id == "missing" {
					want = 2
				}
				if code != want {
					t.Fatalf("want exit %d: got %d %s", want, code, out)
				}
				if id != "missing" && (strings.Contains(out, "unknown job") || strings.Contains(out, "未知 job")) {
					t.Fatalf("hid state error: %s", out)
				}
				if id == "corrupt" && !strings.Contains(out, "invalid character") {
					t.Fatalf("lost JSON error: %s", out)
				}
				if id == "unreadable" && !strings.Contains(out, "permission denied") {
					t.Fatalf("lost permission error: %s", out)
				}
			})
		}
	}
	// Listing must also surface corruption rather than silently omitting jobs.
	out, code := runCLI(t, bin, t.TempDir(), "", "sub", "status", "--json")
	if code != 1 || !strings.Contains(out, "invalid character") {
		t.Fatalf("list hid error: %d %s", code, out)
	}
}

func TestSubStatusUsageCalendarWindowsAndLatestQuota(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, tc := range []struct {
		id            string
		created       time.Time
		input, output int
		quota         string
	}{
		{"today", today, 1, 2, "latest"},
		{"yesterday", today.Add(-time.Nanosecond), 10, 20, "old"},
		{"week-start", today.AddDate(0, 0, -6), 100, 200, "older"},
		{"before-week", today.AddDate(0, 0, -6).Add(-time.Nanosecond), 1000, 2000, "ancient"},
	} {
		statusFixture(t, tc.id, "codex", tc.created, &sub.Result{Status: sub.StatusSucceeded, InputTokens: tc.input, OutputTokens: tc.output, RateLimit: tc.quota})
	}
	out, code, err := captureStatus(t, nil, false)
	if code != 0 || err != nil {
		t.Fatalf("status: %d %v %s", code, err, out)
	}
	if !strings.Contains(out, "今天: codex 输入 1 / 输出 2") || !strings.Contains(out, "近 7 天: codex 输入 111 / 输出 222") || !strings.Contains(out, "codex 额度: latest") {
		t.Fatalf("wrong windows/quota: %s", out)
	}
}

func TestSubStatusUsageRejectsBrokenResults(t *testing.T) {
	for _, broken := range []string{"missing", "corrupt"} {
		t.Run(broken, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			store, st := statusFixture(t, "broken-result", "codex", time.Now(), &sub.Result{Status: sub.StatusSucceeded})
			if err := os.Remove(store.ResultPath(st.ID)); err != nil {
				t.Fatal(err)
			}
			if broken == "corrupt" {
				if err := os.WriteFile(store.ResultPath(st.ID), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, code, err := captureStatus(t, nil, false)
			if code != 1 || err == nil || !strings.Contains(err.Error(), "result") {
				t.Fatalf("hid incomplete usage: %d %v", code, err)
			}
			_, code, err = captureStatus(t, nil, true)
			if code != 0 || err != nil {
				t.Fatalf("JSON status unexpectedly needs result: %d %v", code, err)
			}
		})
	}
}
