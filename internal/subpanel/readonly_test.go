package subpanel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestReadOnlyHandlerCanMountWithoutPanelAuthentication(t *testing.T) {
	_, store := fixture(t)
	mux := http.NewServeMux()
	mux.Handle("/custom/", http.StripPrefix("/custom", ReadOnlyHandler(store)))
	for _, resource := range []string{"jobs", "jobs/job-00", "jobs/job-00/logs", "jobs/job-00/events", "jobs/job-00/diff"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PATCH", "PUT", "DELETE", "OPTIONS", "TRACE", "CONNECT"} {
			r := httptest.NewRequest("GET", "/custom/"+resource, nil)
			r.Method = method
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			want := http.StatusMethodNotAllowed
			if method == "GET" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Errorf("%s %s: %d %s", method, resource, w.Code, w.Body.String())
			}
			if method != "GET" && w.Header().Get("Allow") != "GET" {
				t.Errorf("Allow: %v", w.Header())
			}
		}
	}
}

func TestReadOnlyDiffDeadline(t *testing.T) {
	s, store := fixture(t)
	st, err := store.Load("job-00")
	if err != nil {
		t.Fatal(err)
	}
	st.Worktree = &sub.Worktree{Path: t.TempDir(), BaseCommit: "abc"}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// Exec replaces the shell so CommandContext can kill the blocked process.
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	start := time.Now()
	w := request(s, "GET", "/api/jobs/job-00/diff", s.token)
	if elapsed := time.Since(start); elapsed < 9*time.Second || elapsed > 15*time.Second {
		t.Fatalf("10s deadline: %v", elapsed)
	}
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Live diff unavailable") {
		t.Fatalf("deadline response: %d %s", w.Code, w.Body.String())
	}
}
