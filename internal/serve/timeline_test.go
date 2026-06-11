package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/projection"
	"github.com/agentjoey/pactify/internal/registry"
)

// seedTimelineRepo writes a scripted log.jsonl with a mix of events: an init
// (no task/feature), a task assign (task+feature), and a join (neither). It
// returns the repo root and the raw events for prefix-fold comparisons.
func seedTimelineRepo(t *testing.T) (string, []event.Event) {
	t.Helper()
	root := t.TempDir()
	pactDir := filepath.Join(root, ".pact")
	if err := os.MkdirAll(pactDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lines := []string{
		`{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"pactify","protocol_version":1,"seats":[{"id":"claude-opus","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"},{"id":"opencode","roles":["worker"],"entry":"AGENTS.md"}],"base_branch":"main"}}`,
		`{"event_id":"2","ts":"2026-01-01T00:01:00Z","agent_id":"opencode","role":"worker","event_type":"join","task_id":"","feature":"","payload":{}}`,
		`{"event_id":"3","ts":"2026-01-01T00:02:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"assign","task_id":"t1","feature":"f1","payload":{"branch":"feat/f1","owner":"opencode","reviewer":"claude-opus","spec":"do it"}}`,
	}
	if err := os.WriteFile(filepath.Join(pactDir, "log.jsonl"), []byte(joinLines(lines)), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	evs, err := event.ReadAll(filepath.Join(pactDir, "log.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return root, evs
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

func timelineServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	srv := New([]registry.Project{{Name: "pactify", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestTimeline: total correct, events ordered n=1..total, task/feature present
// only when the event carries them.
func TestTimeline(t *testing.T) {
	root, evs := seedTimelineRepo(t)
	ts := timelineServer(t, root)

	resp, err := http.Get(ts.URL + "/api/projects/pactify/timeline")
	if err != nil {
		t.Fatalf("GET timeline: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("timeline: want 200 got %d", resp.StatusCode)
	}
	var tl TimelineDTO
	if err := json.NewDecoder(resp.Body).Decode(&tl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tl.Total != len(evs) {
		t.Fatalf("total = %d want %d", tl.Total, len(evs))
	}
	if len(tl.Events) != len(evs) {
		t.Fatalf("events len = %d want %d", len(tl.Events), len(evs))
	}
	for i, e := range tl.Events {
		if e.N != i+1 {
			t.Fatalf("event[%d].n = %d want %d", i, e.N, i+1)
		}
		if e.Type != evs[i].EventType {
			t.Fatalf("event[%d].type = %q want %q", i, e.Type, evs[i].EventType)
		}
		if e.Actor != evs[i].AgentID {
			t.Fatalf("event[%d].actor = %q want %q", i, e.Actor, evs[i].AgentID)
		}
		if e.TS != evs[i].TS {
			t.Fatalf("event[%d].ts = %q want %q", i, e.TS, evs[i].TS)
		}
	}
	// init (n=1): no task/feature. assign (n=3): both present.
	if tl.Events[0].Task != "" || tl.Events[0].Feature != "" {
		t.Fatalf("init event must omit task/feature: %+v", tl.Events[0])
	}
	if tl.Events[2].Task != "t1" || tl.Events[2].Feature != "f1" {
		t.Fatalf("assign event must carry task/feature: %+v", tl.Events[2])
	}
}

// TestTimelineUnknownProject: unknown id → 404.
func TestTimelineUnknownProject(t *testing.T) {
	root, _ := seedTimelineRepo(t)
	ts := timelineServer(t, root)
	resp, err := http.Get(ts.URL + "/api/projects/nope/timeline")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 got %d", resp.StatusCode)
	}
}

// TestTimelineOmitemptyTaskFeature: the JSON wire form omits task/feature keys
// entirely when absent.
func TestTimelineOmitemptyTaskFeature(t *testing.T) {
	root, _ := seedTimelineRepo(t)
	ts := timelineServer(t, root)
	resp, _ := http.Get(ts.URL + "/api/projects/pactify/timeline")
	defer resp.Body.Close()
	var raw struct {
		Events []map[string]any `json:"events"`
	}
	json.NewDecoder(resp.Body).Decode(&raw)
	if _, has := raw.Events[0]["task"]; has {
		t.Fatalf("init event[0] must omit task key: %+v", raw.Events[0])
	}
	if _, has := raw.Events[0]["feature"]; has {
		t.Fatalf("init event[0] must omit feature key: %+v", raw.Events[0])
	}
	if _, has := raw.Events[2]["task"]; !has {
		t.Fatalf("assign event[2] must include task key: %+v", raw.Events[2])
	}
}

// getStateAt fetches /state with the given query suffix and returns the response.
func getStateAt(t *testing.T, baseURL, query string) *http.Response {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/projects/pactify/state" + query)
	if err != nil {
		t.Fatalf("GET state%s: %v", query, err)
	}
	return resp
}

// TestStateAtZero: at=0 equals the empty-fold DTO shape.
func TestStateAtZero(t *testing.T) {
	root, _ := seedTimelineRepo(t)
	ts := timelineServer(t, root)
	resp := getStateAt(t, ts.URL, "?at=0")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d", resp.StatusCode)
	}
	got, _ := io_ReadAll(t, resp)
	want, err := json.Marshal(toDTO(projection.Project(nil)))
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("at=0 body\n got %s\nwant %s", got, want)
	}
}

// TestStateAtMid: at=k mid-log equals folding evs[:k].
func TestStateAtMid(t *testing.T) {
	root, evs := seedTimelineRepo(t)
	ts := timelineServer(t, root)
	k := 2
	resp := getStateAt(t, ts.URL, "?at=2")
	defer resp.Body.Close()
	got, _ := io_ReadAll(t, resp)
	want, _ := json.Marshal(toDTO(projection.Project(evs[:k])))
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("at=%d body\n got %s\nwant %s", k, got, want)
	}
}

// TestStateAtClamp: at >= total clamps to the full state.
func TestStateAtClamp(t *testing.T) {
	root, evs := seedTimelineRepo(t)
	ts := timelineServer(t, root)
	resp := getStateAt(t, ts.URL, "?at=9999")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d", resp.StatusCode)
	}
	got, _ := io_ReadAll(t, resp)
	want, _ := json.Marshal(toDTO(projection.Project(evs)))
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("clamp body\n got %s\nwant %s", got, want)
	}
}

// TestStateAtBad: malformed/negative at → 400 with {"error"}.
func TestStateAtBad(t *testing.T) {
	root, _ := seedTimelineRepo(t)
	ts := timelineServer(t, root)
	for _, q := range []string{"?at=junk", "?at=-1"} {
		resp := getStateAt(t, ts.URL, q)
		if resp.StatusCode != http.StatusBadRequest {
			resp.Body.Close()
			t.Fatalf("%s: want 400 got %d", q, resp.StatusCode)
		}
		var m map[string]string
		json.NewDecoder(resp.Body).Decode(&m)
		resp.Body.Close()
		if m["error"] == "" {
			t.Fatalf("%s: want {\"error\":...} got %+v", q, m)
		}
	}
}

// TestStateNoAtByteIdentical: with no at param, the body is byte-identical to
// marshaling the ProjectState result (the pre-change handler output).
func TestStateNoAtByteIdentical(t *testing.T) {
	root, _ := seedTimelineRepo(t)
	ts := timelineServer(t, root)
	resp := getStateAt(t, ts.URL, "")
	defer resp.Body.Close()
	got, _ := io_ReadAll(t, resp)
	dto, err := ProjectState(root)
	if err != nil {
		t.Fatalf("ProjectState: %v", err)
	}
	want, _ := json.Marshal(dto)
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("no-at body\n got %s\nwant %s", got, want)
	}
}

// io_ReadAll drains the response body.
func io_ReadAll(t *testing.T, resp *http.Response) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(resp.Body)
	return buf.Bytes(), err
}
