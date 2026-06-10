package serve

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
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

// TestPostTaskConflictWithCommittedTask asserts that re-POSTing a task id that
// is already a committed (assigned) task returns 409 and does NOT overwrite the
// existing spec file.
func TestPostTaskConflictWithCommittedTask(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	// Assign t1 so it becomes a committed task.
	resp := postJSON(t, ts.URL+"/api/projects/pactify/verbs/assign", map[string]any{
		"task": "t1", "feature": "F", "branch": "feat/x",
		"owner": "opencode", "reviewer": "claude-opus", "spec": ".pact/tasks/t1.md", "deps": []string{},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()

	// Write sentinel content the conflicting POST must not clobber.
	specPath := filepath.Join(dir, ".pact", "tasks", "t1.md")
	os.MkdirAll(filepath.Dir(specPath), 0o755)
	os.WriteFile(specPath, []byte("SENTINEL"), 0o644)

	resp = postJSON(t, ts.URL+"/api/projects/pactify/tasks", map[string]any{"id": "t1", "spec_md": "OVERWRITE"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("want 409 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "already exists") {
		t.Fatalf("error %q must mention already exists", msg)
	}
	b, _ := os.ReadFile(specPath)
	if string(b) != "SENTINEL" {
		t.Fatalf("spec file overwritten: %q", b)
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

// assignViaAPI assigns t1 (owner opencode, reviewer claude-opus) over HTTP and
// fails the test unless it returns 200. Used by the verb happy-path tests.
func assignViaAPI(t *testing.T, ts *httptest.Server) {
	t.Helper()
	resp := postJSON(t, ts.URL+"/api/projects/pactify/verbs/assign", map[string]any{
		"task": "t1", "feature": "F", "branch": "feat/x",
		"owner": "opencode", "reviewer": "claude-opus", "spec": ".pact/tasks/t1.md", "deps": []string{},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()
}

// TestVerbsHappyPath walks the full author flow over HTTP: assign (API) →
// worker join+checkpoint (engine, simulating the worker) → accept (API) →
// merge (API) → STATE reports the feature shipped.
func TestVerbsHappyPath(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus") // acting seat = reviewer
	assignViaAPI(t, ts)

	// Simulate the worker joining its feature branch and checkpointing.
	worker := pact.At(dir).As("opencode")
	if err := worker.Join("opencode", "worker"); err != nil {
		t.Fatalf("worker join: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "work.txt"), []byte("done"), 0o644)
	if err := worker.Checkpoint("t1", "evidence: built"); err != nil {
		t.Fatalf("worker checkpoint: %v", err)
	}

	// reviewer accepts over the API.
	resp := postJSON(t, ts.URL+"/api/projects/pactify/verbs/accept", map[string]any{"task": "t1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()

	// merge over the API.
	resp = postJSON(t, ts.URL+"/api/projects/pactify/verbs/merge", map[string]any{"feature": "F"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()

	state, err := pact.At(dir).Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(state, "shipped") {
		t.Fatalf("STATE missing shipped:\n%s", state)
	}
}

// TestAcceptSelfReview asserts the reviewer-only rule surfaces verbatim as 422:
// here the acting seat is the task OWNER (opencode), so accept must be refused.
func TestAcceptSelfReview(t *testing.T) {
	dir := newAuthorRepo(t)
	// Assign first as the orchestrator (reviewer), then re-seat the server as
	// the owner so the accept call is a self-review.
	assignTS := authorServer(t, dir, "claude-opus")
	assignViaAPI(t, assignTS)
	worker := pact.At(dir).As("opencode")
	if err := worker.Join("opencode", "worker"); err != nil {
		t.Fatalf("worker join: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "work.txt"), []byte("done"), 0o644)
	if err := worker.Checkpoint("t1", "evidence"); err != nil {
		t.Fatalf("worker checkpoint: %v", err)
	}

	ts := authorServer(t, dir, "opencode") // acting seat = OWNER, not reviewer
	resp := postJSON(t, ts.URL+"/api/projects/pactify/verbs/accept", map[string]any{"task": "t1"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "only the reviewer") {
		t.Fatalf("error %q must carry the reviewer-only engine message", msg)
	}
}

// TestMergeBeforeAccepted asserts merge before all tasks are accepted → 422
// with the engine's "not accepted" message verbatim.
func TestMergeBeforeAccepted(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	assignViaAPI(t, ts)
	resp := postJSON(t, ts.URL+"/api/projects/pactify/verbs/merge", map[string]any{"feature": "F"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "not accepted") {
		t.Fatalf("error %q must carry the merge engine message", msg)
	}
}

// TestChanges covers the changes_requested verb: empty reason → 400 (API
// guard, since the engine does not enforce it); a non-empty reason on an
// awaiting_review task → 200.
func TestChanges(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	assignViaAPI(t, ts)
	worker := pact.At(dir).As("opencode")
	if err := worker.Join("opencode", "worker"); err != nil {
		t.Fatalf("worker join: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "work.txt"), []byte("done"), 0o644)
	if err := worker.Checkpoint("t1", "evidence"); err != nil {
		t.Fatalf("worker checkpoint: %v", err)
	}

	// empty reason → 400 (engine does not enforce; API does).
	resp := postJSON(t, ts.URL+"/api/projects/pactify/verbs/changes", map[string]any{"task": "t1", "reason": ""})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty reason: want 400 got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// valid reason → 200.
	resp = postJSON(t, ts.URL+"/api/projects/pactify/verbs/changes", map[string]any{"task": "t1", "reason": "please fix the thing"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("changes: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()
}

// TestLayout covers the squad layout sidecar: GET absent → {}; PUT valid JSON
// → 200, then GET is byte-identical; PUT invalid JSON → 400; PUT >1MiB → 4xx.
func TestLayout(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "claude-opus")
	url := ts.URL + "/api/projects/pactify/squad/layout"

	// GET absent → {}.
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(b) != "{}" {
		t.Fatalf("GET absent = %d %q, want 200 {}", resp.StatusCode, b)
	}

	// PUT valid JSON → 200.
	payload := []byte(`{"nodes":[{"id":"n1","x":10,"y":20}],"v":1}`)
	resp = putBytes(t, url, payload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT valid: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()

	// GET returns byte-identical body.
	resp, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("GET after PUT = %q, want %q", got, payload)
	}

	// PUT invalid JSON → 400.
	resp = putBytes(t, url, []byte(`{not json`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT invalid: want 400 got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// PUT >1MiB → 4xx.
	big := append([]byte(`{"x":"`), bytes.Repeat([]byte("a"), 1<<20+16)...)
	big = append(big, []byte(`"}`)...)
	resp = putBytes(t, url, big)
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("PUT >1MiB: want 4xx got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// putBytes issues a PUT with a raw body (used by the layout test).
func putBytes(t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new PUT %s: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	return resp
}
