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

func seedProjectWithPlan(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pactDir := filepath.Join(root, ".pact")
	os.MkdirAll(filepath.Join(pactDir, "tasks"), 0o755)

	initLine := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"testp","protocol_version":1,"seats":[{"id":"claude","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"alice","roles":["worker"],"entry":"A.md"},{"id":"bob","roles":["worker"],"entry":"B.md"}],"base_branch":"main"}}` + "\n"
	os.WriteFile(filepath.Join(pactDir, "log.jsonl"), []byte(initLine), 0o644)

	manifest := `{"feature":"demo-feature","branch":"feat-demo","tasks":[{"id":"t1","owner":"alice","reviewer":"bob","spec":".pact/tasks/t1.md","verify":"go test"},{"id":"t2","owner":"bob","reviewer":"alice","spec":".pact/tasks/t2.md","verify":"go test","deps":["t1"]}]}`
	os.WriteFile(filepath.Join(pactDir, "plan-demo-feature.json"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(pactDir, "tasks", "t1.md"), []byte("# t1"), 0o644)
	os.WriteFile(filepath.Join(pactDir, "tasks", "t2.md"), []byte("# t2"), 0o644)

	return root
}

func TestHandlePlanApply(t *testing.T) {
	root := seedProjectWithPlan(t)
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/projects/p/plan/demo-feature/apply", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("status=%d, body=%v, want 200", resp.StatusCode, body)
	}
	var result map[string]int
	json.NewDecoder(resp.Body).Decode(&result)
	if result["assigned"] != 2 {
		t.Fatalf("assigned=%d, want 2", result["assigned"])
	}

	srv2 := New([]registry.Project{{Name: "p", Path: root}})
	ts2 := httptest.NewServer(srv2.Handler())
	defer ts2.Close()
	r2, err := http.Post(ts2.URL+"/api/projects/p/plan/demo-feature/apply", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 422 {
		t.Fatalf("no-seat status=%d, want 422", r2.StatusCode)
	}
}
