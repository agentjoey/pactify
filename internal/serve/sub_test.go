package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/agentjoey/pactify/internal/subpanel"
)

func TestSubAPIReadOnlyAndDefaultStore(t *testing.T) {
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "https://dash.example.com")
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Create("job-dashboard", sub.Request{Harness: "codex", Brief: "# Example-689 Jobs", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = sub.StatusSucceeded
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.Dir("incomplete"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.LogPath(st.ID), []byte("old-outside-window\n"+strings.Repeat("x", (1<<20)+128)+"\nmessage after oversized line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := serveHandler(t, New(), "127.0.0.1:7777")
	query := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "https://dash.example.com"+path, nil)
		r.Method = method
		h.ServeHTTP(w, r)
		return w
	}
	w := query("GET", "/api/sub/jobs?all=1")
	var rows []sub.JobSummary
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	if w.Code != 200 || len(rows) != 1 || rows[0].ID != st.ID {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/sub/jobs", "/api/sub/jobs/" + st.ID, "/api/sub/jobs/" + st.ID + "/logs", "/api/sub/jobs/" + st.ID + "/diff", "/api/sub/jobs/" + st.ID + "/cancel", "/api/sub/jobs/" + st.ID + "/resume", "/api/sub/jobs/" + st.ID + "/apply", "/api/sub/jobs/" + st.ID + "/discard"} {
		for _, method := range []string{"HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT"} {
			want := http.StatusMethodNotAllowed
			// Serve-only POSTs require JSON and reject other content types with 415.
			if method == "POST" && (strings.HasSuffix(path, "/cancel") || strings.HasSuffix(path, "/resume") || strings.HasSuffix(path, "/apply") || strings.HasSuffix(path, "/discard")) {
				want = http.StatusUnsupportedMediaType
			}
			if w := query(method, path); w.Code != want {
				t.Errorf("%s %s: %d (want %d)", method, path, w.Code, want)
			}
		}
	}
	for _, path := range []string{"/api/sub/jobs/" + st.ID, "/api/sub/jobs/" + st.ID + "/diff", "/api/sub/jobs/" + st.ID + "/logs"} {
		if w := query("GET", path); w.Code != 200 {
			t.Errorf("GET %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	w = query("GET", "/api/sub/jobs/"+st.ID+"/logs")
	if strings.Contains(w.Body.String(), "old-outside-window") || !strings.Contains(w.Body.String(), "after oversized line") || !strings.Contains(w.Body.String(), "event: done") {
		t.Fatalf("log window: %s", w.Body.String())
	}
	for _, id := range []string{"%2e%2e", "..%2foutside", "..%5coutside", "%00", "missing"} {
		for _, suffix := range []string{"", "/logs", "/diff"} {
			w := query("GET", "/api/sub/jobs/"+id+suffix)
			if w.Code < 400 || w.Code >= 600 {
				t.Errorf("unsafe/missing id %s%s: %d", id, suffix, w.Code)
			}
		}
	}
	var detail subpanel.Detail
	w = query("GET", "/api/sub/jobs/"+st.ID)
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil || detail.Brief != st.Request.Brief {
		t.Fatalf("detail: %s", w.Body.String())
	}
	entries, err := os.ReadDir(store.Dir(st.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "state.json" && entry.Name() != "log.jsonl" {
			t.Errorf("read created %s", entry.Name())
		}
	}
}

func TestSubAPIEmptyAndErrors(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h := serveHandler(t, New(), "127.0.0.1:7777")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:7777/api/sub/jobs", nil))
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty: %d %s", w.Code, w.Body.String())
	}
	store, _ := sub.DefaultStore()
	if err := os.WriteFile(store.Root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:7777/api/sub/jobs", nil))
	if w.Code != 500 {
		t.Fatalf("store error: %d %s", w.Code, w.Body.String())
	}
}
