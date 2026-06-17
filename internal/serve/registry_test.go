package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// registryServer wires an EMPTY server (no projects) with the registry routes,
// isolating the registry file ($PACTIFY_HOME) to a temp dir so Load/Save never
// touch the real ~/.pactify. It starts the watcher pool (needed for live
// AddProject watcher starts) and returns the httptest server.
func registryServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	srv := New(nil)
	srv.SetSeat("test")
	if err := srv.StartWatchers(); err != nil {
		t.Fatalf("start watchers: %v", err)
	}
	t.Cleanup(srv.Stop)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
}

func TestRegistryGETListsStatus(t *testing.T) {
	dir := newAuthorRepo(t)
	srv, ts := registryServer(t)
	if err := srv.AddProject(registry.Project{Name: "pactify", Path: dir}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	resp, _ := http.Get(ts.URL + "/api/registry")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d", resp.StatusCode)
	}
	var out []struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		Status struct {
			Valid       bool   `json:"valid"`
			Error       string `json:"error"`
			Seats       int    `json:"seats"`
			LastEventTs string `json:"lastEventTs"`
		} `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if len(out) != 1 || out[0].Name != "pactify" || out[0].Path != dir {
		t.Fatalf("registry list: %+v", out)
	}
	if !out[0].Status.Valid {
		t.Fatalf("status should be valid: %+v", out[0].Status)
	}
	if out[0].Status.Seats != 2 {
		t.Fatalf("seats = %d want 2", out[0].Status.Seats)
	}
	if out[0].Status.LastEventTs == "" {
		t.Fatalf("lastEventTs should be set")
	}
}

// TestRegistryPOSTInvalidPaths walks each validation in order, asserting a 400
// with a reason-bearing error for: relative path, missing path, a dir that is
// not a git repo, and a git repo lacking .pact/.
func TestRegistryPOSTInvalidPaths(t *testing.T) {
	_, ts := registryServer(t)

	// non-git, git-but-no-.pact fixtures
	nonGit := t.TempDir()
	gitNoPact := t.TempDir()
	gitInit(t, gitNoPact)

	cases := []struct {
		name   string
		path   string
		reason string
	}{
		{"relative", "relative/path", "absolute"},
		{"missing", filepath.Join(t.TempDir(), "does-not-exist"), "exist"},
		{"non-git", nonGit, "git"},
		{"no-pact", gitNoPact, ".pact"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := postJSON(t, ts.URL+"/api/registry", map[string]any{"path": c.path})
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("path %q: want 400 got %d", c.path, resp.StatusCode)
			}
			if msg := errBody(t, resp); !strings.Contains(msg, c.reason) {
				t.Fatalf("path %q: error %q must mention %q", c.path, msg, c.reason)
			}
		})
	}
}

func TestRegistryPOSTDuplicate(t *testing.T) {
	dir := newAuthorRepo(t)
	_, ts := registryServer(t)
	resp := postJSON(t, ts.URL+"/api/registry", map[string]any{"path": dir, "name": "dup"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first register: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()
	resp = postJSON(t, ts.URL+"/api/registry", map[string]any{"path": dir, "name": "dup"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate: want 409 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "already") {
		t.Fatalf("duplicate error %q must mention already", msg)
	}
}

// TestRegistryPOSTDuplicatePath registers the SAME path under two DIFFERENT names
// and asserts the second is rejected 409 (shared fsnotify watch would otherwise be
// killed on remove), naming the existing registration.
func TestRegistryPOSTDuplicatePath(t *testing.T) {
	dir := newAuthorRepo(t)
	_, ts := registryServer(t)
	resp := postJSON(t, ts.URL+"/api/registry", map[string]any{"path": dir, "name": "first"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first register: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()
	resp = postJSON(t, ts.URL+"/api/registry", map[string]any{"path": dir, "name": "second"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate path: want 409 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "already registered as first") {
		t.Fatalf("duplicate-path error %q must name the existing registration", msg)
	}
}

// TestRegistryRegisterMakesProjectLive registers a scratch repo and asserts the
// project (1) appears in GET /api/projects and (2) has a LIVE watcher: appending
// a join line to its log produces an SSE hub event.
func TestRegistryRegisterMakesProjectLive(t *testing.T) {
	dir := newAuthorRepo(t)
	srv, ts := registryServer(t)

	resp := postJSON(t, ts.URL+"/api/registry", map[string]any{"path": dir, "name": "live"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	var got map[string]string
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got["name"] != "live" {
		t.Fatalf("register name = %q want live", got["name"])
	}

	// (1) visible in /api/projects
	presp, _ := http.Get(ts.URL + "/api/projects")
	var projects []map[string]any
	json.NewDecoder(presp.Body).Decode(&projects)
	presp.Body.Close()
	found := false
	for _, p := range projects {
		if p["name"] == "live" {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered project not in /api/projects: %+v", projects)
	}

	// (2) watcher is live: subscribe to the SSE hub, append a join line, observe it.
	ch := srv.hub.subscribe("live")
	defer srv.hub.unsubscribe("live", ch)
	time.Sleep(50 * time.Millisecond) // let the watcher settle
	appendLine(t, dir, `{"event_id":"J","ts":"2026-06-11T00:00:00Z","agent_id":"opencode","role":"worker","event_type":"join","task_id":"","feature":"","payload":{}}`)
	if line := recvWithin(t, ch, 3*time.Second); !strings.Contains(line, `"event_id":"J"`) {
		t.Fatalf("expected live watcher to broadcast join, got %q", line)
	}
}

// TestRegistryDeleteStopsWatcher registers then DELETEs a project and asserts
// (1) DELETE 200, (2) /state 404s, (3) the watcher is STOPPED: appending to the
// (now-unregistered) log produces NO event on a fresh subscription.
func TestRegistryDeleteStopsWatcher(t *testing.T) {
	dir := newAuthorRepo(t)
	srv, ts := registryServer(t)

	resp := postJSON(t, ts.URL+"/api/registry", map[string]any{"path": dir, "name": "gone"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()

	// DELETE
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/registry/gone", nil)
	dresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if dresp.StatusCode != http.StatusOK {
		t.Fatalf("delete: want 200 got %d", dresp.StatusCode)
	}
	dresp.Body.Close()

	// (1) /state now 404s
	sresp, _ := http.Get(ts.URL + "/api/projects/gone/state")
	if sresp.StatusCode != http.StatusNotFound {
		t.Fatalf("state after delete: want 404 got %d", sresp.StatusCode)
	}
	sresp.Body.Close()

	// (2) watcher stopped: a fresh subscription to the (now-stale) id must see no
	// event when the file is appended to.
	ch := srv.hub.subscribe("gone")
	defer srv.hub.unsubscribe("gone", ch)
	time.Sleep(50 * time.Millisecond)
	appendLine(t, dir, `{"event_id":"K","ts":"2026-06-11T00:01:00Z","agent_id":"opencode","role":"worker","event_type":"join","task_id":"","feature":"","payload":{}}`)
	select {
	case line := <-ch:
		t.Fatalf("watcher should be stopped; got broadcast %q", line)
	case <-time.After(500 * time.Millisecond):
		// good: no event after removal
	}
}

func TestRegistryDeleteUnknown(t *testing.T) {
	_, ts := registryServer(t)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/registry/nope", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAddProjectDuplicate asserts the engine-level guard: AddProject of a name
// already present returns an error (and does not double-register).
func TestAddProjectDuplicate(t *testing.T) {
	dir := newAuthorRepo(t)
	srv, _ := registryServer(t)
	if err := srv.AddProject(registry.Project{Name: "x", Path: dir}); err != nil {
		t.Fatal(err)
	}
	if err := srv.AddProject(registry.Project{Name: "x", Path: dir}); err == nil {
		t.Fatal("duplicate AddProject must error")
	}
}

// TestRemoveProjectUnknown asserts RemoveProject of an unknown name errors.
func TestRemoveProjectUnknown(t *testing.T) {
	srv, _ := registryServer(t)
	if err := srv.RemoveProject("nope"); err == nil {
		t.Fatal("unknown RemoveProject must error")
	}
}

// appendLine appends a single log line (with newline) to dir/.pact/log.jsonl.
func appendLine(t *testing.T, dir, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, ".pact", "log.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	f.Close()
}
