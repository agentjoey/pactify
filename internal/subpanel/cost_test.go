package subpanel

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func costFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[path] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestCostAPIUsesReportAndNeverWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, job := range []struct {
		id, harness, result string
		created             time.Time
	}{
		{"codex", "codex", `{"status":"succeeded","inputTokens":1200,"outputTokens":30,"usagePartial":true}`, now.Add(-time.Hour)},
		{"paid", "opencode", `{"status":"failed","inputTokens":200,"outputTokens":10,"costUSD":0.125}`, now.Add(-time.Hour)},
		{"zero", "opencode", `{"status":"succeeded","costUSD":0}`, now.Add(-time.Hour)},
		{"missing", "kimi", "", now.Add(-time.Hour)},
		{"broken", "codex", `{`, now.Add(-time.Hour)},
		{"old", "agy", `{"status":"succeeded","inputTokens":9999}`, now.AddDate(0, 0, -20)},
	} {
		st, err := store.Create(job.id, sub.Request{Harness: job.harness, Model: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		st.CreatedAt = job.created
		// Result publication can precede terminal state publication.
		st.Status = sub.StatusRunning
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}
		if job.result != "" {
			if err := os.WriteFile(store.ResultPath(st.ID), []byte(job.result), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	reset := now.Add(time.Hour)
	expired := now.Add(-time.Hour)
	quotas := map[string]sub.QuotaRoute{
		"codex/fixture": {Harness: "codex", Model: "fixture", ResetAt: &reset, RecordedAt: now},
		"agy":           {Harness: "agy", ResetAt: &expired, RecordedAt: now},
	}
	data, _ := json.Marshal(quotas)
	if err := os.WriteFile(filepath.Join(home, "quota.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	before := costFiles(t, home)
	s, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, by := range []string{"harness", "model", "repo", "day"} {
		w := request(s, "GET", "/api/cost?since=7d&by="+by, s.token)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", by, w.Code, w.Body.String())
		}
		var got struct {
			sub.CostReport
			ExhaustedRoutes []sub.QuotaRoute `json:"exhaustedRoutes"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		want, err := store.CostReport(got.Since, by, got.Until)
		if err != nil {
			t.Fatal(err)
		}
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(got.CostReport)
		if string(gotJSON) != string(wantJSON) || got.Total.Jobs != 3 || got.Total.InputTokens != 1400 || got.Total.CostUSD == nil || *got.Total.CostUSD != 0.125 || got.Skipped.MissingResult != 1 || got.Skipped.InvalidResult != 1 {
			t.Fatalf("report differs from aggregator: %s", w.Body.String())
		}
		start, _ := sub.ParseCostSince("7d", got.Until)
		if !got.Since.Equal(start) || len(got.ExhaustedRoutes) != 1 || got.ExhaustedRoutes[0].Model != "fixture" {
			t.Fatalf("window/quota: %s", w.Body.String())
		}
	}
	for _, method := range []string{"HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1/api/cost", nil)
		r.Method = method
		r.Header.Set("Authorization", "Bearer "+s.token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 405 || w.Header().Get("Allow") != "GET" {
			t.Fatalf("%s: %d", method, w.Code)
		}
	}
	if w := request(s, "GET", "/api/cost", ""); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	if after := costFiles(t, home); !reflect.DeepEqual(before, after) {
		t.Fatal("cost reads modified the store or quota memory")
	}
}

func TestCostAPIParametersAndEmptyStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	store := sub.Store{Root: filepath.Join(home, "absent", "jobs")}
	h := ReadOnlyHandler(store)
	for _, query := range []string{"", "?since=24h", "?since=03d", "?since=2020-01-01&by=day", "?since=0d", "?since=-1h", "?since=all", "?since=1d2h", "?since=+1d", "?since=1.5d", "?since=999999999999d", "?since=9999-12-31", "?since=", "?by=", "?by=route"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/cost"+query, nil))
		want := http.StatusBadRequest
		if query == "" || query == "?since=24h" || query == "?since=03d" || query == "?since=2020-01-01&by=day" {
			want = http.StatusOK
		}
		if w.Code != want {
			t.Errorf("%s: want %d, got %d %s", query, want, w.Code, w.Body.String())
		}
		if query == "" {
			var got struct {
				sub.CostReport
				ExhaustedRoutes []sub.QuotaRoute `json:"exhaustedRoutes"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.By != "harness" || got.Groups == nil || got.ExhaustedRoutes == nil || got.Total.Jobs != 0 {
				t.Fatalf("empty: %s", w.Body.String())
			}
		}
	}
	if _, err := os.Stat(filepath.Dir(store.Root)); !os.IsNotExist(err) {
		t.Fatalf("created absent store: %v", err)
	}
	if err := os.WriteFile(filepath.Dir(store.Root), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/cost", nil))
	if w.Code != 500 {
		t.Fatalf("store error: %d %s", w.Code, w.Body.String())
	}
}
