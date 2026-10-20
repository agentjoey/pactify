package subpanel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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

func TestDoctorAndInfoAreReadOnlySnapshots(t *testing.T) {
	s, store := fixture(t)
	t.Setenv("PATH", t.TempDir()) // No model discovery or doctor process can run.
	before := costFiles(t, filepath.Dir(store.Root))
	for _, resource := range []string{"doctor", "info"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			w := request(s, method, "/api/"+resource, s.token)
			want := 405
			if method == "GET" {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("%s %s: %d %s", method, resource, w.Code, w.Body.String())
			}
			if method != "GET" && w.Header().Get("Allow") != "GET" {
				t.Fatal(w.Header())
			}
			if method == "GET" {
				var body map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if resource == "doctor" && (string(body["lastRun"]) != "null" || len(body["harnesses"]) == 0) {
					t.Fatal(w.Body.String())
				}
				if resource == "info" && len(body["version"]) == 0 {
					t.Fatal(w.Body.String())
				}
			}
		}
		if w := request(s, "GET", "/api/"+resource, ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if !reflect.DeepEqual(before, costFiles(t, filepath.Dir(store.Root))) {
		t.Fatal("read endpoints changed storage")
	}
}
func TestDiffExposesSizeLimit(t *testing.T) {
	s, _ := fixture(t)
	w := request(s, "GET", "/api/jobs/job-00/diff", s.token)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["maxBytes"] != float64(MaxDiffBytes) {
		t.Fatal(w.Body.String())
	}
}

func TestDoctorUsesStoredVersionsWithoutExecutingHarness(t *testing.T) {
	s, store := fixture(t)
	bin := t.TempDir()
	path := filepath.Join(bin, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf executed > '"+filepath.Join(bin, "executed")+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	// The catalog records the symlink-resolved executable (macOS temp dirs live under /private).
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		t.Fatal(err)
	}
	catalog := map[string]any{"codex": map[string]any{"Executable": map[string]any{"Path": resolved, "Size": info.Size(), "ModTime": info.ModTime().UTC()}, "Version": "0.162.1"}}
	data, _ := json.Marshal(catalog)
	if err := os.WriteFile(filepath.Join(filepath.Dir(store.Root), "harness-catalog.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	before := costFiles(t, filepath.Dir(store.Root))
	w := request(s, "GET", "/api/doctor", s.token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":"0.162.1"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(bin, "executed")); !os.IsNotExist(err) {
		t.Fatal("executed harness")
	}
	if !reflect.DeepEqual(before, costFiles(t, filepath.Dir(store.Root))) {
		t.Fatal("changed storage")
	}
}
