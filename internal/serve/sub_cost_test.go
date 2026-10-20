package serve

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubCostAPI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Create("cost-job", sub.Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ResultPath(st.ID), []byte(`{"status":"succeeded","inputTokens":1234,"usagePartial":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	h := serveHandler(t, New(), "127.0.0.1:7777")
	for _, tc := range []struct {
		method, query string
		status        int
	}{
		{"GET", "?since=7d&by=harness", 200},
		{"GET", "?since=0d", 400}, {"GET", "?by=invalid", 400},
		{"HEAD", "", 405}, {"POST", "", 405}, {"DELETE", "", 405},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, "http://127.0.0.1:7777/api/sub/cost"+tc.query, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.query, w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var report sub.CostReport
			if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Total.InputTokens != 1234 || report.Total.SubscriptionJobs != 1 || report.Total.UsagePartialJobs != 1 {
				t.Fatalf("report: %s", w.Body.String())
			}
		}
	}
	entries, err := os.ReadDir(store.Dir(st.ID))
	if err != nil || len(entries) != 2 {
		t.Fatalf("API wrote files: %v %v", entries, err)
	}
}
