package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

type fakeOrchRunner struct {
	mu     sync.Mutex
	called bool
	dir    string
	args   []string
	env    []string
}

func (f *fakeOrchRunner) run(dir string, args, env []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.dir = dir
	f.args = args
	f.env = env
	return nil
}

func seedOrchRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pactDir := filepath.Join(root, ".pact")
	os.MkdirAll(filepath.Join(pactDir, "tasks"), 0o755)
	initLine := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"testp","protocol_version":1,"seats":[{"id":"claude","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"alice","roles":["worker"],"entry":"A.md"}],"base_branch":"main"}}` + "\n"
	os.WriteFile(filepath.Join(pactDir, "log.jsonl"), []byte(initLine), 0o644)
	return root
}

func TestRunNoSeat(t *testing.T) {
	root := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{"feature": "f1"})
	defer resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Fatalf("status=%d, want 422", resp.StatusCode)
	}
}

func TestRunAlreadyRunning(t *testing.T) {
	root := seedOrchRepo(t)
	orchDir := filepath.Join(root, ".pact", "orchestrate")
	os.MkdirAll(orchDir, 0o755)
	recent := time.Now().UTC().Format(time.RFC3339)
	os.WriteFile(filepath.Join(orchDir, "status.json"), []byte(`{"done":false,"escalated":false,"phase":"act","updated_at":"`+recent+`"}`), 0o644)

	fake := &fakeOrchRunner{}
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetExecOrchestrate(fake.run)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{"feature": "f1"})
	defer resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	if fake.called {
		t.Fatal("runner should not be called when already running")
	}
}

func TestRunSuccess(t *testing.T) {
	root := seedOrchRepo(t)
	fake := &fakeOrchRunner{}
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetExecOrchestrate(fake.run)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{"feature": "f1", "max_concurrency": 3})
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("status=%d body=%v, want 202", resp.StatusCode, body)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if !strings.Contains(result["status_url"], "/api/projects/p/orchestrate/status") {
		t.Fatalf("status_url=%q, want path containing /orchestrate/status", result["status_url"])
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !fake.called {
		t.Fatal("runner should be called")
	}
	args := strings.Join(fake.args, " ")
	if !strings.Contains(args, "--feature f1") {
		t.Errorf("args %q should contain --feature f1", args)
	}
	if !strings.Contains(args, "--as claude") {
		t.Errorf("args %q should contain --as claude", args)
	}
	if !strings.Contains(args, "--max-concurrency 3") {
		t.Errorf("args %q should contain --max-concurrency 3", args)
	}
}

func TestRunSeatKindsFromBody(t *testing.T) {
	root := seedOrchRepo(t)
	fake := &fakeOrchRunner{}
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetExecOrchestrate(fake.run)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{
		"feature":    "f1",
		"seat_kinds": map[string]string{"alice": "opencode", "claude": "claude-code"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("status=%d body=%v, want 202", resp.StatusCode, body)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !fake.called {
		t.Fatal("runner should be called")
	}
	args := strings.Join(fake.args, " ")
	if !strings.Contains(args, "--seat-kind alice=opencode") {
		t.Errorf("args %q should contain --seat-kind alice=opencode", args)
	}
	if !strings.Contains(args, "--seat-kind claude=claude-code") {
		t.Errorf("args %q should contain --seat-kind claude=claude-code", args)
	}
}

func TestRunSeatKindsFromInit(t *testing.T) {
	root := t.TempDir()
	pactDir := filepath.Join(root, ".pact")
	os.MkdirAll(filepath.Join(pactDir, "tasks"), 0o755)
	initLine := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"testp","protocol_version":1,"seats":[{"id":"claude","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md","kind":"claude-code"},{"id":"alice","roles":["worker"],"entry":"A.md","kind":"opencode"}],"base_branch":"main"}}` + "\n"
	os.WriteFile(filepath.Join(pactDir, "log.jsonl"), []byte(initLine), 0o644)

	fake := &fakeOrchRunner{}
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetExecOrchestrate(fake.run)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{"feature": "f1"})
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("status=%d body=%v, want 202", resp.StatusCode, body)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	args := strings.Join(fake.args, " ")
	if !strings.Contains(args, "--seat-kind alice=opencode") {
		t.Errorf("args %q should contain --seat-kind alice=opencode", args)
	}
	if !strings.Contains(args, "--seat-kind claude=claude-code") {
		t.Errorf("args %q should contain --seat-kind claude=claude-code", args)
	}
}

func TestResumeHasResumeFlag(t *testing.T) {
	root := seedOrchRepo(t)
	orchDir := filepath.Join(root, ".pact", "orchestrate")
	os.MkdirAll(orchDir, 0o755)
	os.WriteFile(filepath.Join(orchDir, "status.json"), []byte(`{"done":false,"escalated":true}`), 0o644)

	fake := &fakeOrchRunner{}
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetExecOrchestrate(fake.run)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/resume", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("status=%d body=%v, want 202", resp.StatusCode, body)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	args := strings.Join(fake.args, " ")
	if !strings.Contains(args, "--resume") {
		t.Errorf("args %q should contain --resume", args)
	}
}

func TestShipPush(t *testing.T) {
	root := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	var pushCalled bool
	srv.SetFinishRunner(func(dir, name string, args ...string) (string, error) {
		pushCalled = true
		return "pushed to origin/main\n", nil
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/ship", map[string]any{
		"remote": "origin",
		"branch": "main",
	})
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("status=%d body=%v, want 200", resp.StatusCode, body)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["pushed"] != true {
		t.Fatalf("pushed=%v, want true", result["pushed"])
	}
	if !pushCalled {
		t.Fatal("finish runner should be called")
	}
}

func TestShipPR(t *testing.T) {
	root := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetFinishRunner(func(dir, name string, args ...string) (string, error) {
		return "https://github.com/org/repo/pull/42\n", nil
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/ship", map[string]any{
		"pr":     true,
		"branch": "main",
		"title":  "Ship it",
		"body":   "All done",
		"head":   "feat-x",
	})
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("status=%d body=%v, want 200", resp.StatusCode, body)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["pushed"] != true {
		t.Fatalf("pushed=%v, want true", result["pushed"])
	}
	if result["pr_url"] != "https://github.com/org/repo/pull/42" {
		t.Fatalf("pr_url=%v, want PR URL", result["pr_url"])
	}
}

func TestDiff(t *testing.T) {
	root := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetGitDiff(func(dir string, staged bool) (string, error) {
		if staged {
			return "staged diff output", nil
		}
		return "diff --git a/file.go b/file.go\n+line", nil
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/projects/p/orchestrate/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if !strings.Contains(result["diff"], "diff --git") {
		t.Fatalf("diff=%q, want git diff output", result["diff"])
	}

	resp2, err := http.Get(ts.URL + "/api/projects/p/orchestrate/diff?staged")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var result2 map[string]string
	json.NewDecoder(resp2.Body).Decode(&result2)
	if result2["diff"] != "staged diff output" {
		t.Fatalf("staged diff=%q, want 'staged diff output'", result2["diff"])
	}
}
