package serve

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/registry"
)

// newAuthorRepo creates a temp git repo and pact-Inits it (two seats:
// claude-opus orchestrator,reviewer / opencode worker), acting as claude-opus.
// It returns the repo dir. PACT_DIR is left unset so the default ".pact"
// (rooted at dir via the dir-aware engine) is used.
func newAuthorRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.CombinedOutput()
	}
	if err := pact.At(dir).As("claude-opus").Init("pactify", []string{
		"claude-opus:orchestrator,reviewer:CLAUDE.md",
		"opencode:worker:AGENTS.md",
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	return dir
}

// authorServer wires a single project into a Server with the given acting seat.
func authorServer(t *testing.T, dir, seat string) *httptest.Server {
	t.Helper()
	srv := New([]registry.Project{{Name: "pactify", Path: dir}})
	srv.SetSeat(seat)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func errBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	var m map[string]string
	json.NewDecoder(resp.Body).Decode(&m)
	resp.Body.Close()
	return m["error"]
}

func TestActingSeat(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	resp, _ := http.Get(ts.URL + "/api/acting-seat")
	var m map[string]string
	json.NewDecoder(resp.Body).Decode(&m)
	resp.Body.Close()
	if m["seat"] != "claude-opus" {
		t.Fatalf("acting-seat = %q", m["seat"])
	}
}

func TestActingSeatEmpty(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "")
	resp, _ := http.Get(ts.URL + "/api/acting-seat")
	var m map[string]string
	json.NewDecoder(resp.Body).Decode(&m)
	resp.Body.Close()
	if m["seat"] != "" {
		t.Fatalf("acting-seat = %q, want empty", m["seat"])
	}
}

func TestAuthorNoActingSeat(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "")
	resp := postJSON(t, ts.URL+"/api/projects/pactify/tasks", map[string]any{"id": "t1", "spec_md": "x"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "acting seat") {
		t.Fatalf("error %q must mention acting seat", msg)
	}
}

func TestAuthorSeatNotInRoster(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "ghost")
	resp := postJSON(t, ts.URL+"/api/projects/pactify/tasks", map[string]any{"id": "t1", "spec_md": "x"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "roster") {
		t.Fatalf("error %q must mention roster", msg)
	}
}

func TestPostTask(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	resp := postJSON(t, ts.URL+"/api/projects/pactify/tasks", map[string]any{"id": "t1", "spec_md": "# T1\nspec"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()
	b, err := os.ReadFile(filepath.Join(dir, ".pact", "tasks", "t1.md"))
	if err != nil {
		t.Fatalf("task file: %v", err)
	}
	if string(b) != "# T1\nspec" {
		t.Fatalf("task content = %q", b)
	}
}

func TestPostTaskInvalidID(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	for _, id := range []string{"../x", "UPPER", ""} {
		resp := postJSON(t, ts.URL+"/api/projects/pactify/tasks", map[string]any{"id": id, "spec_md": "x"})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("id %q: want 400 got %d", id, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestPostTaskUnknownProject(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	resp := postJSON(t, ts.URL+"/api/projects/nope/tasks", map[string]any{"id": "t1", "spec_md": "x"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAssign(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	resp := postJSON(t, ts.URL+"/api/projects/pactify/verbs/assign", map[string]any{
		"task": "t1", "feature": "F", "branch": "feat/x",
		"owner": "opencode", "reviewer": "claude-opus", "spec": ".pact/tasks/t1.md", "deps": []string{},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()
	// The assign event must be logged under the ACTING seat.
	f, err := os.Open(filepath.Join(dir, ".pact", "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev map[string]any
		json.Unmarshal(sc.Bytes(), &ev)
		if ev["event_type"] == "assign" {
			got, _ = ev["agent_id"].(string)
		}
	}
	if got != "claude-opus" {
		t.Fatalf("assign agent_id = %q, want claude-opus", got)
	}
}

func TestAssignEngineError(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	// owner == reviewer → engine separation-of-duties violation.
	resp := postJSON(t, ts.URL+"/api/projects/pactify/verbs/assign", map[string]any{
		"task": "t1", "feature": "F", "branch": "feat/x",
		"owner": "opencode", "reviewer": "opencode", "spec": ".pact/tasks/t1.md", "deps": []string{},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "separation of duties") {
		t.Fatalf("error %q must carry the engine message verbatim", msg)
	}
}

func TestAssignUnknownProject(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	resp := postJSON(t, ts.URL+"/api/projects/nope/verbs/assign", map[string]any{
		"task": "t1", "feature": "F", "branch": "feat/x",
		"owner": "opencode", "reviewer": "claude-opus", "spec": "s", "deps": []string{},
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
