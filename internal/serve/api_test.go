package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

func seedProject(t *testing.T, root, project string) {
	t.Helper()
	pact := filepath.Join(root, ".pact")
	os.MkdirAll(pact, 0o755)
	line := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"` + project + `","protocol_version":1,"seats":[{"id":"claude-opus","roles":["orchestrator"],"entry":"CLAUDE.md"}],"base_branch":"main"}}` + "\n"
	os.WriteFile(filepath.Join(pact, "log.jsonl"), []byte(line), 0o644)
}

func TestAPIProjectsAndState(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	srv := New([]registry.Project{{Name: "pactify", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, _ := http.Get(ts.URL + "/api/projects")
	var projects []map[string]any
	json.NewDecoder(resp.Body).Decode(&projects)
	resp.Body.Close()
	if len(projects) != 1 || projects[0]["name"] != "pactify" {
		t.Fatalf("projects: %+v", projects)
	}

	resp, _ = http.Get(ts.URL + "/api/projects/pactify/state")
	var st StateDTO
	json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if st.Project != "pactify" {
		t.Fatalf("state: %+v", st)
	}

	resp, _ = http.Get(ts.URL + "/api/projects/nope/state")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
