package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

func TestMachineEndpoints_RequireSeat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	posts := []string{
		"/api/agents/opencode/register",
		"/api/agents/opencode/config",
		"/api/agents/opencode/sessions/prune",
		"/api/manifests",
		"/api/recipes/add-tests/expand",
		"/api/registry",
	}
	for _, path := range posts {
		resp, _ := http.Post(ts.URL+path, "application/json", strings.NewReader("{}"))
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("POST %s no-seat status=%d, want 422", path, resp.StatusCode)
		}
		resp.Body.Close()
	}

	deletes := []string{
		"/api/agents/opencode/register",
		"/api/manifests/webx",
		"/api/registry/nope",
	}
	for _, path := range deletes {
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+path, nil)
		resp, _ := http.DefaultClient.Do(req)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("DELETE %s no-seat status=%d, want 422", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestMachineEndpoints_PassWithSeat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, _ := http.Post(ts.URL+"/api/recipes/add-tests/expand", "application/json", strings.NewReader(`{"goal":"test"}`))
	if resp.StatusCode == http.StatusUnprocessableEntity {
		t.Fatal("with seat configured, should not get 422 from seat gate")
	}
	resp.Body.Close()
}

func TestWire_RequiresActingProject(t *testing.T) {
	dir := newAuthorRepo(t)
	srv := New([]registry.Project{{Name: "pactify", Path: dir}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/pactify/wiring/opencode", map[string]any{"seat": "opencode", "roles": "worker"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("wire no-seat status=%d, want 422", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "acting seat") {
		t.Fatalf("wire error %q must mention acting seat", msg)
	}
}
